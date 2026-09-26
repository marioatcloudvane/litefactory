package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// CancelBugTestCall serializes cancellation against phase command launches.
// It never asserts that a process has stopped.
func (d *DB) CancelBugTestCall(ctx context.Context, id int64) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
		if e != nil {
			return e
		}
		if c.Consumed {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_test_calls SET cancelled=1 WHERE id=?`, id); e != nil {
			return e
		}
		for _, phase := range []string{"regression", "focused", "full"} {
			if _, e = tx.ExecContext(ctx, `UPDATE bug_check_phases SET cancelled=1 WHERE session_id=? AND operation=?`, c.Request.SessionID, c.PhaseBinding(phase).Operation); e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO bug_test_events(call_id,kind,evidence) VALUES(?,'cancel_requested','durable cancellation; cleanup unconfirmed')`, id)
		return e
	})
}
func bugTestEvidenceTx(ctx context.Context, tx *sql.Tx, c BugTestCall) (string, string, error) {
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_check_phases WHERE session_id=? AND json_extract(binding,'$.generation')=? AND cleanup=0`, c.Request.SessionID, c.PhaseBinding("regression").Generation).Scan(&unresolved); err != nil {
		return "", "", err
	}
	if unresolved != 0 {
		return "pending", "check process cleanup is unconfirmed", nil
	}
	for _, phase := range []string{"regression", "focused", "full"} {
		expected := c.PhaseBinding(phase)
		var raw []byte
		var outcome, reason string
		var cleanup, current, unreliable bool
		e := tx.QueryRowContext(ctx, `SELECT binding,outcome,reason,cleanup,current,unreliable FROM bug_check_phases WHERE session_id=? AND operation=?`, c.Request.SessionID, expected.Operation).Scan(&raw, &outcome, &reason, &cleanup, &current, &unreliable)
		if errors.Is(e, sql.ErrNoRows) {
			if c.Cancelled {
				return "cancelled", "test child cancelled before next phase", nil
			}
			return "pending", "check phase has no retained outcome", nil
		}
		if e != nil {
			return "", "", e
		}
		var binding BugCheckBinding
		if e = json.Unmarshal(raw, &binding); e != nil {
			return "", "", e
		}
		if binding != expected {
			return "", "", errors.New("bug child phase binding mismatch")
		}
		if !cleanup {
			return "pending", "check process cleanup is unconfirmed", nil
		}
		if c.Cancelled {
			return "cancelled", "test child cancelled", nil
		}
		if outcome == "pending" {
			return "pending", "check execution unresolved", nil
		}
		if !current || unreliable {
			return "blocked", "check evidence is stale or unreliable", nil
		}
		switch outcome {
		case "failed", "blocked", "cancelled":
			return outcome, reason, nil
		case "passed":
		default:
			return "", "", errors.New("invalid child phase outcome")
		}
		// Recheck the evidence rather than trusting the mutable phase summary alone.
		var count, complete int
		if e = tx.QueryRowContext(ctx, `SELECT command_count FROM bug_check_phases WHERE session_id=? AND operation=?`, c.Request.SessionID, expected.Operation).Scan(&count); e != nil {
			return "", "", e
		}
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_check_commands WHERE session_id=? AND operation=? AND state='completed' AND json_extract(observation,'$.outcome')='passed' AND json_extract(observation,'$.exit')=0 AND json_extract(observation,'$.capture_complete')=1 AND json_extract(observation,'$.cleanup_confirmed')=1`, c.Request.SessionID, expected.Operation).Scan(&complete); e != nil {
			return "", "", e
		}
		if complete != count || count < 1 {
			return "", "", errors.New("incomplete passing child evidence")
		}
	}
	return "passed", "", nil
}

// SettleBugTestCall derives the domain outcome and atomically wakes its parent.
// An unknown process keeps ownership and the suspended call unchanged.
func (d *DB) SettleBugTestCall(ctx context.Context, id int64, at time.Time) (BugTestCall, error) {
	if at.IsZero() {
		return BugTestCall{}, errors.New("bug child settlement needs timestamp")
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		outcome, reason, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if outcome == "pending" {
			return nil
		}
		if outcome == "passed" {
			if e = bugTestRepairTx(ctx, tx, c.Request); e != nil {
				outcome, reason = "blocked", e.Error()
			}
		}
		var guards int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_work_guards WHERE session_id=?`, c.Request.SessionID).Scan(&guards); e != nil {
			return e
		}
		if guards != 0 {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_test_calls SET outcome=?,reason=? WHERE id=?`, outcome, reason, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_test_events(call_id,kind,evidence) VALUES(?,'settled',?)`, id, outcome+":"+reason); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		// Graph success is execution bookkeeping only; the evidence above owns tests.
		// A retained completed phase may have outlived its graph bookkeeping.
		// Finish that original attempt from its evidence instead of cancelling
		// successful work or launching another command.
		for _, phase := range []string{"regression", "focused", "full"} {
			var phaseOutcome string
			lookup := tx.QueryRowContext(ctx, `SELECT outcome FROM bug_check_phases WHERE session_id=? AND operation=? AND cleanup=1 AND outcome!='pending'`, c.Request.SessionID, c.PhaseBinding(phase).Operation).Scan(&phaseOutcome)
			if errors.Is(lookup, sql.ErrNoRows) {
				continue
			}
			if lookup != nil {
				return lookup
			}
			var attemptID int64
			lookup = tx.QueryRowContext(ctx, `SELECT id FROM graph_node_attempts WHERE instance_id=? AND node=? AND status='running'`, c.ChildInstance, phase).Scan(&attemptID)
			if errors.Is(lookup, sql.ErrNoRows) {
				continue
			}
			if lookup != nil {
				return lookup
			}
			next := phaseOutcome
			if phaseOutcome == "passed" {
				next = "focused"
				if phase == "focused" {
					next = "full"
				}
				if phase == "full" {
					next = "passed"
				}
			}
			if _, e = finishGraphAttemptTx(ctx, tx, attemptID, GraphSucceeded, phaseOutcome, at); e != nil {
				return e
			}
			if _, e = recordGraphTransitionTx(ctx, tx, GraphTransition{InstanceID: c.ChildInstance, FromNode: phase, ToNode: next, OnOutcome: phaseOutcome, At: at}); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',finished_at=? WHERE instance_id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET current_node=?,status='succeeded',finished_at=?,next_due_at=NULL WHERE id=? AND status='running'`, outcome, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		var node string
		if e = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, c.Request.ParentAttempt).Scan(&node); e != nil {
			return e
		}
		if e = bugTestDispatchTx(ctx, tx, c.Request.ParentInstance, c.Request.SessionID, node, at); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE bug_execution SET revision=revision+1 WHERE session_id=?`, c.Request.SessionID)
		return e
	})
	if err != nil {
		return BugTestCall{}, err
	}
	return d.GetBugTestCall(ctx, id)
}

// InvalidateBugTestCall retains the original binding and passing history.
func (d *DB) InvalidateBugTestCall(ctx context.Context, id int64, reason string) error {
	if reason == "" {
		return errors.New("bug child invalidation requires cause")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `UPDATE bug_test_calls SET current=0,reason=? WHERE id=? AND consumed=0`, reason, id); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `INSERT INTO bug_test_events(call_id,kind,evidence) VALUES(?,'invalidated',?)`, id, reason)
		return e
	})
}

// bugTestConsumeTx joins parent transition and call consumption atomically.
func bugTestConsumeTx(ctx context.Context, tx *sql.Tx, attempt int64, in GraphCompletion) error {
	c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE parent_attempt=?`, attempt))
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	if in.Status != GraphSucceeded {
		return nil
	}
	if c.Consumed || c.Outcome == "pending" {
		return errors.New("bug child cannot be consumed")
	}
	outcome := c.Outcome
	if c.Cancelled {
		outcome = "cancelled"
	} else if !c.Current {
		outcome = "blocked"
	}
	if outcome == "passed" {
		verified, _, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if verified != "passed" {
			return errors.New("bug child passing evidence changed before consumption")
		}
		if e = bugTestRepairTx(ctx, tx, c.Request); e != nil {
			return e
		}
	}
	if in.Outcome != outcome || in.Transition == nil {
		return errors.New("bug parent transition differs from child outcome")
	}
	_, e = tx.ExecContext(ctx, `UPDATE bug_test_calls SET consumed=1 WHERE id=?`, c.ID)
	return e
}

// ListUnresolvedBugTests is bounded by active task count, not dispatch polling.
func (d *DB) ListUnresolvedBugTests(ctx context.Context) ([]BugTestCall, error) {
	rows, e := d.sql.QueryContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE outcome='pending' ORDER BY id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []BugTestCall
	for rows.Next() {
		c, e := scanBugTest(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// BugTestDispatchCall resolves the recorded child instance, never the latest
// graph in a session (which may instead be its suspended parent).
func (d *DB) BugTestDispatchCall(ctx context.Context, dispatch int64) (BugTestCall, error) {
	var instance int64
	if e := d.sql.QueryRowContext(ctx, `SELECT instance_id FROM graph_dispatches WHERE id=?`, dispatch).Scan(&instance); e != nil {
		return BugTestCall{}, fmt.Errorf("bug child dispatch: %w", e)
	}
	return d.BugTestChildCall(ctx, instance)
}

// FailBugTestCall retains activity errors without confusing them with ordinary
// test failure. Possible running checks leave the call and ownership unresolved.
func (d *DB) FailBugTestCall(ctx context.Context, id int64, reason string, at time.Time) error {
	if reason == "" || at.IsZero() {
		return errors.New("bug child failure requires cause and time")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO bug_test_events(call_id,kind,evidence) VALUES(?,'execution_error',?)`, id, reason); e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		var active int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_work_guards WHERE session_id=?`, c.Request.SessionID).Scan(&active); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_test_calls SET reason=? WHERE id=?`, reason, id); e != nil {
			return e
		}
		if active > 0 {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) SELECT id,'diagnostic',?,? FROM graph_node_attempts WHERE instance_id=? AND status='running'`, []byte(reason), FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='failed',finished_at=? WHERE instance_id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE bug_test_calls SET outcome='blocked' WHERE id=?`, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',finished_at=?,next_due_at=NULL WHERE id=? AND status='running'`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		var node string
		if e = tx.QueryRowContext(ctx, `SELECT node FROM graph_node_attempts WHERE id=?`, c.Request.ParentAttempt).Scan(&node); e != nil {
			return e
		}
		return bugTestDispatchTx(ctx, tx, c.Request.ParentInstance, c.Request.SessionID, node, at)
	})
}

// RecoverBugTestDispatch requeues bookkeeping after restart only. Command replay
// decisions remain with the phase reconciler; no process is launched here.
func (d *DB) RecoverBugTestDispatch(ctx context.Context, id int64, at time.Time) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		var node, status string
		if e = tx.QueryRowContext(ctx, `SELECT current_node,status FROM graph_instances WHERE id=?`, c.ChildInstance).Scan(&node, &status); e != nil {
			return e
		}
		if status != GraphRunning {
			return errors.New("bug child terminal graph requires error settlement")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='finished',finished_at=? WHERE instance_id=? AND status IN ('reserved','running')`, FormatTime(at), c.ChildInstance); e != nil {
			return e
		}
		return bugTestDispatchTx(ctx, tx, c.ChildInstance, c.Request.SessionID, node, at)
	})
}

// ClaimBugTestDispatch consumes the daemon reservation once without allocating
// a fictitious agent run. Child identity is its persisted graph plus dispatch.
func (d *DB) ClaimBugTestDispatch(ctx context.Context, in GraphDispatch) (bool, error) {
	var claimed bool
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE child_instance=? AND session_id=?`, in.InstanceID, in.SessionID))
		if e != nil {
			return e
		}
		if c.Outcome != "pending" {
			return nil
		}
		result, e := tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='running' WHERE id=? AND instance_id=? AND session_id=? AND node=? AND status='reserved' AND allocated_run_id IS NULL`, in.ID, in.InstanceID, in.SessionID, in.Node)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		claimed = n == 1
		return e
	})
	return claimed, err
}

// PassedBugTestCall is the read-only downstream handoff. It revalidates the
// exact consumed child and all phase records; a graph success flag is insufficient.
func (d *DB) PassedBugTestCall(ctx context.Context, id int64) (BugTestCall, error) {
	var c BugTestCall
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		c, e = scanBugTest(tx.QueryRowContext(ctx, `SELECT `+bugTestColumns+` FROM bug_test_calls WHERE id=?`, id))
		if e != nil {
			return e
		}
		if c.Outcome != "passed" || !c.Consumed || c.Cancelled || !c.Current {
			return errors.New("bug child has no current consumed passing evidence")
		}
		outcome, _, e := bugTestEvidenceTx(ctx, tx, c)
		if e != nil {
			return e
		}
		if outcome != "passed" {
			return errors.New("bug child phase evidence no longer passes")
		}
		return bugTestRepairTx(ctx, tx, c.Request)
	})
	return c, err
}
