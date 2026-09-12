package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// StoryRefinement is an admitted owner discussion. Admission is durable but does
// not consume a source comment; only successful result delivery does that.
type StoryRefinement struct {
	SessionID   int64
	CommentID   int64
	ChildNumber int
	ChildID     int64
	Generation  int
	ActorID     int64
	ActorHandle string
	Body        string
	Consumed    bool
}

const refinementColumns = `session_id,comment_id,child_number,child_id,generation,actor_id,actor_handle,body,consumed`

func scanRefinement(row scanner) (StoryRefinement, error) {
	var r StoryRefinement
	err := row.Scan(&r.SessionID, &r.CommentID, &r.ChildNumber, &r.ChildID, &r.Generation, &r.ActorID, &r.ActorHandle, &r.Body, &r.Consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// AdmitStoryRefinement durably queues a discussion and invalidates readiness only on its first admission.
func (db *DB) AdmitStoryRefinement(ctx context.Context, r StoryRefinement) error {
	if r.SessionID <= 0 || r.CommentID <= 0 || r.ChildNumber <= 0 || r.ChildID <= 0 || r.Generation <= 0 || r.ActorID <= 0 {
		return fmt.Errorf("store: invalid story refinement")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO story_refinements (`+refinementColumns+`) VALUES(?,?,?,?,?,?,?,?,0) ON CONFLICT(session_id,comment_id) DO NOTHING`, r.SessionID, r.CommentID, r.ChildNumber, r.ChildID, r.Generation, r.ActorID, r.ActorHandle, r.Body)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		// Newly admitted discussion invalidates readiness atomically. Re-reading an
		// already delivered source must not invalidate a later successful plan.
		_, err = tx.ExecContext(ctx, `UPDATE managed_features SET ready=0 WHERE parent_task=(SELECT task_ref FROM sessions WHERE id=?) AND generation=? AND state='planning' AND NOT EXISTS(SELECT 1 FROM managed_stories s WHERE s.parent_task=managed_features.parent_task AND s.generation=managed_features.generation AND s.issue_id=? AND s.state='completed')`, r.SessionID, r.Generation, r.ChildID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: admit story refinement: %w", err)
	}
	return nil
}

// GetStoryRefinement resolves an admitted source independently of the parent comment watermark.
func (db *DB) GetStoryRefinement(ctx context.Context, sessionID, commentID int64) (StoryRefinement, error) {
	return scanRefinement(db.sql.QueryRowContext(ctx, `SELECT `+refinementColumns+` FROM story_refinements WHERE session_id=? AND comment_id=?`, sessionID, commentID))
}

// ListPendingStoryRefinements returns unconsumed discussions in source-comment order for replay.
func (db *DB) ListPendingStoryRefinements(ctx context.Context, sessionID int64) ([]StoryRefinement, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+refinementColumns+` FROM story_refinements WHERE session_id=? AND consumed=0 ORDER BY comment_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list refinements: %w", err)
	}
	defer rows.Close()
	var out []StoryRefinement
	for rows.Next() {
		r, err := scanRefinement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ConsumeStoryRefinement records delivery without touching the parent watermark.
func (db *DB) ConsumeStoryRefinement(ctx context.Context, sessionID, commentID int64) error {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_refinements SET consumed=1 WHERE session_id=? AND comment_id=?`, sessionID, commentID)
	if err != nil {
		return fmt.Errorf("store: consume refinement: %w", err)
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

// consumeRefinement is the transaction-local branch used by run reporting.
func consumeRefinement(ctx context.Context, tx *sql.Tx, sessionID, commentID int64) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE story_refinements SET consumed=1 WHERE session_id=? AND comment_id=?`, sessionID, commentID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n != 0, err
}

// ListManagedFeatures provides independent discovery, including unchanged parents.
//
// A parent observed closed is left out. This listing is the one discovery axis
// nothing gates on a forge listing — that is the point of it, since GitHub does
// not move a parent's timestamp when a child moves — so a registry row with no
// terminal state is a standing per-tick cost for the life of the row. See
// [DB.SetManagedParentClosed] for why the flag is an observation of the issue
// rather than a fifth value of state.
func (db *DB) ListManagedFeatures(ctx context.Context) ([]ManagedFeature, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT parent_task,generation,state,ready FROM managed_features
WHERE parent_task NOT IN (SELECT parent_task FROM managed_parent_closures)
ORDER BY parent_task`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManagedFeature
	for rows.Next() {
		var f ManagedFeature
		if err := rows.Scan(&f.ParentTask, &f.Generation, &f.State, &f.Ready); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetManagedParentClosed records an authoritative observation of the parent
// issue's open/closed state, retiring or restoring the unconditional discovery
// axis [DB.ListManagedFeatures] provides.
//
// A task that is not a registered parent matches nothing and writes nothing, by
// construction rather than by a preceding lookup: the insert selects its own
// row out of managed_features. That is what lets both observations live where
// the daemon already holds a fresh issue — [Daemon.materialise] for the closure
// and [Daemon.sight] for the reopening — instead of behind a read that would
// cost more than the flag saves. Neither caller knows or needs to know whether
// the task it just read is a managed parent.
//
// Matching no row is success, not [ErrNotFound], in both directions.
func (db *DB) SetManagedParentClosed(ctx context.Context, parentTask string, closed bool) error {
	var err error
	if closed {
		_, err = db.sql.ExecContext(ctx, `
INSERT INTO managed_parent_closures (parent_task)
SELECT parent_task FROM managed_features WHERE parent_task=?
ON CONFLICT(parent_task) DO NOTHING`, parentTask)
	} else {
		_, err = db.sql.ExecContext(ctx,
			`DELETE FROM managed_parent_closures WHERE parent_task=?`, parentTask)
	}
	if err != nil {
		return fmt.Errorf("store: record parent closure=%t for managed feature %s: %w", closed, parentTask, err)
	}
	return nil
}

// ManagedParentForChild resolves only durable registered identities. A marker
// in arbitrary issue text is never a relationship or instruction capability.
func (db *DB) ManagedParentForChild(ctx context.Context, repoID int64, number int) (string, error) {
	var parent string
	err := db.sql.QueryRowContext(ctx, `SELECT s.parent_task FROM managed_stories s JOIN sessions p ON p.task_ref=s.parent_task WHERE p.repo_id=? AND s.issue_number=? ORDER BY s.generation DESC LIMIT 1`, repoID, number).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return parent, err
}

// StoryDiscoveryCursor returns the last discovered child comment, or zero before discovery starts.
func (db *DB) StoryDiscoveryCursor(ctx context.Context, sessionID int64, child int) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT comment_id FROM story_discovery WHERE session_id=? AND child_number=?`, sessionID, child).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// AdvanceStoryDiscovery persists child discovery progress without allowing a cursor rewind.
func (db *DB) AdvanceStoryDiscovery(ctx context.Context, sessionID int64, child int, id int64) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO story_discovery(session_id,child_number,comment_id) VALUES(?,?,?) ON CONFLICT(session_id,child_number) DO UPDATE SET comment_id=MAX(comment_id,excluded.comment_id)`, sessionID, child, id)
	return err
}

// MarkStoryDiscoveryPending records that a child owes a comment sweep.
//
// The caller is the tick's repository comment listing, which names every issue
// commented on since the cursor without costing a request per child. Repeated
// marks are the same fact. A child already swept this tick is marked again
// rather than skipped: the sweep clears the row, so a comment that lands
// between the sweep and the clear is re-found instead of lost.
func (db *DB) MarkStoryDiscoveryPending(ctx context.Context, sessionID int64, child int) error {
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO story_discovery_pending(session_id,child_number) VALUES(?,?) ON CONFLICT DO NOTHING`,
		sessionID, child)
	if err != nil {
		return fmt.Errorf("store: mark child %d of session %d for discovery: %w", child, sessionID, err)
	}
	return nil
}

// ClearStoryDiscoveryPending retires a child's sweep debt.
//
// Only a completed sweep may call this, and only after the comments it read
// have been admitted and the cursor advanced — the same never-rewind asymmetry
// [DB.ClearSessionNeedsPoll] documents. Stale-pending costs one sweep;
// stale-clear loses an owner's comment.
func (db *DB) ClearStoryDiscoveryPending(ctx context.Context, sessionID int64, child int) error {
	_, err := db.sql.ExecContext(ctx,
		`DELETE FROM story_discovery_pending WHERE session_id=? AND child_number=?`, sessionID, child)
	if err != nil {
		return fmt.Errorf("store: clear discovery debt for child %d of session %d: %w", child, sessionID, err)
	}
	return nil
}

// ListStoriesAwaitingDiscovery returns the children of one generation that owe a
// comment sweep, ascending.
//
// A child named by the repository comment listing has something new, and that
// listing runs once per tick for the whole repository — so this is the signal
// that costs nothing per child, and everything it does not name has a cursor
// and nothing to read. That alone is what stops the sweep scaling with the size
// of the registry.
//
// A child with no story_discovery row is owed one too, whatever the listing
// said. Its history has never been read at all, so no listing can vouch for it
// and no feature state excuses it — this is the registry's own axis, and it has
// to hold when the listings answer nothing. It is a one-time cost per child,
// not a per-tick one: the sweep leaves a cursor behind, and after that the
// listing is the only thing that can owe it another.
func (db *DB) ListStoriesAwaitingDiscovery(ctx context.Context, sessionID int64, parent string, generation int) ([]int, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT s.issue_number FROM managed_stories s
LEFT JOIN story_discovery d ON d.session_id=? AND d.child_number=s.issue_number
WHERE s.parent_task=? AND s.generation=?
  AND (d.child_number IS NULL
       OR EXISTS(SELECT 1 FROM story_discovery_pending p
                 WHERE p.session_id=? AND p.child_number=s.issue_number))
ORDER BY s.issue_number`, sessionID, parent, generation, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list the children of %s awaiting discovery: %w", parent, err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var child int
		if err := rows.Scan(&child); err != nil {
			return nil, err
		}
		out = append(out, child)
	}
	return out, rows.Err()
}

// StoryDiscoveryRotation returns the last scanned child so bounded polling can rotate fairly.
func (db *DB) StoryDiscoveryRotation(ctx context.Context, sessionID int64) (int, error) {
	var child int
	err := db.sql.QueryRowContext(ctx, `SELECT child_number FROM story_discovery_rotation WHERE session_id=?`, sessionID).Scan(&child)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return child, err
}

// SetStoryDiscoveryRotation persists the last scanned child across polling ticks.
func (db *DB) SetStoryDiscoveryRotation(ctx context.Context, sessionID int64, child int) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO story_discovery_rotation(session_id,child_number) VALUES(?,?) ON CONFLICT(session_id) DO UPDATE SET child_number=excluded.child_number`, sessionID, child)
	return err
}

// StoryRefinementAcknowledgement returns a posted acknowledgement identity, or zero if delivery is pending.
func (db *DB) StoryRefinementAcknowledgement(ctx context.Context, sessionID, commentID int64) (int64, error) {
	var id int64
	err := db.sql.QueryRowContext(ctx, `SELECT acknowledgement_id FROM story_refinement_acks WHERE session_id=? AND comment_id=?`, sessionID, commentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// AcknowledgeStoryRefinement records the first acknowledgement without consuming its source discussion.
func (db *DB) AcknowledgeStoryRefinement(ctx context.Context, sessionID, commentID, ackID int64) error {
	if ackID <= 0 {
		return fmt.Errorf("store: acknowledgement id required")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO story_refinement_acks(session_id,comment_id,acknowledgement_id) VALUES(?,?,?) ON CONFLICT(session_id,comment_id) DO NOTHING`, sessionID, commentID, ackID)
	return err
}
