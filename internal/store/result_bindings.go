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

// ResultStoryRevision binds an application context to exact frozen requirements.
type ResultStoryRevision struct {
	Story    string `json:"story"`
	Revision string `json:"revision"`
}

// ResultAttempt is supplied by the factory; mutable Plan draft contents are not
// part of its identity. It requires a real run but no managed generation.
type ResultAttempt struct {
	Workflow string

	Task, Mode, WorkContext, Attempt                  string
	RunID                                             int64
	SchemaVersion, WorkflowVersion, SubmissionVersion int
	FrozenStories                                     []ResultStoryRevision
}

// ResultBinding grants one attributed reader and, while active, a writer. Values
// are rechecked against the store; constructing one does not grant authority.
type ResultBinding struct {
	DoctorAttemptID string
	JudgeAttemptID  string
	Workflow        string

	ResultID, RunID                                   int64
	Task, Mode, WorkContext, ContextHash, Attempt     string
	SchemaVersion, WorkflowVersion, SubmissionVersion int
}

// ErrResultAuthority means an attempt no longer has the requested authority.
var ErrResultAuthority = errors.New("store: stale or unauthorized result binding")

// ErrResultCorrelation means an immutable request identity was reused differently.
var ErrResultCorrelation = errors.New("store: result request correlation conflict")

// ResultRevisionConflict exposes optimistic concurrency without content leakage.
type ResultRevisionConflict struct{ Expected, Current int64 }

func (e *ResultRevisionConflict) Error() string {
	return fmt.Sprintf("store: result revision conflict: expected %d, current %d", e.Expected, e.Current)
}

// ResultContextHash normalizes exact frozen requirements without mutable drafts.
func ResultContextHash(a ResultAttempt) (string, error) {
	scopes := append([]ResultStoryRevision{}, a.FrozenStories...)
	slices.SortFunc(scopes, func(x, y ResultStoryRevision) int { return strings.Compare(x.Story, y.Story) })
	for i, s := range scopes {
		if s.Story == "" || !validSubmissionDigest(s.Revision) || (i > 0 && scopes[i-1].Story == s.Story) {
			return "", fmt.Errorf("store: invalid result frozen scope")
		}
	}
	b, err := json.Marshal(struct {
		Context                      string
		Schema, Workflow, Submission int
		Stories                      []ResultStoryRevision
	}{Context: a.WorkContext, Schema: a.SchemaVersion, Workflow: a.WorkflowVersion, Submission: a.SubmissionVersion, Stories: scopes})
	if err != nil {
		return "", fmt.Errorf("store: encode result context: %w", err)
	}
	return submissionDigest(b), nil
}

// BindResultAttempt continues the same context, revoking older task writers.
// Replaying a revoked attempt never resurrects it, even after a fresh run.
func (db *DB) BindResultAttempt(ctx context.Context, a ResultAttempt) (ResultBinding, error) {
	var b ResultBinding
	if a.RunID < 1 || strings.TrimSpace(a.Task) == "" || strings.TrimSpace(a.WorkContext) == "" || a.Attempt == "" || len(a.Attempt) > 256 || len(a.WorkContext) > 8192 || a.SchemaVersion != 1 || (a.WorkflowVersion != 1 && a.WorkflowVersion != 2) || a.SubmissionVersion != 2 || !validResultWorkflow(a.Workflow, a.Mode) {
		return b, fmt.Errorf("store: invalid result attempt")
	}
	hash, err := ResultContextHash(a)
	if err != nil {
		return b, err
	}
	err = db.withTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND r.mode=? AND r.workflow=? AND r.status='running' AND s.task_ref=?`, a.RunID, a.Mode, a.Workflow, a.Task).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrResultAuthority
		}
		old, err := readResultBinding(ctx, tx, a.RunID, a.Attempt, false)
		if err == nil {
			if old.Task != a.Task || old.Mode != a.Mode || old.Workflow != a.Workflow || old.ContextHash != hash {
				return ErrResultAuthority
			}
			if err := checkResultAccess(ctx, tx, old, true); err != nil {
				return err
			}
			b = old
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_results(task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,workflow) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(task,mode,context_hash) DO NOTHING`, a.Task, a.Mode, a.WorkContext, hash, a.SchemaVersion, a.WorkflowVersion, a.SubmissionVersion, a.Workflow); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM application_results WHERE task=? AND mode=? AND context_hash=? AND workflow=?`, a.Task, a.Mode, hash, a.Workflow).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE result_id IN (SELECT id FROM application_results WHERE task=? AND workflow NOT IN ('doctor','judge'))`, a.Task); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active) VALUES(?,?,?,1)`, a.RunID, a.Attempt, id); err != nil {
			return err
		}
		b, err = readResultBinding(ctx, tx, a.RunID, a.Attempt, false)
		return err
	})
	return b, err
}
func readResultBinding(ctx context.Context, tx *sql.Tx, runID int64, attempt string, active bool) (ResultBinding, error) {
	var b ResultBinding
	query := `SELECT r.id,COALESCE(a.run_id,0),r.task,r.mode,r.work_context,r.context_hash,a.attempt,r.schema_version,r.workflow_version,r.submission_version,r.workflow,COALESCE(a.doctor_attempt_id,''),COALESCE(a.judge_attempt_id,'') FROM application_results r JOIN application_result_attempts a ON a.result_id=r.id WHERE COALESCE(a.run_id,0)=? AND a.attempt=?`
	if active {
		query += ` AND a.active=1`
	}
	err := tx.QueryRowContext(ctx, query, runID, attempt).Scan(&b.ResultID, &b.RunID, &b.Task, &b.Mode, &b.WorkContext, &b.ContextHash, &b.Attempt, &b.SchemaVersion, &b.WorkflowVersion, &b.SubmissionVersion, &b.Workflow, &b.DoctorAttemptID, &b.JudgeAttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func checkResultAccess(ctx context.Context, tx *sql.Tx, b ResultBinding, write bool) error {
	if lease, ok := ctx.Value(mcpLeaseKey{}).(MCPLease); ok {
		if lease.Binding != b {
			return ErrResultAuthority
		}
		if err := checkMCPLease(ctx, tx, lease, nowUTC()); err != nil {
			return err
		}
		write = true // MCP never inherits the internal historical-reader exemption.
	}
	old, err := readResultBinding(ctx, tx, b.RunID, b.Attempt, write)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrResultAuthority
		}
		return err
	}
	if old != b {
		return ErrResultAuthority
	}
	if write && b.JudgeAttemptID != "" {
		return checkJudgeResultWriter(ctx, tx, b)
	}
	if write && b.DoctorAttemptID != "" {
		return checkDoctorResultWriter(ctx, tx, b)
	}
	if write {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND r.mode=? AND r.workflow=? AND r.status='running' AND s.task_ref=?`, b.RunID, b.Mode, b.Workflow, b.Task).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return ErrResultAuthority
		}
	}
	return nil
}

// CheckResultWriter rechecks authority without creating a request or revision.
func (db *DB) CheckResultWriter(ctx context.Context, b ResultBinding) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return checkResultAccess(ctx, tx, b, true) })
}

// EndResultAttempt revokes writing while preserving attributed historical reads.
func (db *DB) EndResultAttempt(ctx context.Context, b ResultBinding) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE application_result_attempts SET active=0 WHERE COALESCE(run_id,0)=? AND attempt=?`, b.RunID, b.Attempt)
		return err
	})
}

// CheckResultReader checks historical attribution without granting writing.
func (db *DB) CheckResultReader(ctx context.Context, b ResultBinding) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return checkResultAccess(ctx, tx, b, false) })
}

// ResultHistoricalScopes returns bounded earlier attempts of this exact result
// context. Callers explicitly install these read grants; none restores writing.
func (db *DB) ResultHistoricalScopes(ctx context.Context, b ResultBinding) ([]FamilyScope, error) {
	var scopes []FamilyScope
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if err := checkResultAccess(ctx, tx, b, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT run_id,attempt FROM application_result_attempts WHERE result_id=? AND NOT (run_id=? AND attempt=?) ORDER BY run_id DESC,rowid DESC LIMIT 63`, b.ResultID, b.RunID, b.Attempt)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var scope FamilyScope
			if err := rows.Scan(&scope.RunID, &scope.Attempt); err != nil {
				return err
			}
			scope.Task = b.Task
			scopes = append(scopes, scope)
		}
		return rows.Err()
	})
	return scopes, err
}

func validResultWorkflow(workflow, mode string) bool {
	if workflow == "bug" {
		return mode == ""
	}
	return workflow == "" && submissionState(mode, "research", "plan", "implement", "validate", "spike")
}
