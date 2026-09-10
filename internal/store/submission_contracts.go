package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// FeatureContract versions content independently from execution workflow.
type FeatureContract struct {
	Parent                                        string
	Generation, ContentVersion, SubmissionVersion int
}

// GetFeatureContract reads the currently selected persisted contract.
func (db *DB) GetFeatureContract(ctx context.Context, parent string) (FeatureContract, error) {
	var c FeatureContract
	err := db.sql.QueryRowContext(ctx, `SELECT parent_task,generation,content_version,submission_version FROM managed_features WHERE parent_task=?`, parent).Scan(&c.Parent, &c.Generation, &c.ContentVersion, &c.SubmissionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// GetGenerationContract reads immutable historical dispatch independently of the
// current feature generation and workflow.
func (db *DB) GetGenerationContract(ctx context.Context, parent string, generation int) (FeatureContract, error) {
	c := FeatureContract{Parent: parent, Generation: generation}
	err := db.sql.QueryRowContext(ctx, `SELECT content_version,submission_version FROM submission_generations WHERE parent_task=? AND generation=?`, parent, generation).Scan(&c.ContentVersion, &c.SubmissionVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// BindFeatureContract records an immutable generation binding. Selecting a new
// version is allowed only before this generation has a binding or active run;
// explicit upgrade must start a new safe generation through existing admission.
func (db *DB) BindFeatureContract(ctx context.Context, c FeatureContract) error {
	if c.Parent == "" || c.Generation < 1 || c.ContentVersion != c.SubmissionVersion || (c.ContentVersion != 1 && c.ContentVersion != 2) {
		return fmt.Errorf("store: invalid content contract")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var content, submission int
		err := tx.QueryRowContext(ctx, `SELECT content_version,submission_version FROM submission_generations WHERE parent_task=? AND generation=?`, c.Parent, c.Generation).Scan(&content, &submission)
		if err == nil {
			if content != c.ContentVersion || submission != c.SubmissionVersion {
				return fmt.Errorf("store: immutable generation contract conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM managed_features WHERE parent_task=? AND generation=? AND NOT EXISTS(SELECT 1 FROM runs r JOIN sessions s ON s.id=r.session_id WHERE s.task_ref=? AND r.status='running')`, c.Parent, c.Generation, c.Parent).Scan(&state); err != nil {
			return fmt.Errorf("store: contract admission: %w", err)
		}
		if state != "planning" {
			return fmt.Errorf("store: contract requires planning generation")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO submission_generations VALUES(?,?,?,?)`, c.Parent, c.Generation, c.ContentVersion, c.SubmissionVersion); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE managed_features SET content_version=?,submission_version=? WHERE parent_task=? AND generation=?`, c.ContentVersion, c.SubmissionVersion, c.Parent, c.Generation)
		return err
	})
}

// SubmissionProjection is an immutable derivation of exact issue bytes.
type SubmissionProjection struct {
	Parent, Story, Revision, Title, Body string
	Generation, ContentVersion           int
	IssueID                              int64
	Content                              []byte
	Criteria, RetiredCriteria            []string
}

// PutSubmissionProjection preserves source bytes, typed derivation and monotonic
// ID history. It does not modify the issue, approval, or historical acceptance.
func (db *DB) PutSubmissionProjection(ctx context.Context, p SubmissionProjection) error {
	if p.Parent == "" || p.Story == "" || p.Generation < 1 || p.IssueID < 1 || p.ContentVersion != 2 || submissionDigest([]byte(p.Title+"\x00"+p.Body)) != p.Revision || len(p.Body) > 65536 || !json.Valid(p.Content) || len(p.Content) > 4<<20 {
		return fmt.Errorf("store: invalid issue projection")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT content_version FROM submission_generations WHERE parent_task=? AND generation=?`, p.Parent, p.Generation).Scan(&version); err != nil {
			return err
		}
		if version != p.ContentVersion {
			return fmt.Errorf("store: projection contract mismatch")
		}
		var old []byte
		var oldIssueID int64
		err := tx.QueryRowContext(ctx, `SELECT content,issue_id FROM submission_projections WHERE parent_task=? AND generation=? AND story_key=? AND revision=?`, p.Parent, p.Generation, p.Story, p.Revision).Scan(&old, &oldIssueID)
		if err == nil {
			if string(old) != string(p.Content) || oldIssueID != p.IssueID {
				return fmt.Errorf("store: immutable projection conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO submission_projections VALUES(?,?,?,?,?,?,?,?,?)`, p.Parent, p.Generation, p.Story, p.IssueID, p.Revision, p.ContentVersion, p.Title, p.Body, p.Content); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range p.Criteria {
			if !submissionCriterionID.MatchString(id) || seen[id] {
				return fmt.Errorf("store: invalid criterion identity")
			}
			seen[id] = true
			if _, err := tx.ExecContext(ctx, `INSERT INTO submission_criterion_history(parent_task,story_key,criterion_id,first_revision) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, p.Parent, p.Story, id, p.Revision); err != nil {
				return err
			}
			var retired string
			if err := tx.QueryRowContext(ctx, `SELECT retired_revision FROM submission_criterion_history WHERE parent_task=? AND story_key=? AND criterion_id=?`, p.Parent, p.Story, id).Scan(&retired); err != nil {
				return err
			}
			if retired != "" {
				return fmt.Errorf("store: retired criterion cannot be reused")
			}
		}
		for _, id := range p.RetiredCriteria {
			if !submissionCriterionID.MatchString(id) || seen[id] {
				return fmt.Errorf("store: conflicting retired criterion")
			}
			res, err := tx.ExecContext(ctx, `UPDATE submission_criterion_history SET retired_revision=CASE WHEN retired_revision='' THEN ? ELSE retired_revision END WHERE parent_task=? AND story_key=? AND criterion_id=?`, p.Revision, p.Parent, p.Story, id)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("store: cannot retire unknown criterion")
			}
		}
		return nil
	})
}

// GetSubmissionProjection returns only the exact requested issue revision.
func (db *DB) GetSubmissionProjection(ctx context.Context, parent string, generation int, story, revision string) (SubmissionProjection, error) {
	p := SubmissionProjection{Parent: parent, Generation: generation, Story: story, Revision: revision}
	err := db.sql.QueryRowContext(ctx, `SELECT issue_id,content_version,title,body,content FROM submission_projections WHERE parent_task=? AND generation=? AND story_key=? AND revision=?`, parent, generation, story, revision).Scan(&p.IssueID, &p.ContentVersion, &p.Title, &p.Body, &p.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// CriterionHistory records immutable allocation and permanent retirement.
type CriterionHistory struct{ ID, FirstRevision, RetiredRevision string }

// FindSubmissionProjection reads an exact previously published revision across
// generations. Replanning may change generation but cannot rewrite source bytes.
func (db *DB) FindSubmissionProjection(ctx context.Context, parent, story, revision string, generation int) (SubmissionProjection, error) {
	var found int
	err := db.sql.QueryRowContext(ctx, `SELECT generation FROM submission_projections WHERE parent_task=? AND story_key=? AND revision=? AND generation<=? ORDER BY generation DESC LIMIT 1`, parent, story, revision, generation).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return SubmissionProjection{}, ErrNotFound
	}
	if err != nil {
		return SubmissionProjection{}, err
	}
	return db.GetSubmissionProjection(ctx, parent, found, story, revision)
}

// ListCriterionHistory includes retired IDs so subsequent edits cannot reuse them.
func (db *DB) ListCriterionHistory(ctx context.Context, parent, story string) ([]CriterionHistory, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT criterion_id,first_revision,retired_revision FROM submission_criterion_history WHERE parent_task=? AND story_key=? ORDER BY criterion_id`, parent, story)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CriterionHistory{}
	for rows.Next() {
		var h CriterionHistory
		if err := rows.Scan(&h.ID, &h.FirstRevision, &h.RetiredRevision); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
