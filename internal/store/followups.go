package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// FollowupProposal retains the exact sanitized content approved by the owner.
type FollowupProposal struct {
	AttachmentComment                               int64
	Key, Parent, Title, Body                        string
	Generation                                      int
	RunID, ApprovalComment, ActorID, IssueID        int64
	IssueNumber                                     int
	Attach, Attempted, Attached, Proposed, Reported bool
	Diagnostic                                      string
}

// RetainFollowup persists concrete proposal content before approval or publication.
func (db *DB) RetainFollowup(ctx context.Context, p FollowupProposal) error {
	if p.Key == "" || p.Parent == "" || p.Generation < 1 || p.RunID < 1 || p.Title == "" || p.Body == "" {
		return fmt.Errorf("incomplete followup proposal")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO followup_proposals(proposal_key,parent_task,generation,run_id,title,body) VALUES(?,?,?,?,?,?) ON CONFLICT(proposal_key) DO NOTHING`, p.Key, p.Parent, p.Generation, p.RunID, p.Title, p.Body)
	if err != nil {
		return err
	}
	old, err := db.GetFollowup(ctx, p.Key)
	if err != nil {
		return err
	}
	if old.Parent != p.Parent || old.Generation != p.Generation || old.RunID != p.RunID || old.Title != p.Title || old.Body != p.Body {
		return fmt.Errorf("followup immutable content conflict")
	}
	return nil
}

const followupColumns = `proposal_key,parent_task,generation,run_id,title,body,approval_comment,actor_id,attachment_comment,attach,attempted,issue_id,issue_number,attached,proposed,reported,diagnostic`

func scanFollowup(s interface{ Scan(...any) error }) (p FollowupProposal, err error) {
	err = s.Scan(&p.Key, &p.Parent, &p.Generation, &p.RunID, &p.Title, &p.Body, &p.ApprovalComment, &p.ActorID, &p.AttachmentComment, &p.Attach, &p.Attempted, &p.IssueID, &p.IssueNumber, &p.Attached, &p.Proposed, &p.Reported, &p.Diagnostic)
	return
}

// GetFollowup loads an immutable proposal and its current effect debt.
func (db *DB) GetFollowup(ctx context.Context, key string) (FollowupProposal, error) {
	p, err := scanFollowup(db.sql.QueryRowContext(ctx, `SELECT `+followupColumns+` FROM followup_proposals WHERE proposal_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

// ListFollowups returns all retained proposals in stable key order.
func (db *DB) ListFollowups(ctx context.Context) ([]FollowupProposal, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+followupColumns+` FROM followup_proposals ORDER BY proposal_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FollowupProposal
	for rows.Next() {
		p, err := scanFollowup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ApproveFollowup records explicit creation or subsequent attachment authority.
func (db *DB) ApproveFollowup(ctx context.Context, key string, comment, actor int64, attach bool) error {
	if comment < 1 || actor < 1 {
		return fmt.Errorf("invalid followup approval identity")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanFollowup(tx.QueryRowContext(ctx, `SELECT `+followupColumns+` FROM followup_proposals WHERE proposal_key=?`, key))
		if err != nil {
			return err
		}
		if p.ApprovalComment > 0 {
			if p.ActorID != actor || (p.Attach && !attach) {
				return fmt.Errorf("followup approval scope conflict")
			}
			if attach && !p.Attach {
				_, err = tx.ExecContext(ctx, `UPDATE followup_proposals SET attach=1,attachment_comment=?,reported=0 WHERE proposal_key=?`, comment, key)
				return err
			}
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE followup_proposals SET approval_comment=?,actor_id=?,attach=?,attachment_comment=? WHERE proposal_key=?`, comment, actor, attach, attachmentComment(comment, attach), key)
		return err
	})
}

// SaveFollowupEffects changes only external effect debt, never content or authority.
func (db *DB) SaveFollowupEffects(ctx context.Context, p FollowupProposal) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE followup_proposals SET attempted=?,issue_id=?,issue_number=?,attached=?,proposed=?,reported=?,diagnostic=? WHERE proposal_key=?`, p.Attempted, p.IssueID, p.IssueNumber, p.Attached, p.Proposed, p.Reported, p.Diagnostic, p.Key)
	return err
}

// ListFollowupAssessments loads assessments whose follow-up findings are not
// materialized. Submission-v2 marker rows are excluded: their findings live in
// submission receipts and they are not v1 validation contexts.
func (db *DB) ListFollowupAssessments(ctx context.Context) ([]ValidationAssessment, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT run_id,parent_task,generation,context_json,result_json FROM validation_assessments WHERE context_json<>? AND run_id NOT IN (SELECT run_id FROM followup_materializations) ORDER BY run_id`, SubmissionAssessmentMarker)
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

func attachmentComment(comment int64, attach bool) int64 {
	if attach {
		return comment
	}
	return 0
}

// MarkFollowupsMaterialized records completed proposal extraction from an assessment.
func (db *DB) MarkFollowupsMaterialized(ctx context.Context, runID int64) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO followup_materializations(run_id) VALUES(?) ON CONFLICT(run_id) DO NOTHING`, runID)
	return err
}

// ListPendingFollowupParents lists parents with materialization or publication debt.
func (db *DB) ListPendingFollowupParents(ctx context.Context) ([]string, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT parent_task FROM validation_assessments WHERE context_json<>? AND run_id NOT IN (SELECT run_id FROM followup_materializations) UNION SELECT parent_task FROM followup_proposals WHERE proposed=0 OR (approval_comment>0 AND reported=0)`, SubmissionAssessmentMarker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var parent string
		if err := rows.Scan(&parent); err != nil {
			return nil, err
		}
		out = append(out, parent)
	}
	return out, rows.Err()
}
