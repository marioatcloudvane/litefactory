package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ReserveContinuation and allocation use conditional writes in transactions;
// a cancellation which wins before allocation cannot start a subprocess.
func (db *DB) ReserveContinuation(ctx context.Context, id int64, at time.Time) (Continuation, bool, error) {
	var c Continuation
	var ok bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE automatic_continuations SET status='reserved', reserved_at=? WHERE id=? AND status='pending' AND due_at<=?`, FormatTime(at), id, FormatTime(at))
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		ok = n == 1
		if ok {
			c, err = scanContinuation(tx.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE id=?`, id))
		}
		return err
	})
	return c, ok, err
}

// RequeueContinuation returns an unallocated reservation to pending without
// consuming an automatic invocation attempt.
func (db *DB) RequeueContinuation(ctx context.Context, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE automatic_continuations SET status='pending', reserved_at=NULL WHERE id=? AND status='reserved' AND allocated_run_id IS NULL`, id)
	return err
}

// CancelContinuation permanently cancels unallocated work and blocks its
// suspended prerequisite chain atomically. An allocated run is left alone.
func (db *DB) CancelContinuation(ctx context.Context, id int64, reason string, at time.Time) error {
	if reason == "" {
		return errors.New("continuation cancellation requires a reason")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		c, err := scanContinuation(tx.QueryRowContext(ctx, `UPDATE automatic_continuations SET status='cancelled', cancellation_reason=?, finished_at=? WHERE id=? AND status IN ('pending','reserved') AND allocated_run_id IS NULL RETURNING `+continuationColumns, reason, FormatTime(at), id))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return settleContinuationPrerequisite(ctx, tx, c, 0, false, at)
	})
}

func validateAutomaticAllocation(ctx context.Context, tx *sql.Tx, p AllocateRunParams) error {
	c, err := scanContinuation(tx.QueryRowContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE id=?`, p.TriggerCommentID-automaticTriggerBase))
	if err != nil {
		return err
	}
	if c.Status != "reserved" || c.AllocatedRunID != 0 || c.SessionID != p.SessionID || c.SegmentID != p.SegmentID || c.Context.Agent != p.Agent || c.Context.Mode != p.Mode {
		return errors.New("automatic continuation is no longer admitted")
	}
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM automatic_continuations WHERE root_run_id=? AND allocated_run_id IS NOT NULL`, c.RootRunID).Scan(&attempts); err != nil {
		return err
	}
	if attempts >= MaxAutomaticAttempts {
		return errors.New("automatic continuation invocation budget exhausted")
	}
	return nil
}

// ListUnreportedContinuationCancellations discovers durable notice debt without
// depending on the source report's acknowledgement or the owner watermark.
func (db *DB) ListUnreportedContinuationCancellations(ctx context.Context) ([]Continuation, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+continuationColumns+` FROM automatic_continuations WHERE status='cancelled' AND cancellation_comment_id IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notices []Continuation
	for rows.Next() {
		c, err := scanContinuation(rows)
		if err != nil {
			return nil, err
		}
		notices = append(notices, c)
	}
	return notices, rows.Err()
}

// MarkContinuationCancellationReported acknowledges only a confirmed post.
func (db *DB) MarkContinuationCancellationReported(ctx context.Context, id, commentID int64) error {
	if commentID <= 0 {
		return errors.New("cancellation notice needs a posted comment identity")
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE automatic_continuations SET cancellation_comment_id=? WHERE id=? AND status='cancelled' AND cancellation_comment_id IS NULL`, commentID, id)
	return err
}
