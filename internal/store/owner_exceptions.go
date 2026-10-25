package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// ExceptionObligation is an exact expanded obligation, never a wildcard to be
// re-evaluated on replay. Finding and snapshot remain the original factual data.
type ExceptionObligation struct {
	EvidenceRunID  int64               `json:"evidence_run_id,omitempty"`
	EvidenceDigest string              `json:"evidence_digest,omitempty"`
	Checks         []AcceptanceFinding `json:"checks,omitempty"`
	Story          string              `json:"story,omitempty"`
	IssueID        int64               `json:"issue_id,omitempty"`
	Kind           string              `json:"kind"`
	Name           string              `json:"name"`
	Revision       string              `json:"revision,omitempty"`
	Snapshot       string              `json:"snapshot,omitempty"`
	Finding        AcceptanceFinding   `json:"finding"`
}

// OwnerException retains explicit owner authority and its immutable expanded scope.
type OwnerException struct {
	ID                         int64
	Parent                     string
	Generation                 int
	CommentID, ActorID         int64
	RequestJSON, ExpansionJSON string
	ReportCommentID            int64
}

// GetOwnerException loads an exception by parent and source comment identity.
func (db *DB) GetOwnerException(ctx context.Context, parent string, commentID int64) (OwnerException, error) {
	var e OwnerException
	err := db.sql.QueryRowContext(ctx, `SELECT id,parent_task,generation,comment_id,actor_id,request_json,expansion_json,report_comment_id FROM owner_exceptions WHERE parent_task=? AND comment_id=?`, parent, commentID).Scan(&e.ID, &e.Parent, &e.Generation, &e.CommentID, &e.ActorID, &e.RequestJSON, &e.ExpansionJSON, &e.ReportCommentID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// LatestAcceptance loads historical acceptance or the latest unresolved assessment.
func (db *DB) LatestAcceptance(ctx context.Context, parent string, generation int, story string) (AcceptanceDecision, error) {
	d, err := scanAcceptance(db.sql.QueryRowContext(ctx, `SELECT `+acceptanceColumns+` FROM acceptance_decisions WHERE parent_task=? AND generation=? AND story_key=? ORDER BY accepted DESC,id DESC LIMIT 1`, parent, generation, story))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// RetainOwnerException atomically freezes the authority and exact expansion
// before creating ordered projection debt. Original decisions are never changed.
func (db *DB) RetainOwnerException(ctx context.Context, e OwnerException, decisions []AcceptanceDecision) error {
	if e.ActorID < 1 || e.CommentID < 1 || e.Generation < 1 || !json.Valid([]byte(e.RequestJSON)) || !json.Valid([]byte(e.ExpansionJSON)) {
		return fmt.Errorf("invalid owner exception")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var oldActor int64
		var oldRequest string
		err := tx.QueryRowContext(ctx, `SELECT actor_id,request_json FROM owner_exceptions WHERE parent_task=? AND comment_id=?`, e.Parent, e.CommentID).Scan(&oldActor, &oldRequest)
		if err == nil {
			if oldActor != e.ActorID || oldRequest != e.RequestJSON {
				return fmt.Errorf("conflicting exception replay")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var version, generation int
		if err := tx.QueryRowContext(ctx, `SELECT workflow_version,generation FROM managed_features WHERE parent_task=?`, e.Parent).Scan(&version, &generation); err != nil {
			return err
		}
		if version != WorkflowSeparated || generation != e.Generation {
			return fmt.Errorf("exception requires current separated generation")
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO owner_exceptions(parent_task,generation,comment_id,actor_id,request_json,expansion_json) VALUES(?,?,?,?,?,?)`, e.Parent, e.Generation, e.CommentID, e.ActorID, e.RequestJSON, e.ExpansionJSON)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for _, d := range decisions {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM approved_story_revisions r JOIN managed_stories s USING(parent_task,generation,story_key) WHERE r.parent_task=? AND r.generation=? AND r.story_key=? AND r.digest=? AND r.body=? AND r.parent_id=? AND s.issue_id=? AND s.issue_number=?`, e.Parent, e.Generation, d.Story, d.Revision, d.FrozenBody, d.ParentID, d.IssueID, d.IssueNumber).Scan(&count); err != nil {
				return err
			}
			if count != 1 || d.Parent != e.Parent || d.Generation != e.Generation {
				return fmt.Errorf("exception frozen scope mismatch")
			}
			var f AcceptanceFindings
			if err := json.Unmarshal([]byte(d.FindingsJSON), &f); err != nil {
				return err
			}
			if d.Accepted != f.Accepted() {
				return fmt.Errorf("exception acceptance mismatch")
			}
			// A pending older projection must finish before this newer one. Keeping
			// both debts preserves exactly-once reports and crash recovery ordering.
			_, err = tx.ExecContext(ctx, `INSERT INTO acceptance_decisions(run_id,parent_task,generation,story_key,issue_number,issue_id,parent_id,revision,snapshot,frozen_body,findings_json,accepted,exception_id) VALUES(NULLIF(?,0),?,?,?,?,?,?,?,?,?,?,?,?)`, d.RunID, d.Parent, d.Generation, d.Story, d.IssueNumber, d.IssueID, d.ParentID, d.Revision, d.Snapshot, d.FrozenBody, d.FindingsJSON, d.Accepted, id)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveOwnerExceptionReport records a confirmed owner-exception acknowledgement.
func (db *DB) SaveOwnerExceptionReport(ctx context.Context, id, commentID int64) error {
	if commentID < 1 {
		return fmt.Errorf("invalid exception report identity")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_exceptions SET report_comment_id=? WHERE id=? AND report_comment_id=0`, commentID, id)
	return err
}

// ListReadinessExceptions returns immutable exact readiness expansions for the
// delivery gate. Callers match concrete child/evidence keys, never wildcard text.
func (db *DB) ListReadinessExceptions(ctx context.Context, parent string, generation int) ([]ExceptionObligation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT expansion_json FROM owner_exceptions WHERE parent_task=? AND generation=? AND validate_instance_id IS NULL ORDER BY id`, parent, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExceptionObligation
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var scope []ExceptionObligation
		if err := json.Unmarshal([]byte(raw), &scope); err != nil {
			return nil, err
		}
		for _, o := range scope {
			if o.Kind == "readiness" {
				out = append(out, o)
			}
		}
	}
	return out, rows.Err()
}
