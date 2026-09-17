package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SubmissionStoryBinding is copied from the authorized frozen scope.
type SubmissionStoryBinding struct {
	Story, Revision string
	IssueID         int64
}

// SubmissionAttempt binds local broker authority to one actual subprocess.
type SubmissionAttempt struct {
	RunID                  int64
	Attempt, Parent, Stage string
	Generation             int
	Stories                []SubmissionStoryBinding
}

// BindSubmissionAttempt atomically replaces old attempt authority. The old
// correlation and receipts remain readable, but cannot allocate or decide again.
func (db *DB) BindSubmissionAttempt(ctx context.Context, a SubmissionAttempt) error {
	if a.RunID < 1 || a.Attempt == "" || len(a.Attempt) > 256 || a.Parent == "" || a.Generation < 1 {
		return fmt.Errorf("store: invalid submission attempt")
	}
	a.Stories = slices.Clone(a.Stories)
	slices.SortFunc(a.Stories, func(x, y SubmissionStoryBinding) int { return strings.Compare(x.Story, y.Story) })
	for i, s := range a.Stories {
		if s.Story == "" || s.IssueID < 1 || !validSubmissionDigest(s.Revision) || (i > 0 && a.Stories[i-1].Story == s.Story) {
			return fmt.Errorf("store: invalid submission scope")
		}
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		return err
	}
	hash := submissionDigest(encoded)
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var mode string
		var workflow int
		if err := tx.QueryRowContext(ctx, `SELECT r.mode,f.workflow_version FROM runs r JOIN sessions s ON s.id=r.session_id JOIN managed_features f ON f.parent_task=s.task_ref JOIN submission_generations g ON g.parent_task=f.parent_task AND g.generation=f.generation WHERE r.id=? AND r.status='running' AND f.parent_task=? AND f.generation=? AND g.submission_version=2 AND f.submission_version=2`, a.RunID, a.Parent, a.Generation).Scan(&mode, &workflow); err != nil {
			return fmt.Errorf("store: current attempt admission: %w", err)
		}
		compatible := (a.Stage == "plan" && mode == "plan") || (a.Stage == "engineering" && mode == "implement" && workflow == 2) || (a.Stage == "legacy" && mode == "implement" && workflow == 1) || (a.Stage == "validate" && mode == "validate" && workflow == 2)
		if !compatible {
			return fmt.Errorf("store: stage conflicts with run/workflow")
		}
		var oldHash string
		var active bool
		err := tx.QueryRowContext(ctx, `SELECT scope_hash,active FROM submission_attempts WHERE run_id=? AND attempt=?`, a.RunID, a.Attempt).Scan(&oldHash, &active)
		if err == nil {
			if oldHash != hash || !active {
				return fmt.Errorf("store: immutable or stale attempt binding")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, story := range a.Stories {
			var n int
			query := `SELECT count(*) FROM managed_stories s JOIN approved_story_revisions r USING(parent_task,generation,story_key) WHERE s.parent_task=? AND s.generation=? AND s.story_key=? AND s.issue_id=? AND r.digest=? AND s.state!='completed'`
			if a.Stage == "plan" {
				query = `SELECT count(*) FROM managed_stories WHERE parent_task=? AND generation=? AND story_key=? AND issue_id=? AND body_hash=? AND state!='completed'`
			}
			if err := tx.QueryRowContext(ctx, query, a.Parent, a.Generation, story.Story, story.IssueID, story.Revision).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("store: story binding is not current frozen scope")
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE submission_attempts SET active=0 WHERE run_id=?`, a.RunID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO submission_attempts VALUES(?,?,?,?,?,?,1)`, a.RunID, a.Attempt, a.Parent, a.Generation, a.Stage, hash); err != nil {
			return err
		}
		for _, s := range a.Stories {
			if _, err := tx.ExecContext(ctx, `INSERT INTO submission_scope VALUES(?,?,?,?,?)`, a.RunID, a.Attempt, s.Story, s.IssueID, s.Revision); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO submission_run_counters(run_id) VALUES(?) ON CONFLICT DO NOTHING`, a.RunID)
		return err
	})
}

// EndSubmissionAttempt revokes only the specified broker attempt.
func (db *DB) EndSubmissionAttempt(ctx context.Context, runID int64, attempt string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE submission_attempts SET active=0 WHERE run_id=? AND attempt=?`, runID, attempt)
	return err
}

// SubmissionRequest is already preflight-validated input with factory binding.
// Invalid input may be recorded as rejected, independently from decision state.
type SubmissionRequest struct {
	RunID                                           int64
	Attempt, RequestID, Story, Revision, InputState string
	Payload, Diagnostics                            []byte
}

// SubmissionReceipt keeps local validity, evidence, acceptance and remote effects
// independent. Pending is not a successful engineering or acceptance decision.
type SubmissionReceipt struct {
	ID, RunID                                                             int64
	Attempt, RequestID, RequestHash, Story, Revision                      string
	Sequence, StoryAttempt                                                int
	Payload, Diagnostics                                                  []byte
	InputState, EngineeringState, AcceptanceState, EffectState, PlanState string
}

const receiptColumns = `id,run_id,attempt,request_id,request_hash,story_key,revision,sequence,story_attempt,payload,diagnostics,input_state,engineering_state,acceptance_state,effect_state,plan_state`

func scanSubmissionReceipt(row interface{ Scan(...interface{}) error }) (SubmissionReceipt, error) {
	var r SubmissionReceipt
	err := row.Scan(&r.ID, &r.RunID, &r.Attempt, &r.RequestID, &r.RequestHash, &r.Story, &r.Revision, &r.Sequence, &r.StoryAttempt, &r.Payload, &r.Diagnostics, &r.InputState, &r.EngineeringState, &r.AcceptanceState, &r.EffectState, &r.PlanState)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// AllocateSubmission atomically stores request bytes, hash, unique event counters
// and initial receipt. A content-changing retry never replaces the first request.
func (db *DB) AllocateSubmission(ctx context.Context, q SubmissionRequest) (SubmissionReceipt, error) {
	var receipt SubmissionReceipt
	if q.RequestID == "" || len(q.RequestID) > 256 || len(q.Payload)+len(q.Diagnostics) > 4<<20 || !json.Valid(q.Payload) || !json.Valid(q.Diagnostics) || (q.InputState != "valid" && q.InputState != "rejected") {
		return receipt, fmt.Errorf("store: invalid submission request")
	}
	hash := submissionDigest(q.Payload)
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := currentSubmissionBinding(ctx, tx, q.RunID, q.Attempt, q.Story, q.Revision); err != nil {
			return err
		}
		old, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE run_id=? AND request_id=?`, q.RunID, q.RequestID))
		if err == nil {
			if old.RequestHash != hash || old.Attempt != q.Attempt || old.Story != q.Story || old.Revision != q.Revision {
				return fmt.Errorf("store: request identity reused with different content or binding")
			}
			receipt = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := reserveSubmissionBytes(ctx, tx, q.RunID, len(q.Payload)+len(q.Diagnostics)); err != nil {
			return err
		}
		var sequence, attempt int
		if err := tx.QueryRowContext(ctx, `UPDATE submission_run_counters SET sequence=sequence+1 WHERE run_id=? AND sequence<4096 RETURNING sequence`, q.RunID).Scan(&sequence); err != nil {
			return fmt.Errorf("store: allocate sequence: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(story_attempt),0)+1 FROM submission_receipts WHERE run_id=? AND story_key=?`, q.RunID, q.Story).Scan(&attempt); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO submission_receipts(run_id,attempt,request_id,request_hash,story_key,revision,sequence,story_attempt,payload,diagnostics,input_state,engineering_state,acceptance_state,effect_state,plan_state) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending','pending','pending','pending')`, q.RunID, q.Attempt, q.RequestID, hash, q.Story, q.Revision, sequence, attempt, q.Payload, q.Diagnostics, q.InputState)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		receipt, err = scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		return err
	})
	return receipt, err
}

func currentSubmissionBinding(ctx context.Context, tx *sql.Tx, runID int64, attempt, story, revision string) error {
	var parent, stage string
	var generation int
	err := tx.QueryRowContext(ctx, `SELECT a.parent_task,a.generation,a.stage FROM submission_attempts a JOIN runs r ON r.id=a.run_id JOIN managed_features f ON f.parent_task=a.parent_task WHERE a.run_id=? AND a.attempt=? AND a.active=1 AND r.status='running' AND f.generation=a.generation AND f.submission_version=2`, runID, attempt).Scan(&parent, &generation, &stage)
	if err != nil {
		return fmt.Errorf("store: stale submission attempt: %w", err)
	}
	if stage == "plan" && story == "" && revision == "" {
		return nil
	}
	var n int
	query := `SELECT count(*) FROM submission_scope b JOIN managed_stories s ON s.parent_task=? AND s.generation=? AND s.story_key=b.story_key AND s.issue_id=b.issue_id JOIN approved_story_revisions r USING(parent_task,generation,story_key) WHERE b.run_id=? AND b.attempt=? AND b.story_key=? AND b.revision=? AND r.digest=b.revision AND s.state!='completed'`
	if stage == "plan" {
		query = `SELECT count(*) FROM submission_scope b JOIN managed_stories s ON s.parent_task=? AND s.generation=? AND s.story_key=b.story_key AND s.issue_id=b.issue_id WHERE b.run_id=? AND b.attempt=? AND b.story_key=? AND b.revision=? AND s.body_hash=b.revision AND s.state!='completed'`
	}
	if err := tx.QueryRowContext(ctx, query, parent, generation, runID, attempt, story, revision).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: stale or unauthorized submission story")
	}
	return nil
}

// GetSubmissionReceipt can read historical correlation after the run has ended.
func (db *DB) GetSubmissionReceipt(ctx context.Context, runID int64, requestID string) (SubmissionReceipt, error) {
	return scanSubmissionReceipt(db.sql.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE run_id=? AND request_id=?`, runID, requestID))
}

// SubmissionDecision preserves each independently meaningful decision dimension.
type SubmissionDecision struct{ Engineering, Acceptance, Effects, Plan string }

// DecideSubmission records a local decision only while the exact authority is
// current. Remote recovery uses SetSubmissionEffects without granting acceptance.
func (db *DB) DecideSubmission(ctx context.Context, id int64, decision SubmissionDecision) error {
	if !submissionState(decision.Engineering, "pending", "recorded", "rejected", "not_applicable") || !submissionState(decision.Acceptance, "pending", "accepted", "rejected", "unverified", "not_applicable") || !submissionState(decision.Effects, "pending", "delivered", "failed", "not_applicable") || !submissionState(decision.Plan, "pending", "published", "discussion", "rejected", "not_applicable") {
		return fmt.Errorf("store: invalid receipt decision")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := scanSubmissionReceipt(tx.QueryRowContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if err := currentSubmissionBinding(ctx, tx, r.RunID, r.Attempt, r.Story, r.Revision); err != nil {
			return err
		}
		var stage string
		if err := tx.QueryRowContext(ctx, `SELECT stage FROM submission_attempts WHERE run_id=? AND attempt=?`, r.RunID, r.Attempt).Scan(&stage); err != nil {
			return err
		}
		if decision.Acceptance == "accepted" && stage != "legacy" && stage != "validate" {
			return fmt.Errorf("store: stage cannot grant acceptance")
		}
		if decision.Engineering == "recorded" && stage != "legacy" && stage != "engineering" {
			return fmt.Errorf("store: stage cannot record engineering")
		}
		if decision.Plan == "published" || decision.Plan == "discussion" {
			if stage != "plan" {
				return fmt.Errorf("store: stage cannot decide Plan")
			}
		}
		if (r.EngineeringState != "pending" && r.EngineeringState != decision.Engineering) || (r.AcceptanceState != "pending" && r.AcceptanceState != decision.Acceptance) || (r.PlanState != "pending" && r.PlanState != decision.Plan) {
			return fmt.Errorf("store: immutable receipt decision conflict")
		}
		if r.InputState != "valid" && (decision.Engineering == "recorded" || decision.Acceptance == "accepted" || decision.Plan == "published") {
			return fmt.Errorf("store: rejected input cannot acquire success")
		}
		_, err = tx.ExecContext(ctx, `UPDATE submission_receipts SET engineering_state=?,acceptance_state=?,effect_state=?,plan_state=? WHERE id=?`, decision.Engineering, decision.Acceptance, decision.Effects, decision.Plan, id)
		return err
	})
}

// SetSubmissionEffects recovers publication effects without changing local proof.
func (db *DB) SetSubmissionEffects(ctx context.Context, id int64, state string) error {
	if !submissionState(state, "pending", "delivered", "failed", "not_applicable") {
		return fmt.Errorf("store: invalid effect state")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE submission_receipts SET effect_state=? WHERE id=?`, state, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return ErrNotFound
	}
	return err
}

// ListPendingSubmissions includes effect debt after restart, without run authority.
// It is intentionally global: the daemon-wide maintenance sweep uses it to
// reconcile every run's pending receipts, including one left behind by a
// crash, and MUST NOT be narrowed (AC-004).
func (db *DB) ListPendingSubmissions(ctx context.Context) ([]SubmissionReceipt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE effect_state IN ('pending','failed') OR engineering_state='pending' OR acceptance_state='pending' OR plan_state='pending' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubmissionReceipt{}
	for rows.Next() {
		r, err := scanSubmissionReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListPendingSubmissionsForAttempt scopes discovery to exactly one trusted
// run and attempt. Foreground recovery call sites (submission checkpoints,
// session Close, terminal Validate intake) use this so a broken pending
// receipt belonging to an unrelated run/attempt can never be discovered,
// processed or failed on by this one (AC-003).
func (db *DB) ListPendingSubmissionsForAttempt(ctx context.Context, runID int64, attempt string) ([]SubmissionReceipt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+receiptColumns+` FROM submission_receipts WHERE run_id=? AND attempt=? AND (effect_state IN ('pending','failed') OR engineering_state='pending' OR acceptance_state='pending' OR plan_state='pending') ORDER BY id`, runID, attempt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubmissionReceipt{}
	for rows.Next() {
		r, err := scanSubmissionReceipt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func submissionState(s string, allowed ...string) bool { return slices.Contains(allowed, s) }
