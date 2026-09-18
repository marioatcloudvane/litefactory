package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const graphAttemptColumns = `id,instance_id,node,attempt,run_id,status,outcome,started_at,finished_at`

// StartGraphNodeAttempt reserves MAX(attempt)+1 in a short transaction. Every
// dispatch consumes an identity, including successful loop re-entry. A stale
// running attempt refuses new work until explicitly finalized; this accessor
// does not detect crashes or reconcile uncertain external effects.
func (db *DB) StartGraphNodeAttempt(ctx context.Context, in GraphAttemptStart) (out GraphNodeAttempt, err error) {
	if in.InstanceID <= 0 || in.Node == "" || in.Limit < 0 || in.RunID < 0 || in.StartedAt.IsZero() {
		return out, graphInvariant("invalid attempt reservation")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, in.InstanceID))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning {
			return ErrGraphInstanceTerminal
		}
		if instance.CurrentNode != in.Node {
			return graphInvariant("attempt node differs from current node")
		}
		if in.RunID != 0 {
			var sessionID int64
			if e = tx.QueryRowContext(ctx, `SELECT session_id FROM runs WHERE id=?`, in.RunID).Scan(&sessionID); e != nil {
				return e
			}
			if sessionID != instance.SessionID {
				return graphInvariant("attempt run belongs to another session")
			}
		}
		var maximum, running int
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(attempt),0),COALESCE(SUM(status='running'),0) FROM graph_node_attempts WHERE instance_id=? AND node=?`, in.InstanceID, in.Node).Scan(&maximum, &running); e != nil {
			return e
		}
		if running > 0 {
			return ErrGraphAttemptRunning
		}
		if in.Limit > 0 && maximum >= in.Limit {
			return ErrGraphAttemptLimit
		}
		out, e = scanGraphAttempt(tx.QueryRowContext(ctx, `INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,started_at) VALUES(?,?,?,?,?,?) RETURNING `+graphAttemptColumns, in.InstanceID, in.Node, maximum+1, nullID(in.RunID), GraphRunning, FormatTime(in.StartedAt)))
		return e
	})
	if err != nil {
		return GraphNodeAttempt{}, fmt.Errorf("store: start graph node attempt: %w", err)
	}
	return out, nil
}

// GetGraphNodeAttempt loads a single immutable attempt identity.
func (db *DB) GetGraphNodeAttempt(ctx context.Context, id int64) (GraphNodeAttempt, error) {
	out, err := scanGraphAttempt(db.sql.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, id))
	if err != nil {
		return GraphNodeAttempt{}, fmt.Errorf("store: get graph node attempt: %w", err)
	}
	return out, nil
}

// ListGraphNodeAttempts returns all dispatches in insertion order.
func (db *DB) ListGraphNodeAttempts(ctx context.Context, instanceID int64) ([]GraphNodeAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("store: list graph node attempts: %w", err)
	}
	defer rows.Close()
	out := make([]GraphNodeAttempt, 0)
	for rows.Next() {
		v, e := scanGraphAttempt(rows)
		if e != nil {
			return nil, fmt.Errorf("store: scan graph node attempt: %w", e)
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list graph node attempts: %w", err)
	}
	return out, nil
}

// FinishGraphNodeAttempt finalizes only an attempt. This explicit seam may be
// used after an operator has reconciled stale work; it provides no automatic
// recovery guarantee. Normal graph execution uses CompleteGraphNodeAttempt.
func (db *DB) FinishGraphNodeAttempt(ctx context.Context, id int64, status, outcome string, finished time.Time) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error { _, e := finishGraphAttemptTx(ctx, tx, id, status, outcome, finished); return e })
	if err != nil {
		return fmt.Errorf("store: finish graph node attempt: %w", err)
	}
	return nil
}

func finishGraphAttemptTx(ctx context.Context, tx *sql.Tx, id int64, status, outcome string, finished time.Time) (GraphNodeAttempt, error) {
	if !graphTerminal(status) || (outcome != "" && status != GraphSucceeded) || finished.IsZero() {
		return GraphNodeAttempt{}, graphInvariant("invalid attempt completion status, outcome or finish time")
	}
	old, err := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, id))
	if err != nil {
		return old, err
	}
	if graphTerminal(old.Status) {
		return old, ErrGraphAttemptTerminal
	}
	if finished.Before(old.StartedAt) {
		return old, graphInvariant("attempt finish precedes start")
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status=?,outcome=?,finished_at=? WHERE id=?`, status, nullString(outcome), FormatTime(finished), id)
	return old, err
}

// CompleteGraphNodeAttempt atomically finishes the attempt and writes its
// outputs, optional transition and instance progress. Dispatch must occur
// before this call with no SQL transaction open. A refused outcome uses failed
// status and no transition, preserving current_node and prior history.
func (db *DB) CompleteGraphNodeAttempt(ctx context.Context, id int64, in GraphCompletion) error {
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		old, e := finishGraphAttemptTx(ctx, tx, id, in.Status, in.Outcome, in.FinishedAt)
		if e != nil {
			return e
		}
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, old.InstanceID))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning {
			return ErrGraphInstanceTerminal
		}
		if instance.CurrentNode != old.Node {
			return graphInvariant("attempt no longer owns current node")
		}
		nextNode := old.Node
		if in.Transition != nil {
			t := *in.Transition
			if in.Status != GraphSucceeded || in.Outcome == "" || t.InstanceID != old.InstanceID || t.FromNode != old.Node || t.OnOutcome != in.Outcome {
				return graphInvariant("transition disagrees with completed attempt")
			}
			if _, e = recordGraphTransitionTx(ctx, tx, t); e != nil {
				return e
			}
			nextNode = t.ToNode
		}
		for _, output := range in.Outputs {
			if output.NodeAttemptID != 0 && output.NodeAttemptID != id {
				return graphInvariant("output belongs to another attempt")
			}
			output.NodeAttemptID = id
			if output.CreatedAt.IsZero() {
				output.CreatedAt = in.FinishedAt
			}
			if _, e = recordGraphOutputTx(ctx, tx, output); e != nil {
				return e
			}
		}
		status := in.InstanceStatus
		if status == "" {
			status = GraphRunning
		}
		var finished time.Time
		if graphTerminal(status) {
			finished = in.FinishedAt
		}
		return setGraphInstanceTx(ctx, tx, old.InstanceID, nextNode, status, in.NextDueAt, finished)
	})
	if err != nil {
		return fmt.Errorf("store: complete graph node attempt: %w", err)
	}
	return nil
}

func scanGraphAttempt(row scanner) (GraphNodeAttempt, error) {
	var v GraphNodeAttempt
	var run sql.NullInt64
	var outcome, finished sql.NullString
	var started string
	if err := row.Scan(&v.ID, &v.InstanceID, &v.Node, &v.Attempt, &run, &v.Status, &outcome, &started, &finished); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return v, ErrNotFound
		}
		return v, err
	}
	v.RunID = scanID(run)
	v.Outcome = outcome.String
	var err error
	if v.StartedAt, err = ParseTime(started); err != nil {
		return v, err
	}
	v.FinishedAt, err = scanTime(finished)
	return v, err
}
