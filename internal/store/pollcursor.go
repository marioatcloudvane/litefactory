package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PollCursor is the daemon's place in time across every registered repo. It is
// a single row: the poll is repo-set-wide, not per repo.
type PollCursor struct {
	// TasksUpdatedSince is the `since` value the next tick asks the forge for.
	// The zero time means the cursor has never been initialised, and the very
	// next tick must plant it at "now" rather than at the beginning of history
	// (AC-2.9) — otherwise registering a busy repo replays years of issues.
	//
	// The forge's `since` is inclusive, so the boundary issue is re-seen every
	// tick (D-A). The per-task watermark absorbs that.
	TasksUpdatedSince time.Time
	// LastTickAt is when the last tick completed; `daemon status` reports it.
	LastTickAt time.Time
}

// pollCursorRow is the only row poll_cursor may ever hold; the CHECK in the
// schema enforces it.
const pollCursorRow = 1

// GetPollCursor reads the cursor. A database that has never ticked yields the
// zero [PollCursor] and no error — "never ticked" is a normal state, not a
// missing row to complain about.
func (db *DB) GetPollCursor(ctx context.Context) (PollCursor, error) {
	cursor, err := scanPollCursor(db.sql.QueryRowContext(ctx,
		`SELECT tasks_updated_since, last_tick_at FROM poll_cursor WHERE id = ?`, pollCursorRow))
	switch {
	case errors.Is(err, ErrNotFound):
		return PollCursor{}, nil
	case err != nil:
		return PollCursor{}, fmt.Errorf("store: read poll cursor: %w", err)
	}
	return cursor, nil
}

// InitPollCursor plants the cursor at now, but only if it has never been set,
// and returns the cursor in force afterwards.
//
// This is the first-ever-tick initialisation of AC-2.9, expressed so that
// calling it at the top of every tick is harmless: an existing cursor is
// returned untouched. Doing it as read-then-maybe-write in the caller would
// leave a window where a restart mid-tick resets the cursor forward and
// silently skips comments.
func (db *DB) InitPollCursor(ctx context.Context, now time.Time) (PollCursor, error) {
	var cursor PollCursor
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanPollCursor(tx.QueryRowContext(ctx,
			`SELECT tasks_updated_since, last_tick_at FROM poll_cursor WHERE id = ?`, pollCursorRow))
		switch {
		case err != nil && !errors.Is(err, ErrNotFound):
			return err
		case err == nil && !existing.TasksUpdatedSince.IsZero():
			cursor = existing
			return nil
		}

		cursor, err = scanPollCursor(tx.QueryRowContext(ctx, `
INSERT INTO poll_cursor (id, tasks_updated_since, last_tick_at)
VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET tasks_updated_since = excluded.tasks_updated_since
RETURNING tasks_updated_since, last_tick_at`,
			pollCursorRow, nullTime(now), nullTime(existing.LastTickAt)))
		return err
	})
	if err != nil {
		return PollCursor{}, fmt.Errorf("store: initialise poll cursor: %w", err)
	}
	return cursor, nil
}

// SetPollCursor persists the cursor after a successful tick, so it survives a
// restart (AC-2.9). A tick that failed must not call this: leaving the cursor
// where it was is what makes the next tick redo the work (AC-2.13).
func (db *DB) SetPollCursor(ctx context.Context, cursor PollCursor) error {
	const stmt = `
INSERT INTO poll_cursor (id, tasks_updated_since, last_tick_at)
VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    tasks_updated_since = excluded.tasks_updated_since,
    last_tick_at        = excluded.last_tick_at`

	_, err := db.sql.ExecContext(ctx, stmt,
		pollCursorRow, nullTime(cursor.TasksUpdatedSince), nullTime(cursor.LastTickAt))
	if err != nil {
		return fmt.Errorf("store: write poll cursor: %w", err)
	}
	return nil
}

func scanPollCursor(s scanner) (PollCursor, error) {
	var (
		cursor            PollCursor
		tasksUpdatedSince sql.NullString
		lastTickAt        sql.NullString
	)
	if err := s.Scan(&tasksUpdatedSince, &lastTickAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PollCursor{}, ErrNotFound
		}
		return PollCursor{}, err
	}
	for _, field := range []struct {
		src sql.NullString
		dst *time.Time
	}{
		{tasksUpdatedSince, &cursor.TasksUpdatedSince},
		{lastTickAt, &cursor.LastTickAt},
	} {
		t, err := scanTime(field.src)
		if err != nil {
			return PollCursor{}, err
		}
		*field.dst = t
	}
	return cursor, nil
}

// GetRepoPollCursor returns how far repoID's forge listing has finished. A repo
// that has no row yet (registered after the migration, never listed) yields the
// zero time and false.
func (db *DB) GetRepoPollCursor(ctx context.Context, repoID int64) (time.Time, bool, error) {
	var raw sql.NullString
	err := db.sql.QueryRowContext(ctx,
		`SELECT listed_up_to FROM repo_poll_cursors WHERE repo_id = ?`, repoID).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, fmt.Errorf("store: read repo poll cursor %d: %w", repoID, err)
	}
	t, err := scanTime(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("store: parse repo poll cursor %d: %w", repoID, err)
	}
	return t, true, nil
}

// SetRepoPollCursor records that repoID's listing finished up to at. It only
// moves forward: an older value leaves the row as it is, so a late or repeated
// write cannot rewind a repo into replaying history.
func (db *DB) SetRepoPollCursor(ctx context.Context, repoID int64, at time.Time) error {
	if at.IsZero() {
		return fmt.Errorf("store: write repo poll cursor %d: zero time", repoID)
	}
	const stmt = `
INSERT INTO repo_poll_cursors (repo_id, listed_up_to) VALUES (?, ?)
ON CONFLICT (repo_id) DO UPDATE SET listed_up_to = excluded.listed_up_to
WHERE excluded.listed_up_to > repo_poll_cursors.listed_up_to`
	if _, err := db.sql.ExecContext(ctx, stmt, repoID, FormatTime(at)); err != nil {
		return fmt.Errorf("store: write repo poll cursor %d: %w", repoID, err)
	}
	return nil
}
