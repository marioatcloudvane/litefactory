package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Watermark is how far through a task's comments litefactory has got. Every
// comment with an id at or below LastCommentID has been dealt with; everything
// above it is still to process, in ascending id order (AC-2.11).
type Watermark struct {
	// SessionID is the task, and the primary key.
	SessionID int64
	// LastCommentID is the highest forge comment id consumed.
	LastCommentID int64
	// UpdatedAt is when the watermark last moved.
	UpdatedAt time.Time
}

// GetWatermark reads a session's watermark. exists is false when the task has
// never been bootstrapped, which is the signal that this is a first sight and
// [DB.BootstrapWatermark] is what should happen next, not processing.
func (db *DB) GetWatermark(ctx context.Context, sessionID int64) (Watermark, bool, error) {
	wm, err := scanWatermark(db.sql.QueryRowContext(ctx,
		`SELECT session_id, last_comment_id, updated_at FROM watermarks WHERE session_id = ?`,
		sessionID))
	switch {
	case errors.Is(err, ErrNotFound):
		return Watermark{SessionID: sessionID}, false, nil
	case err != nil:
		return Watermark{}, false, fmt.Errorf("store: read watermark for session %d: %w", sessionID, err)
	}
	return wm, true, nil
}

// BootstrapWatermark performs the first-sight bootstrap (AC-2.10): a task seen
// for the first time has its watermark planted at the highest comment id that
// already exists, so its whole history is skipped rather than processed. Years
// of back-and-forth on an old issue must not become a stampede of runs the
// moment the repo is registered.
//
// It writes only when there is no watermark yet, and reports whether it did.
// An already-bootstrapped session is returned unchanged with created false —
// so calling this on every sighting is safe, and can never rewind a watermark
// that has since advanced.
//
// highestCommentID is 0 for a task with no comments at all, which correctly
// means "nothing consumed yet, process everything from here".
func (db *DB) BootstrapWatermark(ctx context.Context, sessionID, highestCommentID int64) (Watermark, bool, error) {
	if highestCommentID < 0 {
		return Watermark{}, false, errors.New("store: bootstrap watermark: comment id cannot be negative")
	}

	var (
		wm      Watermark
		created bool
	)
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanWatermark(tx.QueryRowContext(ctx,
			`SELECT session_id, last_comment_id, updated_at FROM watermarks WHERE session_id = ?`,
			sessionID))
		switch {
		case err == nil:
			wm = existing
			return nil
		case !errors.Is(err, ErrNotFound):
			return err
		}

		wm, err = scanWatermark(tx.QueryRowContext(ctx, `
INSERT INTO watermarks (session_id, last_comment_id, updated_at)
VALUES (?, ?, ?)
RETURNING session_id, last_comment_id, updated_at`,
			sessionID, highestCommentID, FormatTime(nowUTC())))
		if err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Watermark{}, false, fmt.Errorf("store: bootstrap watermark for session %d: %w", sessionID, err)
	}
	return wm, created, nil
}

// AdvanceWatermark consumes every comment up to and including commentID.
//
// It never moves backwards: a stale or out-of-order advance leaves the stored
// value alone. That matters because a watermark that rewinds means reprocessed
// comments, and reprocessed comments mean duplicate runs.
//
// A run's watermark advance is not made here — it belongs in the same
// transaction as the posted report; see [DB.CompleteRunReport] and AC-4.10.
// This call is for the run-less paths: an unsupported command, an empty
// /spike, plain free text (AC-2.18, AC-2.19, AC-2.20).
func (db *DB) AdvanceWatermark(ctx context.Context, sessionID, commentID int64) (Watermark, error) {
	wm, err := advanceWatermark(ctx, db.sql, sessionID, commentID)
	if err != nil {
		return Watermark{}, fmt.Errorf("store: advance watermark for session %d: %w", sessionID, err)
	}
	return wm, nil
}

// execQuerier is the write-and-read surface shared by *sql.DB and *sql.Tx.
type execQuerier interface {
	querier
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// advanceWatermark is the shared implementation, so the transactional report
// path and the standalone path cannot drift apart.
func advanceWatermark(ctx context.Context, q execQuerier, sessionID, commentID int64) (Watermark, error) {
	const stmt = `
INSERT INTO watermarks (session_id, last_comment_id, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (session_id) DO UPDATE SET
    last_comment_id = MAX(watermarks.last_comment_id, excluded.last_comment_id),
    updated_at      = excluded.updated_at
RETURNING session_id, last_comment_id, updated_at`

	return scanWatermark(q.QueryRowContext(ctx, stmt, sessionID, commentID, FormatTime(nowUTC())))
}

func scanWatermark(s scanner) (Watermark, error) {
	var (
		wm        Watermark
		updatedAt sql.NullString
	)
	if err := s.Scan(&wm.SessionID, &wm.LastCommentID, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Watermark{}, ErrNotFound
		}
		return Watermark{}, err
	}
	var err error
	if wm.UpdatedAt, err = scanTime(updatedAt); err != nil {
		return Watermark{}, err
	}
	return wm, nil
}
