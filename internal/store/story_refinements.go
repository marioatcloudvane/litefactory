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
func (db *DB) ListManagedFeatures(ctx context.Context) ([]ManagedFeature, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT parent_task,generation,state,ready FROM managed_features ORDER BY parent_task`)
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
