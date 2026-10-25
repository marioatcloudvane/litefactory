package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AttemptTiming is the immutable deadline of one logical worker attempt.
// Exactly one of NodeAttemptID and RunID identifies it.
type AttemptTiming struct {
	NodeAttemptID, RunID int64
	StartedAt, Deadline  time.Time
	Allowance            time.Duration
	Source               string
}

// EnsureAttemptTiming retains the first snapshot, including across restart.
func (db *DB) EnsureAttemptTiming(ctx context.Context, in AttemptTiming) (AttemptTiming, error) {
	if (in.NodeAttemptID > 0) == (in.RunID > 0) || in.NodeAttemptID < 0 || in.RunID < 0 || in.StartedAt.IsZero() || in.Deadline.IsZero() || in.Allowance <= 0 || in.Source == "" {
		return AttemptTiming{}, errors.New("store: invalid worker attempt timing")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT OR IGNORE INTO worker_attempt_timing(node_attempt_id,run_id,started_at,deadline,allowance_ns,source) VALUES(?,?,?,?,?,?)`, in.NodeAttemptID, in.RunID, FormatTime(in.StartedAt), FormatTime(in.Deadline), int64(in.Allowance), in.Source)
	if err != nil {
		return AttemptTiming{}, err
	}
	return db.GetAttemptTiming(ctx, in.NodeAttemptID, in.RunID)
}

// GetAttemptTiming reads recorded policy without inventing historical timing.
func (db *DB) GetAttemptTiming(ctx context.Context, attemptID, runID int64) (AttemptTiming, error) {
	out := AttemptTiming{NodeAttemptID: attemptID, RunID: runID}
	var start, deadline string
	var ns int64
	err := db.sql.QueryRowContext(ctx, `SELECT started_at,deadline,allowance_ns,source FROM worker_attempt_timing WHERE node_attempt_id=? AND run_id=?`, attemptID, runID).Scan(&start, &deadline, &ns, &out.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if out.StartedAt, err = ParseTime(start); err != nil {
		return out, err
	}
	out.Deadline, err = ParseTime(deadline)
	out.Allowance = time.Duration(ns)
	return out, err
}
