package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ModeSelection is the durable routing and label-cleanup state of one trigger.
type ModeSelection struct {
	SessionID, TriggerCommentID  int64
	Mode, Label                  string
	LabelApplied, CleanupPending bool
}

// GetOrCreateModeSelection records a decision once; the stored decision wins
// on retries so a classifier is never asked twice for the same trigger.
func (d *DB) GetOrCreateModeSelection(ctx context.Context, selection ModeSelection) (ModeSelection, error) {
	_, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO mode_selections
        (session_id, trigger_comment_id, mode, label, created_at) VALUES (?, ?, ?, ?, datetime('now'))`,
		selection.SessionID, selection.TriggerCommentID, selection.Mode, selection.Label)
	if err != nil {
		return ModeSelection{}, fmt.Errorf("store: record mode selection: %w", err)
	}
	return d.GetModeSelection(ctx, selection.SessionID, selection.TriggerCommentID)
}

// GetModeSelection reads the durable decision for one trigger.
func (d *DB) GetModeSelection(ctx context.Context, sessionID, triggerID int64) (ModeSelection, error) {
	var s ModeSelection
	err := d.sql.QueryRowContext(ctx, `SELECT session_id, trigger_comment_id, mode, label,
        label_applied, cleanup_pending FROM mode_selections WHERE session_id = ? AND trigger_comment_id = ?`,
		sessionID, triggerID).Scan(&s.SessionID, &s.TriggerCommentID, &s.Mode, &s.Label, &s.LabelApplied, &s.CleanupPending)
	if err != nil {
		if err == sql.ErrNoRows {
			return ModeSelection{}, ErrNotFound
		}
		return ModeSelection{}, fmt.Errorf("store: read mode selection: %w", err)
	}
	return s, nil
}

// MarkModeLabelApplied records that GitHub accepted the canonical label.
func (d *DB) MarkModeLabelApplied(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE mode_selections SET label_applied = 1 WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: mark mode label applied: %w", err)
	}
	return requireAffected(res, "store: mark mode label applied")
}

// MarkModeCleanupPending makes label removal a restart-safe obligation.
func (d *DB) MarkModeCleanupPending(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE mode_selections SET cleanup_pending = 1 WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: mark mode cleanup pending: %w", err)
	}
	return requireAffected(res, "store: mark mode cleanup pending")
}

// DeleteModeSelection closes a selection after its label is removed.
func (d *DB) DeleteModeSelection(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM mode_selections WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: delete mode selection: %w", err)
	}
	return requireAffected(res, "store: delete mode selection")
}

// PendingModeCleanup returns a selection explicitly pending cleanup or whose
// trigger watermark proves its report committed before a crash.
func (d *DB) PendingModeCleanup(ctx context.Context, sessionID int64) (ModeSelection, bool, error) {
	var s ModeSelection
	err := d.sql.QueryRowContext(ctx, `SELECT m.session_id, m.trigger_comment_id, m.mode, m.label,
        m.label_applied, m.cleanup_pending FROM mode_selections m
        LEFT JOIN watermarks w ON w.session_id = m.session_id
        WHERE m.session_id = ? AND (m.cleanup_pending = 1 OR w.last_comment_id >= m.trigger_comment_id)
        ORDER BY m.trigger_comment_id LIMIT 1`, sessionID).
		Scan(&s.SessionID, &s.TriggerCommentID, &s.Mode, &s.Label, &s.LabelApplied, &s.CleanupPending)
	if err == sql.ErrNoRows {
		return ModeSelection{}, false, nil
	}
	if err != nil {
		return ModeSelection{}, false, fmt.Errorf("store: pending mode cleanup: %w", err)
	}
	return s, true, nil
}
