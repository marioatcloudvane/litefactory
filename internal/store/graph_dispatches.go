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
	LaunchID                  int64
	Node                      string
	DueAt                     time.Time
	Status                    string
	AllocatedRunID            int64
	ReservedAt, FinishedAt    time.Time
	CancellationReason        string
	Attempts                  int
	CreatedAt                 time.Time
}

// MaxGraphDispatchAttempts bounds how often one owed stage may fail to start.
// A transient failure deserves another tick; a malformed one will never get
// closer to running, and retrying it forever spends a forge observation a
// minute to learn nothing.
const MaxGraphDispatchAttempts = 3

// TriggerID returns the private execution identity for this dispatch.
func (d GraphDispatch) TriggerID() int64 { return graphTriggerBase + d.ID }

// IsGraphTrigger reports whether an identity belongs to graph advancement. The
// bands are disjoint and ordered, so a caller can tell the three apart.
func IsGraphTrigger(id int64) bool { return id > graphTriggerBase && id < automaticTriggerBase }

const graphDispatchColumns = `id,instance_id,session_id,node,due_at,status,allocated_run_id,reserved_at,finished_at,cancellation_reason,attempts,created_at,launch_id`

// GraphTriggerDispatch returns the dispatch a factory-private run identity was
// allocated under, and false for any identity that is not a graph dispatch's.
func GraphTriggerDispatch(id int64) (int64, bool) {
	if !IsGraphTrigger(id) {
		return 0, false
	}
	return id - graphTriggerBase, true
}

// ParkGraphInstance records that an instance stopped mid-graph with work left,
// and schedules the run that will continue it. The instance keeps its current
// node, so the next run resumes exactly where this one stopped.
//
// A second park while one is open is not an error: the open dispatch already
// describes the same intent, and returning it keeps a retried turn idempotent.
func (db *DB) ParkGraphInstance(ctx context.Context, instanceID int64, node string, dueAt, now time.Time) (GraphDispatch, error) {
	return db.ParkGraphInstanceAfter(ctx, instanceID, node, dueAt, now, 0)
}

// ParkGraphInstanceAfter parks an instance from inside the run that a dispatch
// allocated. That run's own dispatch is open until the run finishes, so a plain
// park would find it, treat it as the intent already recorded and schedule
// nothing: an instance with a third stage would then be stranded after its second.
// The spent dispatch is closed in the same transaction that schedules the next, so
// a crash between the two cannot lose the continuation. spent is zero for a run
// that no dispatch allocated.
func (db *DB) ParkGraphInstanceAfter(ctx context.Context, instanceID int64, node string, dueAt, now time.Time, spent int64) (GraphDispatch, error) {
	return db.parkGraphInstance(ctx, instanceID, node, dueAt, now, spent, 0, 0)
}

// ParkGraphInstanceForReply consumes an authenticated reply atomically with
// parking. The caller holds task ownership and freshly observes an open issue.
// Replayed replies cannot revive a later stop, even after a process interruption.
func (db *DB) ParkGraphInstanceForReply(ctx context.Context, instanceID int64, node string, at time.Time, replyID int64) (GraphDispatch, error) {
	if replyID <= 0 || IsPrivateTrigger(replyID) {
		return GraphDispatch{}, graphInvariant("recovery requires an owner comment")
	}
	return db.parkGraphInstance(ctx, instanceID, node, at, at, 0, replyID, 0)
}

// ParkCapturedGraphInstanceAfter retains the executing run's exact capture when
// scheduling its next stage, independently of later launches on the instance.
func (db *DB) ParkCapturedGraphInstanceAfter(ctx context.Context, instanceID int64, node string, dueAt, now time.Time, spent, launchID int64) (GraphDispatch, error) {
	if launchID <= 0 {
		return GraphDispatch{}, graphInvariant("parking execution requires exact capture")
	}
	return db.parkGraphInstance(ctx, instanceID, node, dueAt, now, spent, 0, launchID)
}

// GraphReplyResumeRefusal is the graph invariant a reply recovery meets when
// the stopped work cannot be resumed. Reason is factory-owned, owner-facing text.
type GraphReplyResumeRefusal struct{ Reason string }

func (e *GraphReplyResumeRefusal) Error() string {
	return ErrGraphInvariant.Error() + ": reply recovery refused: " + e.Reason
}

// Is keeps the refusal a graph invariant for callers that only test for one.
func (e *GraphReplyResumeRefusal) Is(target error) bool { return target == ErrGraphInvariant }

func (db *DB) parkGraphInstance(ctx context.Context, instanceID int64, node string, dueAt, now time.Time, spent, replyID, launchID int64) (GraphDispatch, error) {
	var out GraphDispatch
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if err != nil {
			return err
		}
		var capture sql.NullInt64
		switch {
		case launchID > 0:
			if err := tx.QueryRowContext(ctx, `SELECT id FROM research_launch_definitions WHERE id=? AND instance_id=?`, launchID, instanceID).Scan(&capture); err != nil {
				return err
			}
		case spent > 0:
			if err := tx.QueryRowContext(ctx, `SELECT launch_id FROM graph_dispatches WHERE id=? AND instance_id=?`, spent, instanceID).Scan(&capture); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		case replyID > 0:
			if err := tx.QueryRowContext(ctx, `SELECT launch_id FROM graph_dispatches WHERE instance_id=? ORDER BY id DESC LIMIT 1`, instanceID).Scan(&capture); err != nil {
				return err
			}
		default:
			err := tx.QueryRowContext(ctx, `SELECT id FROM research_launch_definitions WHERE instance_id=? ORDER BY id DESC LIMIT 1`, instanceID).Scan(&capture)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if replyID > 0 {
			var consumed bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM watermarks WHERE session_id=? AND last_comment_id>=?)`, instance.SessionID, replyID).Scan(&consumed); err != nil || consumed {
				return err
			}
			var launched, newer, running, stepping, waiting, stopped bool
			if err = tx.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM research_launch_definitions WHERE instance_id=?),
EXISTS(SELECT 1 FROM graph_instances WHERE session_id=? AND id>?),
EXISTS(SELECT 1 FROM runs WHERE session_id=? AND status='running'),
EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=? AND status IN ('pending','running')),
EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')),
EXISTS(SELECT 1 FROM graph_dispatches WHERE id=(SELECT MAX(id) FROM graph_dispatches WHERE instance_id=?) AND status='cancelled' AND allocated_run_id IS NULL AND node=?)`, instanceID, instance.SessionID, instanceID, instance.SessionID, instanceID, instanceID, instanceID, node).Scan(&launched, &newer, &running, &stepping, &waiting, &stopped); err != nil {
				return err
			}
			switch {
			case !stopped:
				return &GraphReplyResumeRefusal{Reason: "it is no longer stopped at that step"}
			case newer:
				return &GraphReplyResumeRefusal{Reason: "newer work on this issue has replaced it"}
			case waiting:
				return &GraphReplyResumeRefusal{Reason: "it is waiting for an answer to its open question"}
			case running:
				return &GraphReplyResumeRefusal{Reason: "another run on this issue is still in progress"}
			case stepping:
				return &GraphReplyResumeRefusal{Reason: "one of its steps is still in progress"}
			case !launched:
				return &GraphReplyResumeRefusal{Reason: "it has no saved launch to resume from"}
			case !instance.NextDueAt.IsZero():
				return &GraphReplyResumeRefusal{Reason: "it is already scheduled to continue"}
			}
		}
		if instance.Status != GraphRunning {
			return ErrGraphInstanceTerminal
		}
		if instance.CurrentNode != node {
			return graphInvariant("park disagrees with the instance's current node")
		}
		if spent > 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished', finished_at=? WHERE id=? AND instance_id=? AND status IN ('reserved','running')`, FormatTime(now), spent, instanceID); err != nil {
				return err
			}
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
			`INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id) VALUES(?,?,?,?,'pending',?,?) RETURNING `+graphDispatchColumns,
			instanceID, instance.SessionID, node, FormatTime(dueAt), FormatTime(now), capture))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(dueAt), instanceID)
		if err == nil && replyID > 0 {
			_, err = advanceWatermark(ctx, tx, instance.SessionID, replyID)
		}
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

// NextFreshGraphDispatch returns the oldest pending dispatch of a session that
// has never been attempted and is already due: a stage its instance parked for
// and is owed at once. A dispatch parked for later, such as an Implement
// coordinator retrying a merge, is left to the tick. A dispatch that failed to start keeps its back-off and is left to the
// tick.
func (db *DB) NextFreshGraphDispatch(ctx context.Context, sessionID int64) (GraphDispatch, bool, error) {
	row := db.sql.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE session_id=? AND status='pending' AND attempts=0 AND due_at<=? ORDER BY due_at,id LIMIT 1`, sessionID, FormatTime(nowUTC()))
	d, err := scanGraphDispatch(row)
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return GraphDispatch{}, false, nil
	}
	if err != nil {
		return GraphDispatch{}, false, fmt.Errorf("store: next fresh graph dispatch: %w", err)
	}
	return d, true, nil
}

// ReserveGraphDispatch claims one pending dispatch. Exactly one caller wins,
// which is what keeps two daemons, or two ticks, from starting the same stage.
func (db *DB) ReserveGraphDispatch(ctx context.Context, id int64, at time.Time) (GraphDispatch, bool, error) {
	var d GraphDispatch
	var ok bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='reserved', reserved_at=?, attempts=attempts+1 WHERE id=? AND status='pending' AND due_at<=?`, FormatTime(at), id, FormatTime(at))
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

// RequeueGraphDispatch returns an unallocated reservation to pending, due again
// at retryAt. Losing a worker slot must not cost the instance its continuation,
// so the work stays owed — but only while there is reason to think another
// attempt could start it. Past the cap it is cancelled with its own reason, and
// the instance keeps its node and findings either way.
func (db *DB) RequeueGraphDispatch(ctx context.Context, id int64, retryAt time.Time, cause string) (bool, error) {
	requeued, _, err := db.RequeueGraphDispatchWithStop(ctx, id, retryAt, cause)
	return requeued, err
}

// RequeueGraphDispatchWithStop returns the cancellation only when this call
// actually exhausts an unallocated reservation. A no-op is not a stop notice.
func (db *DB) RequeueGraphDispatchWithStop(ctx context.Context, id int64, retryAt time.Time, cause string) (bool, *GraphDispatch, error) {
	var requeued bool
	var stopped *GraphDispatch
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.Status != "reserved" || d.AllocatedRunID != 0 {
			return nil
		}
		if d.Attempts >= MaxGraphDispatchAttempts {
			reason := fmt.Sprintf("graph stage %q could not start in %d attempts", d.Node, d.Attempts)
			if cause != "" {
				reason += ": " + cause
			}
			if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled', cancellation_reason=?, finished_at=? WHERE id=?`, reason, FormatTime(retryAt), id); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, d.InstanceID)
			d.Status, d.CancellationReason, d.FinishedAt = "cancelled", reason, retryAt
			stopped = &d
			return err
		}
		requeued = true
		if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending', reserved_at=NULL, due_at=? WHERE id=?`, FormatTime(retryAt), id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(retryAt), d.InstanceID)
		return err
	})
	return requeued, stopped, err
}

// GraphDispatchRetryDelay spaces a failed start out instead of hammering the
// next tick. It grows with the attempts already spent.
func GraphDispatchRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	return time.Duration(attempts) * 5 * time.Minute
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
	var allocated, launch sql.NullInt64
	if err := row.Scan(&v.ID, &v.InstanceID, &v.SessionID, &v.Node, &due, &v.Status, &allocated, &reserved, &finished, &v.CancellationReason, &v.Attempts, &created, &launch); err != nil {
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
	v.LaunchID = scanID(launch)
	return v, nil
}

// bindGraphDispatch claims the dispatch in the transaction that starts execution.
// A stale or foreign reservation must roll back the run's launch as well.
func bindGraphDispatch(ctx context.Context, tx *sql.Tx, run Run) error {
	id, ok := GraphTriggerDispatch(run.TriggerCommentID)
	if !ok {
		return graphInvariant("graph run requires a graph trigger")
	}
	res, err := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='running', allocated_run_id=? WHERE id=? AND session_id=? AND status='reserved' AND allocated_run_id IS NULL`, run.ID, id, run.SessionID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return graphInvariant("graph run must be allocated to its own reserved dispatch")
	}
	return nil
}

// LatestGraphDispatch reads the latest intent without interpreting its reason.
func (db *DB) LatestGraphDispatch(ctx context.Context, instanceID int64) (GraphDispatch, error) {
	return scanGraphDispatch(db.sql.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE instance_id=? ORDER BY id DESC LIMIT 1`, instanceID))
}

// ValidateGraphDispatchTarget fences dispatch execution under the caller's task
// lock. A stale dispatch must never execute a newer instance selected by a mode.
func (db *DB) ValidateGraphDispatchTarget(ctx context.Context, sessionID, triggerID int64) error {
	id, ok := GraphTriggerDispatch(triggerID)
	if !ok {
		return nil
	}
	var valid bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_dispatches d JOIN graph_instances i ON i.id=d.instance_id WHERE d.id=? AND d.session_id=? AND d.status='reserved' AND d.allocated_run_id IS NULL AND i.status='running' AND i.current_node=d.node AND NOT EXISTS(SELECT 1 FROM graph_instances newer WHERE newer.session_id=i.session_id AND newer.id>i.id))`, id, sessionID).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return graphInvariant("dispatch no longer targets the current saved node")
	}
	return nil
}

// GraphDispatchFollowsCancellation identifies a fresh replacement using the
// retained prior intent, without adding recovery state or rewriting history.
func (db *DB) GraphDispatchFollowsCancellation(ctx context.Context, dispatch GraphDispatch) (bool, error) {
	var recovered bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_dispatches WHERE id=(SELECT MAX(id) FROM graph_dispatches WHERE instance_id=? AND id<?) AND status='cancelled' AND allocated_run_id IS NULL AND node=?)`, dispatch.InstanceID, dispatch.ID, dispatch.Node).Scan(&recovered)
	return recovered, err
}

// WakeGraphInstance makes a resting instance owed its next run now. It acts only
// on a running instance that rests on node with no attempt in flight, and it
// never adds a second open dispatch: an open pending one is brought forward, one
// already reserved or running is returned as it is, and only an instance with
// none gets a new one. However many wakes arrive, exactly one dispatch is open. A
// terminal or moved-on instance is reported as not woken.
func (db *DB) WakeGraphInstance(ctx context.Context, instanceID int64, node string, at time.Time) (woken bool, err error) {
	if instanceID < 1 || node == "" || at.IsZero() {
		return false, graphInvariant("invalid graph wake")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning || instance.CurrentNode != node {
			return nil
		}
		var owner int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')`, instanceID).Scan(&owner); e != nil || owner > 0 {
			return e
		}
		stamp := FormatTime(at)
		res, e := tx.ExecContext(ctx, `UPDATE graph_dispatches SET due_at=? WHERE instance_id=? AND status='pending' AND due_at>?`, stamp, instanceID, stamp)
		if e != nil {
			return e
		}
		brought, e := res.RowsAffected()
		if e != nil {
			return e
		}
		var open int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running')`, instanceID).Scan(&open); e != nil {
			return e
		}
		if open == 0 {
			if _, e = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id) VALUES(?,?,?,?,'pending',?,(SELECT id FROM research_launch_definitions WHERE instance_id=? ORDER BY id DESC LIMIT 1))`, instanceID, instance.SessionID, node, stamp, stamp, instanceID); e != nil {
				return e
			}
			brought = 1
		}
		if brought > 0 {
			if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, stamp, instanceID); e != nil {
				return e
			}
			woken = true
		}
		return nil
	})
	return woken, err
}

// ListInterruptedGraphDispatches finds allocated ownership whose run has ended
// or is missing. Selection grants no retry authority; settlement rechecks it.
func (db *DB) ListInterruptedGraphDispatches(ctx context.Context) ([]GraphDispatch, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE status='running' AND allocated_run_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM runs WHERE id=allocated_run_id AND status IN ('prelaunch','running')) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphDispatch
	for rows.Next() {
		d, err := scanGraphDispatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ReconcileGraphDispatch closes interrupted ownership without rewriting run
// history. The caller holds task exclusion and validates the exact capture before
// permitting a successor. Attempts and effects belong to their recovery owners.
func (db *DB) ReconcileGraphDispatch(ctx context.Context, id int64, captureID int64, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.Status != "running" || d.AllocatedRunID == 0 {
			return nil
		}
		var protected bool
		err = tx.QueryRowContext(ctx, `SELECT
EXISTS(SELECT 1 FROM runs WHERE (id=? OR session_id=?) AND status IN ('prelaunch','running')) OR
EXISTS(SELECT 1 FROM doctor_held_session_owners WHERE session_id=?) OR
EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=? AND status IN ('pending','running')) OR
EXISTS(SELECT 1 FROM graph_effects WHERE instance_id=? AND state IN ('intent','dispatched','uncertain'))`, d.AllocatedRunID, d.SessionID, d.SessionID, d.InstanceID, d.InstanceID).Scan(&protected)
		if err != nil || protected {
			return err
		}
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, d.InstanceID))
		if err != nil {
			return err
		}
		var waiting, superseded bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=? AND state IN ('waiting','claimed')), EXISTS(SELECT 1 FROM graph_instances WHERE session_id=? AND id>?)`, instance.ID, instance.SessionID, instance.ID).Scan(&waiting, &superseded)
		if err != nil {
			return err
		}
		retry := instance.Status == GraphRunning && !waiting && !superseded
		if retry && (captureID == 0 || captureID != d.LaunchID) {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE id=?`, FormatTime(at), d.ID); err != nil {
			return err
		}
		if !retry {
			_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, instance.ID)
			return err
		}
		due := instance.NextDueAt
		if due.IsZero() {
			due = at
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id) VALUES(?,?,?,?,'pending',?,?)`, instance.ID, instance.SessionID, instance.CurrentNode, FormatTime(due), FormatTime(at), captureID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(due), instance.ID)
		return err
	})
}
