package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func bugFactoryRecoveryNode(node string) bool {
	switch node {
	case "record", "record_analysis", "analysis_needed", "analysis_done", "analysis_questions", "analysis_review", "attempt", "record_repair", "fixer_reshape", "test", "pr", "delivery_review", "merge", "close", "report":
		return true
	default:
		return false
	}
}

// CurrentBugFactoryAttempt exposes only the exact unfinished factory operation.
// A missing agent run binding is never evidence that an agent was undispatched.
func (d *DB) CurrentBugFactoryAttempt(ctx context.Context, instance int64, node string) (GraphNodeAttempt, error) {
	var out GraphNodeAttempt
	if !bugFactoryRecoveryNode(node) {
		return out, ErrNotFound
	}
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bug_lifecycles l JOIN graph_instances i ON i.id=l.parent_instance JOIN graph_definitions g ON g.id=i.definition_id WHERE i.id=? AND i.current_node=? AND i.status='running' AND g.workflow='bug'`, instance, node).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrNotFound
		}
		var e error
		out, e = scanGraphAttempt(tx.QueryRowContext(ctx, `SELECT `+graphAttemptColumns+` FROM graph_node_attempts WHERE instance_id=? AND node=? AND status IN ('pending','running') AND run_id IS NULL ORDER BY id DESC LIMIT 1`, instance, node))
		return e
	})
	return out, err
}

// ReconcileBugParentGraphs runs once at startup after child reconciliation.
// Factory effects retain their original attempt and dispatch identities; their
// owning activities must reconcile journals before attempting any external write.
// Interrupted agent work remains spent and cannot be blindly launched again.
func (d *DB) ReconcileBugParentGraphs(ctx context.Context, at time.Time) error {
	if at.IsZero() {
		return errors.New("bug parent recovery requires observation time")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT a.id,a.instance_id,a.node,COALESCE(a.run_id,0),i.session_id,a.started_at FROM bug_lifecycles l JOIN graph_instances i ON i.id=l.parent_instance JOIN graph_definitions g ON g.id=i.definition_id JOIN graph_node_attempts a ON a.instance_id=i.id AND a.node=i.current_node WHERE g.workflow='bug' AND i.status='running' AND a.status IN ('pending','running') AND NOT EXISTS(SELECT 1 FROM bug_test_calls c WHERE c.parent_attempt=a.id AND c.consumed=0) ORDER BY a.id`)
		if e != nil {
			return e
		}
		type pending struct {
			id, instance, run, session int64
			node, started              string
		}
		var work []pending
		for rows.Next() {
			var p pending
			if e = rows.Scan(&p.id, &p.instance, &p.node, &p.run, &p.session, &p.started); e != nil {
				break
			}
			work = append(work, p)
		}
		if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
			return e
		}
		for _, p := range work {
			stamp := FormatTime(at)
			if stamp < p.started {
				stamp = p.started
			}
			if p.run == 0 && bugFactoryRecoveryNode(p.node) {
				var dispatchID, allocated int64
				var dispatchNode string
				e = tx.QueryRowContext(ctx, `SELECT id,node,COALESCE(allocated_run_id,0) FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running')`, p.instance).Scan(&dispatchID, &dispatchNode, &allocated)
				switch {
				case e == nil:
					if allocated != 0 || dispatchNode != p.node {
						return errors.New("bug factory recovery dispatch binding mismatch")
					}
					if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='pending',reserved_at=NULL,due_at=? WHERE id=?`, stamp, dispatchID); e != nil {
						return e
					}
				case errors.Is(e, sql.ErrNoRows):
					if _, e = tx.ExecContext(ctx, `INSERT INTO graph_dispatches(instance_id,session_id,node,due_at,created_at) VALUES(?,?,?,?,?)`, p.instance, p.session, p.node, stamp, stamp); e != nil {
						return e
					}
				default:
					return e
				}
				continue
			}
			reason := "Bug agent execution interrupted before graph completion; retained work and reservations require reconciliation, not relaunch"
			if p.run == 0 && p.node != "analyst" && p.node != "fixer" {
				reason = "Unknown Bug node interrupted; factory recovery refused"
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='failed',outcome=NULL,finished_at=? WHERE id=?`, stamp, p.id); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) VALUES(?,'diagnostic',?,?)`, p.id, []byte(reason), stamp); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status='failed',next_due_at=NULL,finished_at=? WHERE id=?`, stamp, p.instance); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status='cancelled',finished_at=?,cancellation_reason=? WHERE instance_id=? AND status IN ('pending','reserved','running')`, stamp, reason, p.instance); e != nil {
				return e
			}
			event := fmt.Sprintf("restart-attempt:%d", p.id)
			if _, e = tx.ExecContext(ctx, `INSERT INTO bug_lifecycle_events(session_id,event,kind,attempt_id,body,delivery_key) VALUES(?,?,'failure',?,?,?)`, p.session, event, p.id, reason, fmt.Sprintf("bug-restart:%d:%d", p.session, p.id)); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `UPDATE bug_lifecycles SET disposition='blocked',reference=?,revision=revision+1 WHERE session_id=?`, event, p.session); e != nil {
				return e
			}
		}
		return nil
	})
}

// BindBugRestartReport binds the one factory-rendered projection to a restart
// diagnostic. It cannot change an already bound or published report.
func (d *DB) BindBugRestartReport(ctx context.Context, session int64, event, key, body string) error {
	if key == "" || body == "" || len(body) > 65536 {
		return errors.New("invalid Bug restart report projection")
	}
	return d.withTx(ctx, func(tx *sql.Tx) error {
		var oldKey, oldBody, kind string
		var attempt int64
		var reported bool
		if e := tx.QueryRowContext(ctx, `SELECT kind,attempt_id,body,delivery_key,reported FROM bug_lifecycle_events WHERE session_id=? AND event=?`, session, event).Scan(&kind, &attempt, &oldBody, &oldKey, &reported); e != nil {
			return e
		}
		if kind != "failure" || attempt <= 0 || event != fmt.Sprintf("restart-attempt:%d", attempt) {
			return errors.New("bug report is not a restart diagnostic")
		}
		if oldKey == key && oldBody == body {
			return nil
		}
		if reported || oldKey != fmt.Sprintf("bug-restart:%d:%d", session, attempt) || key == oldKey {
			return errors.New("bug restart projection already bound")
		}
		var status string
		if e := tx.QueryRowContext(ctx, `SELECT a.status FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id WHERE a.id=? AND i.session_id=?`, attempt, session).Scan(&status); e != nil {
			return e
		}
		if status != GraphFailed {
			return errors.New("bug restart projection lacks failed source")
		}
		_, e := tx.ExecContext(ctx, `UPDATE bug_lifecycle_events SET body=?,delivery_key=? WHERE session_id=? AND event=? AND reported=0`, body, key, session, event)
		return e
	})
}
