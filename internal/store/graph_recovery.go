package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HasWorkflowGraphRun distinguishes internal graph invocations from legacy
// whole-turn publication. It includes terminal instances and attempts.
func (db *DB) HasWorkflowGraphRun(ctx context.Context, runID int64, mode string) (bool, error) {
	var found bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE a.run_id=? AND d.mode=?)`, runID, mode).Scan(&found)
	return found, err
}

// HasWorkflowGraphSession excludes graph work from legacy automatic quota
// continuation, including failed instances which require fresh owner authority.
func (db *DB) HasWorkflowGraphSession(ctx context.Context, sessionID int64, mode string) (bool, error) {
	var found bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode=?)`, sessionID, mode).Scan(&found)
	return found, err
}

// ReconcileWorkflowGraphs runs only at daemon startup after orphan runs have
// been settled. Unallocated owner claims and authored results are untouched.
// An allocated but unretained attempt is failed, never replayed. Existing run
// causes, outputs and publication identities are retained without modification.
func (db *DB) ReconcileWorkflowGraphs(ctx context.Context, mode string, at time.Time) (count int, err error) {
	if mode == "" || at.IsZero() {
		return 0, graphInvariant("reconciliation requires a timestamp")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT a.id,a.instance_id,i.status,COALESCE(r.error,''),a.started_at FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id LEFT JOIN runs r ON r.id=a.run_id WHERE d.mode=? AND i.session_id NOT IN (SELECT session_id FROM doctor_held_session_owners) AND a.status IN ('pending','running') ORDER BY a.id`, mode)
		if e != nil {
			return e
		}
		type interrupted struct {
			id, instance            int64
			status, reason, started string
		}
		var attempts []interrupted
		for rows.Next() {
			var a interrupted
			if e = rows.Scan(&a.id, &a.instance, &a.status, &a.reason, &a.started); e != nil {
				break
			}
			attempts = append(attempts, a)
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return e
		}
		for _, a := range attempts {
			reason := a.reason
			if reason == "" {
				reason = "interrupted before validated graph result retention; explicit owner continuation required"
			}
			finished := FormatTime(at)
			if finished < a.started {
				finished = a.started
			}
			// An attempt left open by an instance that already finished is
			// cancelled: it can no longer run and must not block a successor.
			status := "failed"
			if a.status != GraphRunning {
				status = "cancelled"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status=?,outcome=NULL,finished_at=? WHERE id=? AND status IN ('pending','running')`, status, finished, a.id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'diagnostic',?,?)`, a.id, []byte(reason), finished); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',next_due_at=NULL,finished_at=? WHERE id=? AND status='running'`, finished, a.instance); e != nil {
				return e
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("store: reconcile workflow graphs: %w", err)
	}
	return count, nil
}

// CancelWorkflowGraphs settles active/paused execution while retaining all
// findings, owner claims and owed/uncertain publications. It never alters runs.
func (db *DB) CancelWorkflowGraphs(ctx context.Context, sessionID int64, mode, reason string, at time.Time) error {
	if mode == "" || sessionID < 1 || strings.TrimSpace(reason) == "" || at.IsZero() {
		return graphInvariant("invalid workflow cancellation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error { return cancelWorkflowGraphsTx(ctx, tx, sessionID, mode, reason, at) })
}

func cancelWorkflowGraphsTx(ctx context.Context, tx *sql.Tx, sessionID int64, mode, reason string, at time.Time) error {
	return cancelWorkflowGraphsFilteredTx(ctx, tx, sessionID, mode, reason, at, false)
}

func cancelWorkflowGraphsFilteredTx(ctx context.Context, tx *sql.Tx, sessionID int64, mode, reason string, at time.Time, preserveWaits bool) error {
	stamp := FormatTime(at)
	// Closure must not bypass the idle-owner-wait guards below.
	filter := ""
	if preserveWaits {
		filter = " AND NOT EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state IN ('waiting','claimed'))"
	}
	// Save cause before terminalizing attempts. Append-only output keeps original
	// diagnostics and authored results independently recoverable.
	_, err := tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) SELECT a.id,'diagnostic',?,MAX(?,a.started_at) FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode=? AND a.status IN ('running','pending')`+filter, []byte(reason), stamp, sessionID, mode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',outcome=NULL,finished_at=MAX(?,started_at) WHERE status IN ('running','pending') AND instance_id IN (SELECT i.id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode=?`+filter+`)`, stamp, sessionID, mode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_instances AS i SET status='cancelled',next_due_at=NULL,finished_at=MAX(?,started_at) WHERE session_id=? AND status='running' AND definition_id IN (SELECT id FROM graph_definitions WHERE mode=?)`+filter, stamp, sessionID, mode)
	return err
}

// ReconcileResearchGraphs settles interrupted Research activity attempts after restart.
func (db *DB) ReconcileResearchGraphs(ctx context.Context, at time.Time) (int, error) {
	return db.ReconcileWorkflowGraphs(ctx, "research", at)
}

// CancelResearchGraphs cancels active Research execution while retaining its cause.
func (db *DB) CancelResearchGraphs(ctx context.Context, sessionID int64, reason string, at time.Time) error {
	return db.CancelWorkflowGraphs(ctx, sessionID, "research", reason, at)
}
func cancelResearchGraphsTx(ctx context.Context, tx *sql.Tx, sessionID int64, reason string, at time.Time) error {
	return cancelWorkflowGraphsTx(ctx, tx, sessionID, "research", reason, at)
}

// CancelClosedIssueGraphs settles idle owner waits across workflows and retains
// the existing non-wait Research/Spike closure cleanup. The caller holds the
// session lock; all eligibility checks and graph writes share one transaction.
func (db *DB) CancelClosedIssueGraphs(ctx context.Context, sessionID int64, at time.Time) error {
	if sessionID < 1 || at.IsZero() {
		return graphInvariant("invalid issue closure cancellation")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var protected bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE session_id=? AND status IN ('prelaunch','running')) OR EXISTS(SELECT 1 FROM doctor_held_session_owners WHERE session_id=?)`, sessionID, sessionID).Scan(&protected); err != nil {
			return err
		}
		if protected {
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances i
WHERE session_id=? AND status='running'
AND EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state IN ('waiting','claimed'))
AND NOT EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=i.id AND a.status IN ('pending','running'))
AND NOT EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.instance_id=i.id AND d.allocated_run_id IS NOT NULL AND d.status IN ('pending','reserved','running'))
ORDER BY id`, sessionID)
		if err != nil {
			return err
		}
		var instances []GraphInstance
		for rows.Next() {
			var instance GraphInstance
			instance, err = scanGraphInstance(rows)
			if err != nil {
				break
			}
			instances = append(instances, instance)
		}
		if err = errors.Join(err, rows.Err(), rows.Close()); err != nil {
			return err
		}
		const reason = "issue closed by owner"
		for _, instance := range instances {
			finished := at
			if finished.Before(instance.StartedAt) {
				finished = instance.StartedAt
			}
			if _, err = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='cancelled' WHERE instance_id=? AND state IN ('waiting','claimed')`, instance.ID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=MAX(?,created_at),cancellation_reason=? WHERE instance_id=? AND status IN ('pending','reserved') AND allocated_run_id IS NULL`, FormatTime(finished), reason, instance.ID); err != nil {
				return err
			}
			if err = setGraphInstanceTx(ctx, tx, instance.ID, instance.CurrentNode, GraphCancelled, time.Time{}, finished); err != nil {
				return err
			}
			if _, err = recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: instance.ID, FromNode: instance.CurrentNode, ToNode: instance.CurrentNode, OnOutcome: reason, At: finished}); err != nil {
				return err
			}
		}
		for _, mode := range []string{"research", "spike"} {
			if err = cancelWorkflowGraphsFilteredTx(ctx, tx, sessionID, mode, "issue closed", at, true); err != nil {
				return err
			}
		}
		return nil
	})
}

// CancelIdleGraphOwnerWait ends one running instance that is idle at an owner
// wait: its wait and unallocated dispatches are cancelled and the instance is
// cancelled with the reason in its trace. It reports false and changes nothing
// when the instance is not running, has no waiting question, has an attempt in
// progress or has a dispatch already allocated to a run. A claimedReply above
// zero also accepts a wait claimed by exactly that reply, which is cancelled too.
func (db *DB) CancelIdleGraphOwnerWait(ctx context.Context, instanceID, claimedReply int64, reason string, at time.Time) (cancelled bool, err error) {
	if instanceID < 1 || strings.TrimSpace(reason) == "" || at.IsZero() {
		return false, graphInvariant("invalid owner wait cancellation")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		instance, e := scanGraphInstance(tx.QueryRowContext(ctx, `SELECT `+graphInstanceColumns+` FROM graph_instances i
WHERE id=? AND status='running'
AND EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND (w.state='waiting' OR (?>0 AND w.state='claimed' AND w.reply_id=?)))
AND NOT EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=i.id AND a.status IN ('pending','running'))
AND NOT EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.instance_id=i.id AND d.allocated_run_id IS NOT NULL AND d.status IN ('pending','reserved','running'))`, instanceID, claimedReply, claimedReply))
		if errors.Is(e, ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		finished := at
		if finished.Before(instance.StartedAt) {
			finished = instance.StartedAt
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state='cancelled' WHERE instance_id=? AND (state='waiting' OR (?>0 AND state='claimed' AND reply_id=?))`, instance.ID, claimedReply, claimedReply); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=MAX(?,created_at),cancellation_reason=? WHERE instance_id=? AND status IN ('pending','reserved') AND allocated_run_id IS NULL`, FormatTime(finished), reason, instance.ID); e != nil {
			return e
		}
		if e = setGraphInstanceTx(ctx, tx, instance.ID, instance.CurrentNode, GraphCancelled, time.Time{}, finished); e != nil {
			return e
		}
		if _, e = recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: instance.ID, FromNode: instance.CurrentNode, ToNode: instance.CurrentNode, OnOutcome: reason, At: finished}); e != nil {
			return e
		}
		cancelled = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: cancel idle owner wait: %w", err)
	}
	return cancelled, nil
}
