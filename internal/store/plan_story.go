package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PlanStoryDraft binds a durable ordinal and remote identity to one parent key.
type PlanStoryDraft struct {
	Parent, Key                           string
	Ordinal, IssueID                      int64
	IssueNumber                           int
	Revision, Candidate, Stage, DraftJSON string
	Retired                               bool
}

// PlanStoryCandidate is append-only intent, persisted before forge mutation.
type PlanStoryCandidate struct{ Parent, RequestID, Key, Candidate, RequestJSON string }

// PlanStoryEffect is one append-only observation, including pre-mutation intent.
type PlanStoryEffect struct {
	ID                                int64
	Parent, RequestID, Phase, Outcome string
	IssueID                           int64
	IssueNumber                       int
	Detail                            string
	Revision                          string
}

// GetPlanStoryDraft returns the retained identity and current applied draft.
func (db *DB) GetPlanStoryDraft(ctx context.Context, parent, key string) (PlanStoryDraft, error) {
	var d PlanStoryDraft
	d.Parent = parent
	d.Key = key
	err := db.sql.QueryRowContext(ctx, `SELECT ordinal,issue_id,issue_number,revision,candidate,stage,draft_json,retired FROM plan_story_drafts WHERE parent=? AND story_key=?`, parent, key).Scan(&d.Ordinal, &d.IssueID, &d.IssueNumber, &d.Revision, &d.Candidate, &d.Stage, &d.DraftJSON, &d.Retired)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// ReservePlanStory monotonically allocates within the parent in one local
// transaction. Retired and unsuccessfully published reservations are never reused.
func (db *DB) ReservePlanStory(ctx context.Context, parent, key string) (PlanStoryDraft, error) {
	if parent == "" || key == "" {
		return PlanStoryDraft{}, errors.New("plan parent and key required")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) SELECT ?,?,COALESCE(MAX(ordinal),0)+1 FROM plan_story_drafts WHERE parent=? ON CONFLICT(parent,story_key) DO NOTHING`, parent, key, parent)
	if err != nil {
		return PlanStoryDraft{}, fmt.Errorf("reserve Plan ordinal: %w", err)
	}
	return db.GetPlanStoryDraft(ctx, parent, key)
}

// ListPlanStoryDrafts returns all reservations, including retired scope, in ordinal order.
func (db *DB) ListPlanStoryDrafts(ctx context.Context, parent string) ([]PlanStoryDraft, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT story_key,ordinal,issue_id,issue_number,revision,candidate,stage,draft_json,retired FROM plan_story_drafts WHERE parent=? ORDER BY ordinal`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanStoryDraft
	for rows.Next() {
		d := PlanStoryDraft{Parent: parent}
		if err := rows.Scan(&d.Key, &d.Ordinal, &d.IssueID, &d.IssueNumber, &d.Revision, &d.Candidate, &d.Stage, &d.DraftJSON, &d.Retired); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutPlanStoryCandidate persists immutable content and refuses a competing unresolved request.
func (db *DB) PutPlanStoryCandidate(ctx context.Context, c PlanStoryCandidate) error {
	if c.Parent == "" || c.RequestID == "" || c.Key == "" || c.Candidate == "" || c.RequestJSON == "" || len(c.RequestJSON) > 128*1024 {
		return errors.New("invalid Plan candidate")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_candidates(parent,request_id,story_key,candidate,request_json)
 SELECT ?,?,?,?,? WHERE NOT EXISTS (
 SELECT 1 FROM plan_story_candidates c WHERE c.parent=? AND c.story_key=? AND c.request_id<>?
 AND COALESCE((SELECT outcome FROM plan_story_effects e WHERE e.parent=c.parent AND e.request_id=c.request_id ORDER BY id DESC LIMIT 1),'pending') NOT IN ('applied','conflict','failed'))
 ON CONFLICT(parent,request_id) DO NOTHING`, c.Parent, c.RequestID, c.Key, c.Candidate, c.RequestJSON, c.Parent, c.Key, c.RequestID)
	if err != nil {
		return err
	}
	prior, err := db.GetPlanStoryCandidate(ctx, c.Parent, c.RequestID)
	if err != nil {
		return err
	}
	if prior != c {
		return errors.New("plan request identity reused with different content")
	}
	return nil
}

// GetPlanStoryCandidate returns immutable content for exact request replay.
func (db *DB) GetPlanStoryCandidate(ctx context.Context, parent, request string) (PlanStoryCandidate, error) {
	c := PlanStoryCandidate{Parent: parent, RequestID: request}
	err := db.sql.QueryRowContext(ctx, `SELECT story_key,candidate,request_json FROM plan_story_candidates WHERE parent=? AND request_id=?`, parent, request).Scan(&c.Key, &c.Candidate, &c.RequestJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// LatestPlanStoryEffect returns the most recent observation for a request.
func (db *DB) LatestPlanStoryEffect(ctx context.Context, parent, request string) (PlanStoryEffect, error) {
	e := PlanStoryEffect{Parent: parent, RequestID: request}
	err := db.sql.QueryRowContext(ctx, `SELECT id,phase,outcome,issue_id,issue_number,detail,revision FROM plan_story_effects WHERE parent=? AND request_id=? ORDER BY id DESC LIMIT 1`, parent, request).Scan(&e.ID, &e.Phase, &e.Outcome, &e.IssueID, &e.IssueNumber, &e.Detail, &e.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// AppendPlanStoryEffect records an observation without overwriting history.
func (db *DB) AppendPlanStoryEffect(ctx context.Context, e PlanStoryEffect) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO plan_story_effects(parent,request_id,phase,outcome,issue_id,issue_number,detail,revision) VALUES(?,?,?,?,?,?,?,?)`, e.Parent, e.RequestID, e.Phase, e.Outcome, e.IssueID, e.IssueNumber, e.Detail, e.Revision)
	return err
}

// BindPlanStoryIssue saves known creation before attachment. Identity cannot be
// replaced even when attachment failed or its response was lost.
func (db *DB) BindPlanStoryIssue(ctx context.Context, parent, key string, id int64, number int) error {
	if id < 1 || number < 1 {
		return errors.New("invalid Plan issue identity")
	}
	result, err := db.sql.ExecContext(ctx, `UPDATE plan_story_drafts SET issue_id=?,issue_number=? WHERE parent=? AND story_key=? AND (issue_id=0 OR (issue_id=? AND issue_number=?))`, id, number, parent, key, id, number)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("plan story issue identity conflict")
	}
	return nil
}

// BindPlanStoryShell binds a story to the issue created for it at promotion and
// records that issue's content revision, so the first draft updates it. A story
// that already has a candidate keeps its revision.
func (db *DB) BindPlanStoryShell(ctx context.Context, parent, key string, id int64, number int, revision string) error {
	if err := db.BindPlanStoryIssue(ctx, parent, key, id, number); err != nil {
		return err
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE plan_story_drafts SET revision=? WHERE parent=? AND story_key=? AND issue_id=? AND candidate=''`, revision, parent, key, id); err != nil {
		return fmt.Errorf("record story issue revision: %w", err)
	}
	return nil
}

// ApplyPlanStoryDraft atomically records content and its confirmed effect. The
// expected revision guards only local state; it is not a remote GitHub CAS.
func (db *DB) ApplyPlanStoryDraft(ctx context.Context, d PlanStoryDraft, expected string, e PlanStoryEffect) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE plan_story_drafts SET revision=?,candidate=?,stage=?,draft_json=?,retired=? WHERE parent=? AND story_key=? AND revision=? AND issue_id=? AND issue_number=?`, d.Revision, d.Candidate, d.Stage, d.DraftJSON, d.Retired, d.Parent, d.Key, expected, d.IssueID, d.IssueNumber)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("plan content changed during effect")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_story_effects(parent,request_id,phase,outcome,issue_id,issue_number,detail,revision) VALUES(?,?,?,?,?,?,?,?)`, e.Parent, e.RequestID, e.Phase, e.Outcome, e.IssueID, e.IssueNumber, e.Detail, e.Revision)
		if err != nil {
			return err
		}
		return nil
	})
}

// HasPlanStoryMutation distinguishes a retained mutation intent from failures
// during preflight observation; recovery never interprets absence as execution.
func (db *DB) HasPlanStoryMutation(ctx context.Context, parent, request, phase string) (bool, error) {
	var found bool
	err := db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plan_story_effects WHERE parent=? AND request_id=? AND phase=? AND outcome='uncertain')`, parent, request, phase).Scan(&found)
	return found, err
}
