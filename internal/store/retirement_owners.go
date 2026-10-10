package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// retirementFamilySQL, retirementActivitySQL and retirementPendingSQL are the
// owner-observation statements shared with the archive settled recheck, which
// must run them inside its own transaction on the single connection.
const (
	retirementFamilySQL   = `WITH family AS (SELECT id, task_ref, lock_owner, state FROM sessions WHERE id=? OR (repo_id=? AND issue_number IN (SELECT issue_number FROM managed_stories WHERE parent_task=?))) `
	retirementActivitySQL = `SELECT COUNT(*),MAX(finished_at),EXISTS(SELECT 1 FROM runs WHERE session_id IN (SELECT id FROM family) AND (finished_at IS NULL OR status IN ('running','prelaunch'))) OR EXISTS(SELECT 1 FROM family WHERE (COALESCE(lock_owner,'')<>'' AND id<>?) OR state='running') FROM runs WHERE session_id IN (SELECT id FROM family)`
	retirementPendingSQL  = `SELECT
 EXISTS(SELECT 1 FROM automatic_continuations WHERE session_id IN (SELECT id FROM family) AND status IN ('pending','reserved','running'))
 OR EXISTS(SELECT 1 FROM conflict_proposals WHERE session_id IN (SELECT id FROM family) AND status='pending')
 OR EXISTS(SELECT 1 FROM report_deliveries WHERE session_id IN (SELECT id FROM family) AND completed=0)
 OR EXISTS(SELECT 1 FROM delivery_proposals WHERE parent_task IN (SELECT task_ref FROM family) AND superseded=0 AND closed=0)
 OR EXISTS(SELECT 1 FROM story_publications WHERE parent_task IN (SELECT task_ref FROM family) AND delivered=0)
 OR EXISTS(SELECT 1 FROM story_revision_operations WHERE parent_task IN (SELECT task_ref FROM family) AND delivered=0)`
	// retirementSharedStorySQL selects the managed story relation of the outer
	// sessions row when that story shares its parent's checkout: no branch of
	// its own, the parent's branch, or the parent's worktree. A story with its
	// own branch and worktree is its own retirement owner.
	retirementSharedStorySQL = `SELECT 1 FROM managed_stories m JOIN sessions p ON p.task_ref=m.parent_task WHERE p.repo_id=sessions.repo_id AND m.issue_number=sessions.issue_number AND (COALESCE(sessions.branch,'')='' OR sessions.branch=p.branch OR (COALESCE(sessions.worktree_path,'')<>'' AND sessions.worktree_path=p.worktree_path))`
)

// RetirementOwner is a registered canonical resource owner, independent of forge
// polling. Child stories that share the parent's checkout are never owners here;
// a story with its own branch and worktree owns them itself.
type RetirementOwner struct {
	ObservationError error
	Session          Session
	Repo             Repo
	LastFinishedAt   time.Time
	RunCount         int
	Active           bool
	// BusyWithoutOwnerLock lets the executor discount only its own acquired lock.
	BusyWithoutOwnerLock bool
	Pending              bool
	Canonical            bool
}

// RetirementBoundary fixes the inclusive end of a pass so new tasks cannot
// indefinitely extend it. No scheduling state is changed by this observation.
func (db *DB) RetirementBoundary(ctx context.Context) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM sessions`).Scan(&id)
	return id, err
}

// ListRetirementOwners returns a bounded stable page; closed and remote-only
// tasks remain discoverable because ownership comes from durable sessions.
func (db *DB) ListRetirementOwners(ctx context.Context, afterID, throughID int64, limit int) ([]RetirementOwner, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("retirement page size must be between 1 and 100")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT sessions.id FROM sessions WHERE sessions.id>? AND sessions.id<=? AND NOT EXISTS (`+retirementSharedStorySQL+`) ORDER BY sessions.id LIMIT ?`, afterID, throughID, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read retirement owner: %w", errors.Join(err, rows.Close()))
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	owners := make([]RetirementOwner, 0, len(ids))
	for _, id := range ids {
		owner, err := db.GetRetirementOwner(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			owner.Session.ID = id
			owner.ObservationError = err
		}
		owners = append(owners, owner)
	}
	return owners, nil
}

// GetRetirementOwner rereads activity and checkout-dependent debt. The caller
// supplies coordination when using this observation to authorize mutation.
func (db *DB) GetRetirementOwner(ctx context.Context, id int64) (RetirementOwner, error) {
	var owner RetirementOwner
	var err error
	owner.Session, err = db.GetSession(ctx, id)
	if err != nil {
		return owner, err
	}
	err = db.sql.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM sessions WHERE sessions.id=? AND EXISTS(`+retirementSharedStorySQL+`)) AND NOT EXISTS(SELECT 1 FROM sessions s WHERE s.id<>? AND s.repo_id=? AND NOT EXISTS(SELECT 1 FROM managed_stories child WHERE child.parent_task=? AND child.issue_number=s.issue_number) AND ((s.branch IS NOT NULL AND s.branch<>'' AND s.branch=?) OR (s.worktree_path IS NOT NULL AND s.worktree_path<>'' AND s.worktree_path=?)))`, id, id, owner.Session.RepoID, owner.Session.TaskRef, owner.Session.Branch, owner.Session.WorktreePath).Scan(&owner.Canonical)
	if err != nil {
		return owner, err
	}
	owner.Repo, err = db.GetRepo(ctx, owner.Session.RepoID)
	if err != nil {
		return owner, err
	}
	// Include any historical child session that shares this owner's checkout.
	// Normal managed execution uses the parent, but old child rows cannot hide
	// active work or a more recent finish time.
	family := retirementFamilySQL
	args := []any{id, owner.Session.RepoID, owner.Session.TaskRef}
	var finished sql.NullString
	err = db.sql.QueryRowContext(ctx, family+retirementActivitySQL, append(args, id)...).Scan(&owner.RunCount, &finished, &owner.BusyWithoutOwnerLock)
	if err != nil {
		return owner, err
	}
	owner.Active = owner.BusyWithoutOwnerLock || owner.Session.LockOwner != ""
	if finished.Valid {
		owner.LastFinishedAt, err = ParseTime(finished.String)
		if err != nil {
			return owner, err
		}
	}
	err = db.sql.QueryRowContext(ctx, family+retirementPendingSQL, args...).Scan(&owner.Pending)
	return owner, err
}
