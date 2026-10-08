package store

import (
	"context"
	"fmt"
)

// validateReviewTriggerBase reserves the upper half of the graph band for the
// runs of Validate reviewers. A reviewer is started by the review step itself,
// not by a graph dispatch, yet it is a real run and the runs table admits one
// run per (session, trigger). Graph dispatch ids never reach this half, and a
// run allocated here is never started from prelaunch, so no dispatch is ever
// looked up for it. Being inside the graph band, every such identity is a
// private trigger: it never advances the comment watermark.
const validateReviewTriggerBase = graphTriggerBase + 1<<59

// isValidateReviewTrigger reports whether an identity belongs to a reviewer run
// rather than to a graph dispatch.
func isValidateReviewTrigger(id int64) bool {
	return id > validateReviewTriggerBase && id < automaticTriggerBase
}

// NextValidateReviewTrigger returns an unused private trigger identity for the
// next reviewer run of a session. The caller allocates the run under the task
// lock, so two reviewers of one session never race for the same identity.
func (db *DB) NextValidateReviewTrigger(ctx context.Context, sessionID int64) (int64, error) {
	var last int64
	err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(trigger_comment_id),?) FROM runs WHERE session_id=? AND trigger_comment_id>? AND trigger_comment_id<?`,
		validateReviewTriggerBase, sessionID, validateReviewTriggerBase, automaticTriggerBase).Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("store: next reviewer trigger: %w", err)
	}
	return last + 1, nil
}

// ResultBindingsByAttempt lists every result binding made under one attempt
// identity, newest run first. A reviewer whose reply was lost is found by its
// attempt alone; the bindings grant historical reads only.
func (db *DB) ResultBindingsByAttempt(ctx context.Context, attempt string) ([]ResultBinding, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT r.id,COALESCE(a.run_id,0),r.task,r.mode,r.work_context,r.context_hash,a.attempt,r.schema_version,r.workflow_version,r.submission_version,r.workflow,COALESCE(a.doctor_attempt_id,'') FROM application_results r JOIN application_result_attempts a ON a.result_id=r.id WHERE a.attempt=? ORDER BY COALESCE(a.run_id,0) DESC`, attempt)
	if err != nil {
		return nil, fmt.Errorf("store: read result bindings: %w", err)
	}
	defer rows.Close()
	var out []ResultBinding
	for rows.Next() {
		var b ResultBinding
		if err := rows.Scan(&b.ResultID, &b.RunID, &b.Task, &b.Mode, &b.WorkContext, &b.ContextHash, &b.Attempt, &b.SchemaVersion, &b.WorkflowVersion, &b.SubmissionVersion, &b.Workflow, &b.DoctorAttemptID); err != nil {
			return nil, fmt.Errorf("store: scan result binding: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
