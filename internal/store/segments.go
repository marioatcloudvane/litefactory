package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FirstSegmentOrdinal is the ordinal of a session's first segment. M0 creates
// exactly one segment per session and never advances past this (LF-8 is where
// segments start to multiply).
const FirstSegmentOrdinal = 1

// Segment is one continuous claude conversation within a task. M0 always has
// exactly one per session; the table exists so the claude session id is
// recorded now and LF-8/LF-17 need no migration later.
type Segment struct {
	// ID is the surrogate key.
	ID int64
	// SessionID is the task this segment belongs to.
	SessionID int64
	// Ordinal is the segment's position within the session, 1 at M0.
	Ordinal int
	// ClaudeSessionID is the value passed to `claude --session-id`. It is
	// derived deterministically from the task ref and the segment ordinal by
	// internal/naming, and recorded here so a run can be correlated with
	// claude's own transcript (AC-3.14).
	ClaudeSessionID string
	// CreatedAt is bookkeeping.
	CreatedAt time.Time
}

// GetOrCreateSegment returns the session's segment with this ordinal, creating
// it on first use.
//
// The second run of a task reuses the first run's segment rather than starting
// a new conversation, so claudeSessionID is only used when the row is created;
// an existing row is returned as stored. Since the id is derived
// deterministically from the task ref and the ordinal, those two agree anyway
// — and if they ever disagree, the stored value is the one claude has already
// seen and therefore the one that wins.
func (db *DB) GetOrCreateSegment(ctx context.Context, sessionID int64, ordinal int, claudeSessionID string) (Segment, error) {
	if sessionID == 0 {
		return Segment{}, errors.New("store: get or create segment: session id is required")
	}
	if ordinal < 1 {
		return Segment{}, fmt.Errorf("store: get or create segment: ordinal must be positive, got %d", ordinal)
	}
	if claudeSessionID == "" {
		return Segment{}, errors.New("store: get or create segment: claude session id is required")
	}

	var segment Segment
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanSegment(tx.QueryRowContext(ctx,
			`SELECT `+segmentColumns+` FROM segments WHERE session_id = ? AND ordinal = ?`,
			sessionID, ordinal))
		switch {
		case err == nil:
			segment = existing
			return nil
		case !errors.Is(err, ErrNotFound):
			return err
		}

		segment, err = scanSegment(tx.QueryRowContext(ctx, `
INSERT INTO segments (session_id, ordinal, claude_session_id, created_at)
VALUES (?, ?, ?, ?)
RETURNING `+segmentColumns,
			sessionID, ordinal, claudeSessionID, FormatTime(nowUTC())))
		return err
	})
	if err != nil {
		return Segment{}, fmt.Errorf("store: get or create segment %d on session %d: %w", ordinal, sessionID, err)
	}
	return segment, nil
}

// GetSegment reads a segment by id, returning [ErrNotFound] when there is none.
func (db *DB) GetSegment(ctx context.Context, id int64) (Segment, error) {
	segment, err := scanSegment(db.sql.QueryRowContext(ctx,
		`SELECT `+segmentColumns+` FROM segments WHERE id = ?`, id))
	if err != nil {
		return Segment{}, fmt.Errorf("store: read segment %d: %w", id, err)
	}
	return segment, nil
}

// ListSegments returns a session's segments in ordinal order.
func (db *DB) ListSegments(ctx context.Context, sessionID int64) ([]Segment, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+segmentColumns+` FROM segments WHERE session_id = ? ORDER BY ordinal`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list segments for session %d: %w", sessionID, err)
	}
	defer rows.Close()

	var out []Segment
	for rows.Next() {
		segment, err := scanSegment(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list segments for session %d: %w", sessionID, err)
		}
		out = append(out, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list segments for session %d: %w", sessionID, err)
	}
	return out, nil
}

// segmentColumns is the column list every segment read shares, in the order
// scanSegment expects.
const segmentColumns = `id, session_id, ordinal, claude_session_id, created_at`

func scanSegment(s scanner) (Segment, error) {
	var (
		segment   Segment
		createdAt sql.NullString
	)
	err := s.Scan(&segment.ID, &segment.SessionID, &segment.Ordinal, &segment.ClaudeSessionID, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Segment{}, ErrNotFound
		}
		return Segment{}, err
	}
	if segment.CreatedAt, err = scanTime(createdAt); err != nil {
		return Segment{}, err
	}
	return segment, nil
}
