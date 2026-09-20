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
		rows, e := tx.QueryContext(ctx, `SELECT a.id,a.instance_id,COALESCE(r.error,''),a.started_at FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id LEFT JOIN runs r ON r.id=a.run_id WHERE d.mode=? AND a.status IN ('pending','running') ORDER BY a.id`, mode)
		if e != nil {
			return e
		}
		type interrupted struct {
			id, instance    int64
			reason, started string
		}
		var attempts []interrupted
		for rows.Next() {
			var a interrupted
			if e = rows.Scan(&a.id, &a.instance, &a.reason, &a.started); e != nil {
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
			if _, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='failed',outcome=NULL,finished_at=? WHERE id=? AND status IN ('pending','running')`, finished, a.id); e != nil {
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
	stamp := FormatTime(at)
	// Save cause before terminalizing attempts. Append-only output keeps original
	// diagnostics and authored results independently recoverable.
	_, err := tx.ExecContext(ctx, `INSERT INTO graph_outputs(node_attempt_id,kind,payload,created_at) SELECT a.id,'diagnostic',?,MAX(?,a.started_at) FROM graph_node_attempts a JOIN graph_instances i ON i.id=a.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode=? AND a.status IN ('running','pending')`, []byte(reason), stamp, sessionID, mode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status='cancelled',outcome=NULL,finished_at=MAX(?,started_at) WHERE status IN ('running','pending') AND instance_id IN (SELECT i.id FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND d.mode=?)`, stamp, sessionID, mode)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE graph_instances SET status='cancelled',next_due_at=NULL,finished_at=MAX(?,started_at) WHERE session_id=? AND status='running' AND definition_id IN (SELECT id FROM graph_definitions WHERE mode=?)`, stamp, sessionID, mode)
	return err
}

// HasResearchGraphRun reports whether a run belongs to a Research graph.
func (db *DB) HasResearchGraphRun(ctx context.Context, runID int64) (bool, error) {
	return db.HasWorkflowGraphRun(ctx, runID, "research")
}

// HasResearchGraphSession reports whether a session has a Research graph.
func (db *DB) HasResearchGraphSession(ctx context.Context, sessionID int64) (bool, error) {
	return db.HasWorkflowGraphSession(ctx, sessionID, "research")
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
