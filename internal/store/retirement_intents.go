package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RetirementIntent retains exact pre-mutation evidence. Unknown means that the
// side effect has not been durably confirmed, including after process death.
type RetirementIntent struct {
	ID          int64
	SessionID   int64
	SweepID     string
	Kind        string
	Resource    string
	ExpectedSHA string
	BaseSHA     string
	Action      string
	Outcome     string
	Reason      string
	ObservedAt  time.Time
}

// RecordRetirementIntent replaces one resource's last outcome with fresh intent.
// Callers reconcile pending uncertainty before recording another attempt.
func (db *DB) RecordRetirementIntent(ctx context.Context, in RetirementIntent) (int64, error) {
	if in.SessionID <= 0 || in.SweepID == "" || in.Resource == "" || in.ObservedAt.IsZero() || in.Action != "remove" {
		return 0, fmt.Errorf("store: invalid retirement intent")
	}
	var id int64
	err := db.sql.QueryRowContext(ctx, `INSERT INTO retirement_intents(session_id,sweep_id,kind,resource,expected_sha,base_sha,action,observed_at) VALUES(?,?,?,?,?,?,?,?)
 ON CONFLICT(session_id,kind,resource) DO UPDATE SET sweep_id=excluded.sweep_id,expected_sha=excluded.expected_sha,base_sha=excluded.base_sha,action=excluded.action,observed_at=excluded.observed_at,outcome='unknown',reason='intent_recorded'
 RETURNING id`, in.SessionID, in.SweepID, in.Kind, in.Resource, in.ExpectedSHA, in.BaseSHA, in.Action, FormatTime(in.ObservedAt)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: record retirement intent: %w", err)
	}
	return id, nil
}

// RecordRetirementOutcome atomically clears only the exact confirmed worktree
// path together with the outcome. No external I/O occurs in this transaction.
func (db *DB) RecordRetirementOutcome(ctx context.Context, id int64, outcome, reason string, clearPath bool) error {
	switch outcome {
	case "removed", "already_absent", "failed", "unknown", "kept":
	default:
		return fmt.Errorf("store: invalid retirement outcome")
	}
	if clearPath && outcome != "removed" && outcome != "already_absent" {
		return fmt.Errorf("store: unconfirmed retirement cannot clear ownership")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var sessionID int64
		var kind, resource string
		if err := tx.QueryRowContext(ctx, `SELECT session_id,kind,resource FROM retirement_intents WHERE id=?`, id).Scan(&sessionID, &kind, &resource); err != nil {
			return fmt.Errorf("store: read retirement intent: %w", err)
		}
		if clearPath {
			if err := cancelResearchGraphsTx(ctx, tx, sessionID, "worktree retired", nowUTC()); err != nil {
				return err
			}
			if err := cancelWorkflowGraphsTx(ctx, tx, sessionID, "spike", "worktree retired", nowUTC()); err != nil {
				return err
			}
			if kind != "worktree" {
				return fmt.Errorf("store: only a worktree outcome can clear its path")
			}
			if _, err := tx.ExecContext(ctx, `UPDATE sessions SET worktree_path=NULL WHERE id=? AND worktree_path=?`, sessionID, resource); err != nil {
				return fmt.Errorf("store: clear retired worktree: %w", err)
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE retirement_intents SET outcome=?,reason=? WHERE id=?`, outcome, reason, id)
		return err
	})
}

// PendingRetirementIntents returns uncertain effects only. Retrying callers must
// observe again; these records confer no authority over present resources.
func (db *DB) PendingRetirementIntents(ctx context.Context, sessionID int64) ([]RetirementIntent, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,session_id,sweep_id,kind,resource,expected_sha,base_sha,action,outcome,reason,observed_at FROM retirement_intents WHERE session_id=? AND outcome='unknown' ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetirementIntent
	for rows.Next() {
		var in RetirementIntent
		var at string
		if err := rows.Scan(&in.ID, &in.SessionID, &in.SweepID, &in.Kind, &in.Resource, &in.ExpectedSHA, &in.BaseSHA, &in.Action, &in.Outcome, &in.Reason, &at); err != nil {
			return nil, err
		}
		in.ObservedAt, err = ParseTime(at)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
