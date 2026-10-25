package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RedirectGraphBugFixerToSync routes a pending fixer through its factory-resolved
// prerequisite. It keeps the original authenticated claim and all attempt counts.
func (db *DB) RedirectGraphBugFixerToSync(ctx context.Context, instanceID int64, gate BugSyncPrerequisite, at time.Time) error {
	if instanceID < 1 || gate.SyncNode == "" || gate.FixerNode == "" || gate.SyncNode == gate.FixerNode || at.IsZero() {
		return graphInvariant("invalid Bug sync prerequisite")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=? AND i.status='running' AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2 AND NOT EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=i.id AND a.status='running') AND NOT EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state='waiting'))`, instanceID, gate.FixerNode).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return graphInvariant("Bug fixer no longer owns prerequisite reroute")
		}
		if _, err := recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: instanceID, FromNode: gate.FixerNode, ToNode: gate.SyncNode, OnOutcome: "sync_required", At: at}); err != nil {
			return err
		}
		return setGraphInstanceTx(ctx, tx, instanceID, gate.SyncNode, GraphRunning, at, time.Time{})
	})
}

// GraphBugMechanicalAllowance reads the same unconsumed continuation that
// reservation verifies and consumes atomically. It grants no authority itself.
func (db *DB) GraphBugMechanicalAllowance(ctx context.Context, instanceID int64, node string, gate *BugSyncPrerequisite) (source int64, err error) {
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT continuation_attempt_id FROM graph_owner_waits WHERE instance_id=? AND state='allocated' AND allowance_attempt_id IS NULL AND continuation_attempt_id IS NOT NULL ORDER BY rowid DESC`, instanceID)
		if e != nil {
			return e
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			ok, e := graphMechanicalAllowanceTx(ctx, tx, GraphAttemptStart{InstanceID: instanceID, Node: node, RunID: 1, OwnerAllowanceSource: id, BugSyncPrerequisite: gate})
			if e != nil {
				return e
			}
			if ok {
				source = id
				break
			}
		}
		return nil
	})
	return
}

func graphBugSyncAllowanceTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart) (bool, error) {
	gate := in.BugSyncPrerequisite
	pending := gate != nil && in.Node == gate.SyncNode
	if gate == nil || gate.SyncNode == "" || (gate.FixerNode != in.Node && !pending) || in.RunID < 1 || in.OwnerAllowanceSource < 1 {
		return false, nil
	}
	var origin string
	err := tx.QueryRowContext(ctx, `SELECT w.origin_node FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE w.instance_id=? AND w.state='allocated' AND w.continuation_attempt_id=? AND w.allowance_attempt_id IS NULL AND (w.allow_runless=1 OR w.origin_node=?) AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2`, in.InstanceID, in.OwnerAllowanceSource, gate.FixerNode, in.Node).Scan(&origin)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND id>=? ORDER BY id`, in.InstanceID, in.OwnerAllowanceSource)
	if err != nil {
		return false, err
	}
	var attempts []GraphNodeAttempt
	for rows.Next() {
		a, e := scanGraphAttempt(rows)
		if e != nil {
			_ = rows.Close()
			return false, e
		}
		attempts = append(attempts, a)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	if len(attempts) == 0 || attempts[0].ID != in.OwnerAllowanceSource {
		return false, nil
	}
	for idx, a := range attempts {
		if a.RunID != 0 || a.Status != GraphSucceeded {
			return false, nil
		}
		next := gate.SyncNode
		if a.Node == gate.SyncNode {
			permitted := a.Outcome == "synced" || a.Outcome == "current" || gate.AllowDeferred && a.Outcome == "deferred"
			if idx == len(attempts)-1 && !pending {
				if !permitted {
					return false, nil
				}
				next = gate.FixerNode
			} else if a.Outcome != "retry" {
				if !permitted {
					return false, nil
				}
				var recovered bool
				var upper string
				if idx+1 < len(attempts) {
					upper = FormatTime(attempts[idx+1].StartedAt)
				}
				if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_transitions WHERE instance_id=? AND from_node=? AND to_node=? AND on_outcome='sync_required' AND at>=? AND at<=COALESCE(?,at))`, in.InstanceID, gate.FixerNode, gate.SyncNode, FormatTime(a.FinishedAt), nullString(upper)).Scan(&recovered); err != nil {
					return false, err
				}
				if !recovered {
					return false, nil
				}
				next = gate.FixerNode
			}
		} else if idx != 0 || origin == gate.FixerNode || (a.Outcome != "approved" && a.Outcome != "revalidate") {
			return false, nil
		}
		if idx+1 < len(attempts) && attempts[idx+1].Node != gate.SyncNode {
			return false, nil
		}
		var linked bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_transitions WHERE instance_id=? AND from_node=? AND to_node=? AND on_outcome=? AND at=?)`, in.InstanceID, a.Node, next, a.Outcome, FormatTime(a.FinishedAt)).Scan(&linked); err != nil {
			return false, err
		}
		if !linked {
			return false, nil
		}
	}
	return pending || attempts[len(attempts)-1].Node == gate.SyncNode, nil
}

// GraphBugSyncEntryAllowance verifies existing repair eligibility before a sync
// dispatch when the ordinary fixer budget has already been spent. A sync reply
// retains its authenticated continuation; the sync result alone grants none.
func (db *DB) GraphBugSyncEntryAllowance(ctx context.Context, instanceID int64, gate BugSyncPrerequisite) (allowed bool, err error) {
	if instanceID < 1 || gate.SyncNode == "" || gate.FixerNode == "" || gate.SyncNode == gate.FixerNode {
		return false, graphInvariant("invalid Bug sync prerequisite")
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var valid bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=? AND i.status='running' AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2)`, instanceID, gate.SyncNode).Scan(&valid); e != nil {
			return e
		}
		if !valid {
			return nil
		}
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_owner_waits WHERE instance_id=? AND state='claimed' AND origin_node IN (?,?))`, instanceID, gate.FixerNode, gate.SyncNode).Scan(&allowed); e != nil {
			return e
		}
		if allowed {
			return nil
		}
		rows, e := tx.QueryContext(ctx, `SELECT continuation_attempt_id FROM graph_owner_waits WHERE instance_id=? AND state='allocated' AND allowance_attempt_id IS NULL AND continuation_attempt_id IS NOT NULL ORDER BY rowid DESC`, instanceID)
		if e != nil {
			return e
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			allowed, e = graphBugSyncAllowanceTx(ctx, tx, GraphAttemptStart{InstanceID: instanceID, Node: gate.SyncNode, RunID: 1, OwnerAllowanceSource: id, BugSyncPrerequisite: &gate})
			if e != nil {
				return e
			}
			if allowed {
				return nil
			}
		}
		return nil
	})
	return
}

// RedirectGraphBugSyncToFixerStop preserves the substantive budget's origin
// when a pending gate has no permission to enter another fixer attempt.
func (db *DB) RedirectGraphBugSyncToFixerStop(ctx context.Context, instanceID int64, gate BugSyncPrerequisite, at time.Time) error {
	if instanceID < 1 || gate.SyncNode == "" || gate.FixerNode == "" || gate.SyncNode == gate.FixerNode || at.IsZero() {
		return graphInvariant("invalid Bug sync prerequisite")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.id=? AND i.status='running' AND i.current_node=? AND d.workflow='bug' AND d.profile_version=2 AND NOT EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=i.id AND a.status='running') AND NOT EXISTS(SELECT 1 FROM graph_owner_waits w WHERE w.instance_id=i.id AND w.state IN ('waiting','claimed')))`, instanceID, gate.SyncNode).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return graphInvariant("Bug sync no longer owns fixer exhaustion reroute")
		}
		if _, err := recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: instanceID, FromNode: gate.SyncNode, ToNode: gate.FixerNode, OnOutcome: "fixer_exhausted", At: at}); err != nil {
			return err
		}
		return setGraphInstanceTx(ctx, tx, instanceID, gate.FixerNode, GraphRunning, at, time.Time{})
	})
}
