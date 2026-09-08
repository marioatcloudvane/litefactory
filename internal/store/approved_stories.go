package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ApprovedStoryRevision preserves exactly the input selected by a promotion.
// Its identity and digest are independent of subsequent GitHub body edits.
type ApprovedStoryRevision struct {
	ManagedStory
	ParentID          int64
	Title, Body, Hash string
	FetchedAt         time.Time
}

// ApproveManagedStories freezes the whole unfinished set in the same transaction
// that excludes pending admitted discussions. No partial approval is observable.
func (db *DB) ApproveManagedStories(ctx context.Context, parent string, generation int, revisions []ApprovedStoryRevision) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE managed_features SET state='freezing',ready=0 WHERE parent_task=? AND generation=? AND state='planning' AND ready=1 AND NOT EXISTS (SELECT 1 FROM story_refinements r JOIN sessions s ON s.id=r.session_id WHERE s.task_ref=? AND r.consumed=0)`, parent, generation, parent)
		if err != nil {
			return fmt.Errorf("store: approve scope: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: plan is not ready or has pending refinements")
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM managed_stories WHERE parent_task=? AND generation=? AND state!='completed'`, parent, generation).Scan(&count); err != nil {
			return err
		}
		if count != len(revisions) || count == 0 {
			return fmt.Errorf("store: approval must contain every unfinished story")
		}
		for _, r := range revisions {
			if r.ParentTask != parent || r.Generation != generation || r.ParentID <= 0 || r.Title == "" || r.Body == "" || r.Hash == "" || r.FetchedAt.IsZero() {
				return fmt.Errorf("store: invalid approved revision")
			}
			res, err := tx.ExecContext(ctx, `UPDATE managed_stories SET state='frozen',body_hash=? WHERE parent_task=? AND generation=? AND story_key=? AND issue_number=? AND issue_id=? AND state='planning'`, r.Hash, parent, generation, r.Key, r.IssueNumber, r.IssueID)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("store: story changed during approval")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO approved_story_revisions(parent_task,generation,story_key,parent_id,title,body,digest,fetched_at) VALUES(?,?,?,?,?,?,?,?)`, parent, generation, r.Key, r.ParentID, r.Title, r.Body, r.Hash, r.FetchedAt.UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO story_scope_notices(parent_task,generation,story_key,kind) VALUES(?,?,?,'freeze')`, parent, generation, r.Key); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListApprovedStoryRevisions loads frozen content in story-key order without consulting live forge edits.
func (db *DB) ListApprovedStoryRevisions(ctx context.Context, parent string, generation int) ([]ApprovedStoryRevision, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT s.parent_task,s.generation,s.story_key,s.issue_number,s.issue_id,s.body_hash,s.dependencies,s.state,r.parent_id,r.title,r.body,r.digest,r.fetched_at FROM managed_stories s JOIN approved_story_revisions r USING(parent_task,generation,story_key) WHERE s.parent_task=? AND s.generation=? ORDER BY s.story_key`, parent, generation)
	if err != nil {
		return nil, fmt.Errorf("store: read approved scope: %w", err)
	}
	defer rows.Close()
	var out []ApprovedStoryRevision
	for rows.Next() {
		var r ApprovedStoryRevision
		var deps, at string
		if err := rows.Scan(&r.ParentTask, &r.Generation, &r.Key, &r.IssueNumber, &r.IssueID, &r.BodyHash, &deps, &r.State, &r.ParentID, &r.Title, &r.Body, &r.Hash, &at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(deps), &r.Dependencies); err != nil {
			return nil, err
		}
		r.FetchedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// StoryScopeNoticeDelivered reports whether a generation-specific notice has been acknowledged.
func (db *DB) StoryScopeNoticeDelivered(ctx context.Context, parent string, generation int, key, kind string) (bool, error) {
	var delivered bool
	err := db.sql.QueryRowContext(ctx, `SELECT delivered FROM story_scope_notices WHERE parent_task=? AND generation=? AND story_key=? AND kind=?`, parent, generation, key, kind).Scan(&delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return delivered, err
}

// DeliverStoryScopeNotice records successful notice delivery so retries do not repeat it.
func (db *DB) DeliverStoryScopeNotice(ctx context.Context, parent string, generation int, key, kind string) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_scope_notices SET delivered=1 WHERE parent_task=? AND generation=? AND story_key=? AND kind=?`, parent, generation, key, kind)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

// ActivateManagedScope permits execution only after every freeze notice has been delivered.
func (db *DB) ActivateManagedScope(ctx context.Context, parent string, generation int) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE managed_features SET state='implementing' WHERE parent_task=? AND generation=? AND state='freezing' AND NOT EXISTS(SELECT 1 FROM story_scope_notices WHERE parent_task=? AND generation=? AND kind='freeze' AND delivered=0)`, parent, generation, parent, generation)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: freeze notices remain pending")
	}
	return nil
}

// ReplanManagedFeature can only run under the parent turn lock before allocating
// its new run. Completed identities stay completed; unfinished work gets a new
// editable generation while all historical approvals remain immutable.
func (db *DB) ReplanManagedFeature(ctx context.Context, parent string, generation int) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE managed_features SET generation=generation+1,state='planning',ready=0 WHERE parent_task=? AND generation=? AND state!='planning' AND NOT EXISTS(SELECT 1 FROM story_progress_runs p WHERE p.parent_task=managed_features.parent_task AND p.active=1) AND NOT EXISTS(SELECT 1 FROM story_progress_effects e WHERE e.parent_task=managed_features.parent_task AND e.delivered=0 AND e.superseded=0) AND NOT EXISTS(SELECT 1 FROM sessions s WHERE s.task_ref=? AND (s.state='running' OR EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status='running'))) AND NOT EXISTS(SELECT 1 FROM story_refinements r JOIN sessions s ON s.id=r.session_id WHERE s.task_ref=? AND r.consumed=0)`, parent, generation, parent, parent)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("store: parent is not quiescent for Plan")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO managed_stories(parent_task,generation,story_key,issue_number,issue_id,body_hash,dependencies,state) SELECT parent_task,generation+1,story_key,issue_number,issue_id,body_hash,dependencies,CASE WHEN state='completed' THEN 'completed' ELSE 'planning' END FROM managed_stories WHERE parent_task=? AND generation=?`, parent, generation)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO story_scope_notices(parent_task,generation,story_key,kind) SELECT parent_task,generation,story_key,'planning' FROM managed_stories WHERE parent_task=? AND generation=? AND state='planning'`, parent, generation+1)
		return err
	})
}
