package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// AcceptanceFinding preserves the factual assessment. WaiverReason is supplied
// only by the factory's authenticated owner exception path, never agent output.
type AcceptanceFinding struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	Evidence       string `json:"evidence"`
	WaiverReason   string `json:"waiver_reason,omitempty"`
}

// AcceptanceFindings groups factual criteria and review obligations.
type AcceptanceFindings struct {
	Criteria []AcceptanceFinding `json:"criteria"`
	Reviews  []AcceptanceFinding `json:"reviews"`
}

// Accepted reports whether every required obligation is satisfied or owner-waived.
func (f AcceptanceFindings) Accepted() bool {
	if len(f.Criteria) == 0 || len(f.Reviews) != 3 {
		return false
	}
	for _, c := range f.Criteria {
		if c.Status != "satisfied" && c.WaiverReason == "" {
			return false
		}
	}
	for _, c := range f.Reviews {
		// A gap is not a defect, but it is not satisfied review evidence either.
		if (c.Classification == "blocking" || c.Classification == "gap") && c.Status != "satisfied" && c.WaiverReason == "" {
			return false
		}
	}
	return true
}

// AcceptanceDecision is both the retained attributed assessment and ordered
// report/projection/close debt. Final historical decisions are never rewritten.
type AcceptanceDecision struct {
	ID, RunID                                    int64
	ExceptionID                                  int64
	Parent                                       string
	Generation                                   int
	Story                                        string
	IssueNumber                                  int
	IssueID, ParentID                            int64
	Revision, Snapshot, FrozenBody, FindingsJSON string
	Accepted                                     bool
	CommentID                                    int64
	Projected, Delivered                         bool
	Diagnostic                                   string
}

const acceptanceColumns = `id,coalesce(run_id,0),parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,snapshot,frozen_body,findings_json,accepted,comment_id,projected,delivered,diagnostic,exception_id`

func scanAcceptance(s interface{ Scan(...any) error }) (AcceptanceDecision, error) {
	var d AcceptanceDecision
	err := s.Scan(&d.ID, &d.RunID, &d.Parent, &d.Generation, &d.Story, &d.IssueNumber, &d.IssueID, &d.ParentID, &d.Revision, &d.Snapshot, &d.FrozenBody, &d.FindingsJSON, &d.Accepted, &d.CommentID, &d.Projected, &d.Delivered, &d.Diagnostic, &d.ExceptionID)
	return d, err
}

// RetainAcceptanceDecision binds a factory-validated assessment to its frozen
// target. Duplicate ingestion preserves both effect progress and original facts.
func (db *DB) RetainAcceptanceDecision(ctx context.Context, d AcceptanceDecision) error {
	var f AcceptanceFindings
	if err := json.Unmarshal([]byte(d.FindingsJSON), &f); err != nil {
		return err
	}
	if d.RunID < 1 || d.Generation < 1 || d.IssueID < 1 || d.IssueNumber < 1 || d.ParentID < 1 || len(d.Revision) != 64 || d.FrozenBody == "" || d.Accepted != f.Accepted() {
		return fmt.Errorf("invalid acceptance decision")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM validation_assessments a JOIN managed_features f ON f.parent_task=a.parent_task JOIN managed_stories s ON s.parent_task=a.parent_task AND s.generation=a.generation JOIN approved_story_revisions r ON r.parent_task=s.parent_task AND r.generation=s.generation AND r.story_key=s.story_key WHERE a.run_id=? AND a.parent_task=? AND a.generation=? AND f.workflow_version=2 AND s.story_key=? AND s.issue_number=? AND s.issue_id=? AND r.parent_id=? AND r.digest=? AND r.body=?`, d.RunID, d.Parent, d.Generation, d.Story, d.IssueNumber, d.IssueID, d.ParentID, d.Revision, d.FrozenBody).Scan(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("acceptance frozen attribution mismatch")
		}
		old, err := scanAcceptance(tx.QueryRowContext(ctx, `SELECT `+acceptanceColumns+` FROM acceptance_decisions WHERE run_id=? AND story_key=? AND exception_id=0`, d.RunID, d.Story))
		if err == nil {
			if old.Parent != d.Parent || old.Generation != d.Generation || old.Revision != d.Revision || old.Snapshot != d.Snapshot || old.FindingsJSON != d.FindingsJSON {
				return fmt.Errorf("conflicting acceptance replay")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// An accepted decision is historical, even while projection is pending.
		// A later assessment remains retained without replacing that sign-off.
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM acceptance_decisions WHERE parent_task=? AND generation=? AND story_key=? AND accepted=1`, d.Parent, d.Generation, d.Story).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO acceptance_decisions(run_id,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,snapshot,frozen_body,findings_json,accepted) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, d.RunID, d.Parent, d.Generation, d.Story, d.IssueNumber, d.IssueID, d.ParentID, d.Revision, d.Snapshot, d.FrozenBody, d.FindingsJSON, d.Accepted)
		return err
	})
}

// ListPendingAcceptance loads ordered acceptance projection and closure debt.
func (db *DB) ListPendingAcceptance(ctx context.Context) ([]AcceptanceDecision, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+acceptanceColumns+` FROM acceptance_decisions WHERE delivered=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AcceptanceDecision
	for rows.Next() {
		d, err := scanAcceptance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListUnprojectedValidationAssessments loads assessments awaiting decision materialization.
func (db *DB) ListUnprojectedValidationAssessments(ctx context.Context) ([]ValidationAssessment, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT run_id,parent_task,generation,context_json,result_json FROM validation_assessments WHERE materialized=0 ORDER BY run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ValidationAssessment
	for rows.Next() {
		var a ValidationAssessment
		if err := rows.Scan(&a.RunID, &a.Parent, &a.Generation, &a.ContextJSON, &a.ResultJSON); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAcceptanceComment records the confirmed acceptance report identity.
func (db *DB) SaveAcceptanceComment(ctx context.Context, id, commentID int64) error {
	if commentID < 1 {
		return fmt.Errorf("invalid acceptance comment identity")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE acceptance_decisions SET comment_id=? WHERE id=? AND comment_id=0`, commentID, id)
	return err
}

// SaveAcceptanceProjection records confirmed checkbox projection after its report.
func (db *DB) SaveAcceptanceProjection(ctx context.Context, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE acceptance_decisions SET projected=1,diagnostic='' WHERE id=? AND comment_id>0`, id)
	return err
}

// SaveAcceptanceDiagnostic retains a recoverable acceptance effect failure.
func (db *DB) SaveAcceptanceDiagnostic(ctx context.Context, id int64, diagnostic string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE acceptance_decisions SET diagnostic=? WHERE id=?`, diagnostic, id)
	return err
}

// DeliverAcceptance settles confirmed projection and closure without changing findings.
func (db *DB) DeliverAcceptance(ctx context.Context, id int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanAcceptance(tx.QueryRowContext(ctx, `SELECT `+acceptanceColumns+` FROM acceptance_decisions WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.CommentID < 1 || !d.Projected {
			return fmt.Errorf("acceptance projection incomplete")
		}
		if d.Accepted {
			_, err = tx.ExecContext(ctx, `UPDATE managed_stories SET state='completed' WHERE parent_task=? AND generation=? AND story_key=? AND issue_id=? AND body_hash=?`, d.Parent, d.Generation, d.Story, d.IssueID, d.Revision)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE acceptance_decisions SET delivered=1,diagnostic='' WHERE id=?`, id)
		return err
	})
}

// MaterializeValidationAssessment marks an assessment whose decisions are durably retained.
func (db *DB) MaterializeValidationAssessment(ctx context.Context, runID int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE validation_assessments SET materialized=1 WHERE run_id=?`, runID)
	return err
}
