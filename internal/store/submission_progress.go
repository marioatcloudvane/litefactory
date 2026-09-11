package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// CommitSubmissionProgress commits engineering/legacy progress and its receipt
// decision together, so replay cannot observe acceptance without correlation.
func (db *DB) CommitSubmissionProgress(ctx context.Context, id int64, e StoryProgressEvent, f StoryProgressEffect) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if r.EngineeringState != "pending" {
			if r.EngineeringState == "recorded" {
				return nil
			}
			return fmt.Errorf("store: submission already rejected")
		}
		if r.RunID != e.RunID || r.Sequence != e.Sequence || r.Story != e.Story || r.StoryAttempt != e.Attempt || r.Revision != f.Revision || r.InputState != "valid" {
			return fmt.Errorf("store: submission event correlation mismatch")
		}
		if err := currentSubmissionBinding(ctx, tx, r.RunID, r.Attempt, r.Story, r.Revision); err != nil {
			return err
		}
		if err := reserveSubmissionBytes(ctx, tx, r.RunID, len(e.Payload)); err != nil {
			return err
		}
		if err := acceptStoryProgressTx(ctx, tx, e, f); err != nil {
			return err
		}
		engineering, acceptance := "recorded", "not_applicable"
		if e.Kind == "verified" {
			acceptance = "accepted"
		}
		if e.Kind == "blocked" || e.Kind == "invalidated" {
			engineering = "rejected"
		}
		_, err = tx.ExecContext(ctx, `UPDATE submission_receipts SET engineering_state=?,acceptance_state=?,plan_state='not_applicable',effect_state='pending' WHERE id=?`, engineering, acceptance, id)
		return err
	})
}

// RejectSubmissionProgress preserves invalid-input sequence and attempt slots
// without converting diagnostics into engineering or a remote effect.
func (db *DB) RejectSubmissionProgress(ctx context.Context, id int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if r.EngineeringState == "rejected" {
			return nil
		}
		if err := currentSubmissionBinding(ctx, tx, r.RunID, r.Attempt, r.Story, r.Revision); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE story_progress_runs SET last_sequence=? WHERE run_id=? AND active=1 AND last_sequence=?`, r.Sequence, r.RunID, r.Sequence-1)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: rejected submission is not next")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO story_progress_events(run_id,sequence,story_key,attempt,kind,digest,payload) VALUES(?,?,?,?,?,?,?)`, r.RunID, r.Sequence, r.Story, r.StoryAttempt, "rejected", submissionDigest(nil), []byte{}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE submission_receipts SET engineering_state='rejected',acceptance_state='not_applicable',plan_state='not_applicable',effect_state='not_applicable' WHERE id=?`, id)
		return err
	})
}

// ReconcileSubmissionEffects exposes actual ordered-effect completion after
// daemon delivery, without granting local engineering or acceptance authority.
func (db *DB) ReconcileSubmissionEffects(ctx context.Context) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE submission_receipts SET effect_state='delivered' WHERE effect_state IN ('pending','failed') AND engineering_state IN ('recorded','rejected') AND EXISTS(SELECT 1 FROM story_progress_events e WHERE e.run_id=submission_receipts.run_id AND e.sequence=submission_receipts.sequence) AND NOT EXISTS(SELECT 1 FROM story_progress_effects f WHERE f.run_id=submission_receipts.run_id AND f.sequence=submission_receipts.sequence AND f.delivered=0 AND f.superseded=0)`)
	if err != nil {
		return err
	}
	_, err = db.sql.ExecContext(ctx, `UPDATE submission_receipts SET effect_state='delivered' WHERE effect_state IN ('pending','failed') AND engineering_state='not_applicable' AND acceptance_state IN ('accepted','unverified') AND EXISTS(SELECT 1 FROM acceptance_decisions a WHERE a.run_id=submission_receipts.run_id AND a.story_key=submission_receipts.story_key AND a.delivered=1)`)
	return err
}

// CommitSubmissionAssessment retains a per-story new-contract assessment and its
// receipt atomically. The compatibility run row is a version marker; full typed
// findings and frozen correlation remain in immutable submission receipts.
func (db *DB) CommitSubmissionAssessment(ctx context.Context, id int64, d AcceptanceDecision) error {
	var findings AcceptanceFindings
	if err := json.Unmarshal([]byte(d.FindingsJSON), &findings); err != nil {
		return err
	}
	if d.Accepted != findings.Accepted() {
		return fmt.Errorf("store: inconsistent assessment decision")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if r.AcceptanceState != "pending" {
			return nil
		}
		if r.RunID != d.RunID || r.Story != d.Story || r.Revision != d.Revision || r.InputState != "valid" {
			return fmt.Errorf("store: assessment correlation mismatch")
		}
		if err := currentSubmissionBinding(ctx, tx, r.RunID, r.Attempt, r.Story, r.Revision); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM submission_attempts a JOIN managed_stories s ON s.parent_task=a.parent_task AND s.generation=a.generation JOIN approved_story_revisions v USING(parent_task,generation,story_key) WHERE a.run_id=? AND a.attempt=? AND a.stage='validate' AND a.parent_task=? AND a.generation=? AND s.story_key=? AND s.issue_id=? AND s.issue_number=? AND v.parent_id=? AND v.digest=? AND v.body=?`, r.RunID, r.Attempt, d.Parent, d.Generation, d.Story, d.IssueID, d.IssueNumber, d.ParentID, d.Revision, d.FrozenBody).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: assessment frozen attribution mismatch")
		}
		const marker = `{"submission_version":2,"findings_source":"submission_receipts"}`
		if _, err := tx.ExecContext(ctx, `INSERT INTO validation_assessments(run_id,parent_task,generation,context_json,result_json,materialized) VALUES(?,?,?,?,?,1) ON CONFLICT DO NOTHING`, d.RunID, d.Parent, d.Generation, marker, marker); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM acceptance_decisions WHERE parent_task=? AND generation=? AND story_key=? AND accepted=1`, d.Parent, d.Generation, d.Story).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO acceptance_decisions(run_id,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,snapshot,frozen_body,findings_json,accepted) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, d.RunID, d.Parent, d.Generation, d.Story, d.IssueNumber, d.IssueID, d.ParentID, d.Revision, d.Snapshot, d.FrozenBody, d.FindingsJSON, d.Accepted); err != nil {
				return err
			}
		}
		state := "unverified"
		if d.Accepted {
			state = "accepted"
		}
		_, err = tx.ExecContext(ctx, `UPDATE submission_receipts SET engineering_state='not_applicable',acceptance_state=?,plan_state='not_applicable',effect_state='pending' WHERE id=?`, state, id)
		return err
	})
}
