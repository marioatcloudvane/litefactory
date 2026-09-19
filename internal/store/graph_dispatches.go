package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// graphTriggerBase reserves a third band of factory-private run identities,
// beside automatic recovery and manual schedules. A graph stage is not a forge
// comment, so it cannot borrow one: the runs table admits at most one run per
// (session, trigger), and that uniqueness is the crash-replay guarantee. Minting
// an identity here is what lets a graph advance without weakening it.
const graphTriggerBase int64 = 1 << 60

// GraphDispatch is one intended graph step and the run it was allocated.
type GraphDispatch struct {
	ID, InstanceID, SessionID int64
	Node                      string
	DueAt                     time.Time
	Status                    string
	AllocatedRunID            int64
	ReservedAt, FinishedAt    time.Time
	CancellationReason        string
	CreatedAt                 time.Time
}

// TriggerID returns the private execution identity for this dispatch.
func (d GraphDispatch) TriggerID() int64 { return graphTriggerBase + d.ID }

// IsGraphTrigger reports whether an identity belongs to graph advancement. The
// bands are disjoint and ordered, so a caller can tell the three apart.
func IsGraphTrigger(id int64) bool { return id > graphTriggerBase && id < automaticTriggerBase }

const graphDispatchColumns = `id,instance_id,session_id,node,due_at,status,allocated_run_id,reserved_at,finished_at,cancellation_reason,created_at`

// ParkGraphInstance records that an instance stopped mid-graph with work left,
// and schedules the run that will continue it. The instance keeps its current
// node, so the next run resumes exactly where this one stopped.
//
// A second park while one is open is not an error: the open dispatch already
// describes the same intent, and returning it keeps a retried turn idempotent.
func (db *DB) ParkGraphInstance(ctx context.Context, instanceID int64, node string, dueAt, now time.Time) (GraphDispatch, error) {
	var out GraphDispatch
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if err != nil {
			return err
		}
		if instance.Status != GraphRunning {
			return ErrGraphInstanceTerminal
		}
		if instance.CurrentNode != node {
			return graphInvariant("park disagrees with the instance's current node")
		}
		existing, err := scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running')`, instanceID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		out, err = scanGraphDispatch(tx.QueryRowContext(ctx,
			`INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at) VALUES(?,?,?,?,'pending',?) RETURNING `+graphDispatchColumns,
			instanceID, instance.SessionID, node, FormatTime(dueAt), FormatTime(now)))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(dueAt), instanceID)
		return err
	})
	return out, err
}

// ListDueGraphDispatches selects pending work whose time has come, oldest first.
func (db *DB) ListDueGraphDispatches(ctx context.Context, now time.Time, limit int) ([]GraphDispatch, error) {
	if limit <= 0 {
		return nil, graphInvariant("due dispatch limit must be positive")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE status='pending' AND due_at<=? ORDER BY due_at,id LIMIT ?`, FormatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due graph dispatches: %w", err)
	}
	defer rows.Close()
	out := make([]GraphDispatch, 0)
	for rows.Next() {
		v, e := scanGraphDispatch(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan due graph dispatch: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list due graph dispatches: %w", err)
	}
	return out, nil
}

// ReserveGraphDispatch claims one pending dispatch. Exactly one caller wins,
// which is what keeps two daemons, or two ticks, from starting the same stage.
func (db *DB) ReserveGraphDispatch(ctx context.Context, id int64, at time.Time) (GraphDispatch, bool, error) {
	var d GraphDispatch
	var ok bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='reserved', reserved_at=? WHERE id=? AND status='pending' AND due_at<=?`, FormatTime(at), id, FormatTime(at))
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		ok = n == 1
		if ok {
			d, err = scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, id))
		}
		return err
	})
	return d, ok, err
}

// RequeueGraphDispatch returns an unallocated reservation to pending. Losing a
// worker slot must not cost the instance its continuation.
func (db *DB) RequeueGraphDispatch(ctx context.Context, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending', reserved_at=NULL WHERE id=? AND status='reserved' AND allocated_run_id IS NULL`, id)
	return err
}

// FinishGraphDispatch closes a dispatch and clears the instance's due time. A
// still-running instance is parked again by the turn that advanced it, so
// clearing here cannot strand one.
func (db *DB) FinishGraphDispatch(ctx context.Context, id int64, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var instanceID int64
		if err := tx.QueryRowContext(ctx, `SELECT instance_id FROM graph_dispatches WHERE id=?`, id).Scan(&instanceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished', finished_at=? WHERE id=? AND status IN ('reserved','running')`, FormatTime(at), id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=? AND NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, instanceID, instanceID)
		return err
	})
}

// CancelGraphDispatch stops unallocated work for a stated reason. An allocated
// run is left alone: its own completion owns what happens next.
func (db *DB) CancelGraphDispatch(ctx context.Context, id int64, reason string, at time.Time) error {
	if reason == "" {
		return graphInvariant("cancelling a dispatch requires a reason")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var instanceID int64
		if err := tx.QueryRowContext(ctx, `SELECT instance_id FROM graph_dispatches WHERE id=?`, id).Scan(&instanceID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled', cancellation_reason=?, finished_at=? WHERE id=? AND status IN ('pending','reserved') AND allocated_run_id IS NULL`, reason, FormatTime(at), id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=? AND NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, instanceID, instanceID)
		return err
	})
}

func scanGraphDispatch(row scanner) (GraphDispatch, error) {
	var v GraphDispatch
	var due, created string
	var reserved, finished sql.NullString
	var allocated sql.NullInt64
	if err := row.Scan(&v.ID, &v.InstanceID, &v.SessionID, &v.Node, &due, &v.Status, &allocated, &reserved, &finished, &v.CancellationReason, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, ErrNotFound
		}
		return v, err
	}
	var err error
	if v.DueAt, err = ParseTime(due); err != nil {
		return v, err
	}
	if v.CreatedAt, err = ParseTime(created); err != nil {
		return v, err
	}
	if reserved.Valid {
		if v.ReservedAt, err = ParseTime(reserved.String); err != nil {
			return v, err
		}
	}
	if finished.Valid {
		if v.FinishedAt, err = ParseTime(finished.String); err != nil {
			return v, err
		}
	}
	v.AllocatedRunID = allocated.Int64
	return v, nil
}
