package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SubmissionRecovery locates original authority and its trusted worktree without
// recreating permissions or consulting current issue prose.
type SubmissionRecovery struct {
	Binding  SubmissionAttempt
	Worktree string
	Active   bool
}

// GetSubmissionRecovery reads persisted scope even for an orphaned attempt.
func (db *DB) GetSubmissionRecovery(ctx context.Context, runID int64, attempt string) (SubmissionRecovery, error) {
	out := SubmissionRecovery{Binding: SubmissionAttempt{RunID: runID, Attempt: attempt}}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT a.parent_task,a.generation,a.stage,coalesce(p.worktree,''),a.active=1 AND r.status='running' AND f.generation=a.generation FROM submission_attempts a JOIN runs r ON r.id=a.run_id JOIN managed_features f ON f.parent_task=a.parent_task LEFT JOIN story_progress_runs p ON p.run_id=a.run_id WHERE a.run_id=? AND a.attempt=?`, runID, attempt).Scan(&out.Binding.Parent, &out.Binding.Generation, &out.Binding.Stage, &out.Worktree, &out.Active)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT story_key,issue_id,revision FROM submission_scope WHERE run_id=? AND attempt=? ORDER BY story_key`, runID, attempt)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s SubmissionStoryBinding
			if err := rows.Scan(&s.Story, &s.IssueID, &s.Revision); err != nil {
				return err
			}
			out.Binding.Stories = append(out.Binding.Stories, s)
		}
		return rows.Err()
	})
	return out, err
}

// AbandonSubmission resolves an expired pending request without granting any
// engineering or acceptance. Its raw contribution and diagnostics remain intact.
func (db *DB) AbandonSubmission(ctx context.Context, id int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if r.EngineeringState != "pending" || r.AcceptanceState != "pending" {
			return nil
		}
		var active bool
		var last int
		err = tx.QueryRowContext(ctx, `SELECT active,last_sequence FROM story_progress_runs WHERE run_id=?`, r.RunID).Scan(&active, &last)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && active && last < r.Sequence {
			if last+1 != r.Sequence {
				return fmt.Errorf("store: pending submissions must retire in sequence")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO story_progress_events(run_id,sequence,story_key,attempt,kind,digest,payload) VALUES(?,?,?,?,?,?,?)`, r.RunID, r.Sequence, r.Story, r.StoryAttempt, "rejected", submissionDigest(nil), []byte{}); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE story_progress_runs SET last_sequence=? WHERE run_id=?`, r.Sequence, r.RunID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE submission_receipts SET engineering_state='rejected',acceptance_state='unverified',effect_state='not_applicable',plan_state='not_applicable' WHERE id=?`, id)
		return err
	})
}
