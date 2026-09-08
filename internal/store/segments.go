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

// Segment is one continuous coding-agent conversation within a task. M0
// always has exactly one per session; the table exists so the conversation's
// identity is recorded now and LF-8/LF-17 need no migration later.
type Segment struct {
	// DeliveryIdentity and DeliveryRoot pin the effective instruction bundle.
	DeliveryIdentity string
	DeliveryRoot     string
	// ID is the surrogate key.
	ID int64
	// SessionID is the task this segment belongs to.
	SessionID int64
	// Ordinal is the segment's position within the session, 1 at M0.
	Ordinal int
	// SessionRef is litefactory's own pre-computed identity for this
	// conversation: for a Claude segment, the value passed to
	// `claude --session-id`. It is derived deterministically from the task
	// ref and the segment ordinal by internal/naming, and recorded here so a
	// run can be correlated with the tool's own transcript (AC-3.14).
	//
	// It is always computable before any process runs, and stays NOT NULL for
	// every agent: a tool that only discovers its own conversation handle
	// after a run started still gets a session_ref up front, from
	// internal/naming, the same way every other agent does. That handle,
	// once known, belongs on [ResumeToken] instead.
	SessionRef string
	// Agent is which coding-agent tool owns this segment — "claude" for every
	// segment created before this column existed, via the column's DEFAULT.
	Agent string
	// ResumeToken is the tool's own reported conversation handle, distinct
	// from SessionRef because it is sometimes absent: it is written only
	// after a real run reports one, never derived and never guessed ahead of
	// time. Empty means the tool has not reported one yet.
	ResumeToken string
	// CreatedAt is bookkeeping.
	CreatedAt time.Time
}

// GetOrCreateSegment returns the session's segment with this ordinal, creating
// it on first use.
//
// The second run of a task reuses the first run's segment rather than starting
// a new conversation, so sessionRef and kind are only used when the row is
// created; an existing row is returned as stored. Since sessionRef is derived
// deterministically from the task ref and the ordinal, the derivation and the
// stored value agree anyway — and if they ever disagree, the stored value is
// the one the tool has already seen and therefore the one that wins.
func (db *DB) GetOrCreateSegment(ctx context.Context, sessionID int64, ordinal int, sessionRef string, kind string) (Segment, error) {
	if sessionID == 0 {
		return Segment{}, errors.New("store: get or create segment: session id is required")
	}
	if ordinal < 1 {
		return Segment{}, fmt.Errorf("store: get or create segment: ordinal must be positive, got %d", ordinal)
	}
	if sessionRef == "" {
		return Segment{}, errors.New("store: get or create segment: session ref is required")
	}
	if kind == "" {
		return Segment{}, errors.New("store: get or create segment: agent kind is required")
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
INSERT INTO segments (session_id, ordinal, session_ref, agent, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING `+segmentColumns,
			sessionID, ordinal, sessionRef, kind, FormatTime(nowUTC())))
		return err
	})
	if err != nil {
		return Segment{}, fmt.Errorf("store: get or create segment %d on session %d: %w", ordinal, sessionID, err)
	}
	return segment, nil
}

// SetSegmentResumeToken records the tool's own reported conversation handle,
// once a real run has reported one.
//
// It never overwrites a non-empty token: the first token a segment's
// conversation ever reports is the one every later run resumes against, and a
// later run that reported a different value would be reporting a different
// conversation, not a correction to record over the first one.
func (db *DB) SetSegmentResumeToken(ctx context.Context, segmentID int64, token string) error {
	if segmentID == 0 {
		return errors.New("store: set segment resume token: segment id is required")
	}
	if token == "" {
		return errors.New("store: set segment resume token: token is required")
	}

	err := db.withTx(ctx, func(tx *sql.Tx) error {
		// Read first so a segment that does not exist reports ErrNotFound
		// rather than being indistinguishable from "already has a token".
		if _, err := scanSegment(tx.QueryRowContext(ctx,
			`SELECT `+segmentColumns+` FROM segments WHERE id = ?`, segmentID)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
UPDATE segments SET resume_token = ?
WHERE id = ? AND (resume_token IS NULL OR resume_token = '')`,
			token, segmentID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: set resume token on segment %d: %w", segmentID, err)
	}
	return nil
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
const segmentColumns = `id, session_id, ordinal, session_ref, agent, resume_token, created_at`

func scanSegment(s scanner) (Segment, error) {
	var (
		segment     Segment
		resumeToken sql.NullString
		createdAt   sql.NullString
	)
	err := s.Scan(&segment.ID, &segment.SessionID, &segment.Ordinal, &segment.SessionRef,
		&segment.Agent, &resumeToken, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Segment{}, ErrNotFound
		}
		return Segment{}, err
	}
	segment.ResumeToken = resumeToken.String
	if segment.CreatedAt, err = scanTime(createdAt); err != nil {
		return Segment{}, err
	}
	return segment, nil
}
