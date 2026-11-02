package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const graphCapacityDeferral = `{"capacity_deferred":true}`

// DeferGraphNodeAttempt retains an unfinished activity after its started nested
// work has joined and cleaned up. Capacity waiting does not finish the attempt
// or advance its counters, outputs or current node.
func (db *DB) DeferGraphNodeAttempt(ctx context.Context, id int64, at time.Time) error {
	if id <= 0 || at.IsZero() {
		return graphInvariant("invalid capacity deferral")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		attempt, err := scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE id=?`, id))
		if err != nil {
			return err
		}
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, attempt.InstanceID))
		if err != nil {
			return err
		}
		if instance.Status != GraphRunning || instance.CurrentNode != attempt.Node || attempt.Status != GraphRunning || at.Before(attempt.StartedAt) {
			return graphInvariant("capacity deferral does not own the current running attempt")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'json',?,?)`, id, []byte(graphCapacityDeferral), FormatTime(at)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='pending' WHERE id=?`, id); err != nil {
			return err
		}
		// A first-entry turn may not own a graph dispatch yet. Retain its exact
		// continuation with the pending attempt so a crash before the driver's
		// subsequent park cannot strand completed nested results.
		if attempt.LaunchID > 0 {
			if _, err = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id)
SELECT ?,?,?,?,'pending',?,? WHERE NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, instance.ID, instance.SessionID, attempt.Node, FormatTime(at), FormatTime(at), attempt.LaunchID, instance.ID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(at), instance.ID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: defer graph node attempt: %w", err)
	}
	return nil
}

// ResumeDeferredGraphNodeAttempt readmits the latest pending attempt at the
// current node. The caller must acquire capacity first. A running or completed
// attempt is never reclaimed here; crash reconciliation retains its ownership.
// ErrNotFound means there is no yielded attempt and a fresh attempt may start.
func (db *DB) ResumeDeferredGraphNodeAttempt(ctx context.Context, instanceID int64, node string, at time.Time) (out GraphNodeAttempt, err error) {
	if instanceID <= 0 || node == "" || at.IsZero() {
		return out, graphInvariant("invalid capacity resume")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, instanceID))
		if e != nil {
			return e
		}
		if instance.Status != GraphRunning || instance.CurrentNode != node {
			return graphInvariant("capacity resume differs from current running node")
		}
		out, e = scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND node=? ORDER BY attempt DESC LIMIT 1`, instanceID, node))
		if e != nil {
			return e
		}
		var yielded bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_outputs WHERE node_attempt_id=? AND kind='json' AND payload=?)`, out.ID, []byte(graphCapacityDeferral)).Scan(&yielded); e != nil {
			return e
		}
		if out.Status != GraphPending || !yielded {
			return ErrNotFound
		}
		if at.Before(out.StartedAt) {
			return graphInvariant("capacity resume precedes attempt start")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='running' WHERE id=?`, out.ID); e != nil {
			return e
		}
		out.Status = GraphRunning
		return nil
	})
	if err != nil {
		return GraphNodeAttempt{}, fmt.Errorf("store: resume deferred graph node attempt: %w", err)
	}
	return out, nil
}

// DeferGraphDispatchForCapacity retains owed work after a capacity-only refusal.
// No retry allowance is spent. Allocated run identities stay on their completed
// dispatch; an unallocated reservation is returned to pending in place.
func (db *DB) DeferGraphDispatchForCapacity(ctx context.Context, id int64, at time.Time) error {
	if id <= 0 || at.IsZero() {
		return graphInvariant("invalid capacity dispatch deferral")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanGraphDispatch(tx.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.Status != "reserved" && d.Status != "running" {
			return graphInvariant("capacity dispatch is not owned")
		}
		instance, err := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances WHERE id=?`, d.InstanceID))
		if err != nil {
			return err
		}
		if instance.Status != GraphRunning {
			return graphInvariant("capacity dispatch differs from current running node")
		}
		// One dispatch can run several factory nodes back to back. When the
		// node that defers is not the dispatch's own, that stage finished and
		// the owed work belongs to the node the instance now rests on.
		advanced := instance.CurrentNode != d.Node
		var live bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE session_id=? AND status IN ('prelaunch','running')) OR EXISTS(SELECT 1 FROM graph_node_attempts WHERE instance_id=? AND status='running')`, d.SessionID, d.InstanceID).Scan(&live); err != nil {
			return err
		}
		if live {
			return graphInvariant("capacity dispatch still has executing work")
		}
		switch {
		case advanced:
			if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE id=?`, FormatTime(at), d.ID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id)
SELECT ?,?,?,?,'pending',?,? WHERE NOT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, d.InstanceID, d.SessionID, instance.CurrentNode, FormatTime(at), FormatTime(at), nullID(d.LaunchID), d.InstanceID)
		case d.AllocatedRunID == 0:
			_, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending',reserved_at=NULL,due_at=?,attempts=MAX(0,attempts-1) WHERE id=?`, FormatTime(at), d.ID)
		default:
			if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE id=?`, FormatTime(at), d.ID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,status,created_at,launch_id) VALUES(?,?,?,?,'pending',?,?)`, d.InstanceID, d.SessionID, d.Node, FormatTime(at), FormatTime(at), nullID(d.LaunchID))
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET next_due_at=? WHERE id=?`, FormatTime(at), d.InstanceID)
		return err
	})
}
