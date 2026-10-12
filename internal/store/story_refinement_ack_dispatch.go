package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"
)

// StoryRefinementACK is retained prepared acknowledgement intent. The existing
// acknowledgement table owns both dispatch and confirmation; its source remains
// unconsumed until the eventual result is separately delivered.
type StoryRefinementACK struct {
	AcknowledgementID int64
	PreparedBody      string
	DispatchAttempted bool
}

func scanRefinementACK(row scanner) (StoryRefinementACK, error) {
	var a StoryRefinementACK
	err := row.Scan(&a.AcknowledgementID, &a.PreparedBody, &a.DispatchAttempted)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// GetStoryRefinementACK reads original prepared bytes and dispatch uncertainty.
func (db *DB) GetStoryRefinementACK(ctx context.Context, sessionID, commentID int64) (StoryRefinementACK, error) {
	a, err := scanRefinementACK(db.sql.QueryRowContext(ctx, `SELECT acknowledgement_id,prepared_body,dispatch_attempted FROM story_refinement_acks WHERE session_id=? AND comment_id=?`, sessionID, commentID))
	if err != nil {
		return a, fmt.Errorf("store: read refinement acknowledgement: %w", err)
	}
	return a, nil
}

// PrepareStoryRefinementACK records redacted bytes once. Replays return the
// original body and never rewrite legacy confirmed acknowledgements.
func (db *DB) PrepareStoryRefinementACK(ctx context.Context, sessionID, commentID int64, body string) (StoryRefinementACK, error) {
	if sessionID < 1 || commentID < 1 || len(body) == 0 || len(body) > 65536 || !utf8.ValidString(body) {
		return StoryRefinementACK{}, fmt.Errorf("store: invalid prepared refinement acknowledgement")
	}
	var a StoryRefinementACK
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO story_refinement_acks(session_id,comment_id,acknowledgement_id,prepared_body) VALUES(?,?,0,?) ON CONFLICT(session_id,comment_id) DO NOTHING`, sessionID, commentID, body); err != nil {
			return err
		}
		var err error
		a, err = scanRefinementACK(tx.QueryRowContext(ctx, `SELECT acknowledgement_id,prepared_body,dispatch_attempted FROM story_refinement_acks WHERE session_id=? AND comment_id=?`, sessionID, commentID))
		return err
	})
	if err != nil {
		return a, fmt.Errorf("store: prepare refinement acknowledgement: %w", err)
	}
	return a, nil
}

// ClaimStoryRefinementACK burns the sole dispatch right before remote I/O. A
// failed or absent observation cannot restore it or authorize a second POST.
func (db *DB) ClaimStoryRefinementACK(ctx context.Context, sessionID, commentID int64, body string) (bool, error) {
	res, err := db.sql.ExecContext(ctx, `UPDATE story_refinement_acks SET dispatch_attempted=1 WHERE session_id=? AND comment_id=? AND acknowledgement_id=0 AND dispatch_attempted=0 AND prepared_body=? AND prepared_body<>''`, sessionID, commentID, body)
	if err != nil {
		return false, fmt.Errorf("store: claim refinement acknowledgement: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
