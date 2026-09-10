package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ModeSelection is the durable routing and recovery state of one trigger.
// CleanupPending retains its legacy storage name but now requests journal
// retirement only; issue labels outlive the trigger.
type ModeSelection struct {
	SessionID, TriggerCommentID  int64
	Mode, Label                  string
	PreviousLabel                string
	PreviousLabelRemoved         bool
	LabelApplied, CleanupPending bool
}

// GetOrCreateModeSelection records a decision once; the stored decision wins
// on retries so a classifier is never asked twice for the same trigger.
func (d *DB) GetOrCreateModeSelection(ctx context.Context, selection ModeSelection) (ModeSelection, error) {
	_, err := d.sql.ExecContext(ctx, `INSERT OR IGNORE INTO mode_selections
        (session_id, trigger_comment_id, mode, label, label_applied, previous_label, previous_label_removed, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		selection.SessionID, selection.TriggerCommentID, selection.Mode, selection.Label, selection.LabelApplied, selection.PreviousLabel, selection.PreviousLabelRemoved)
	if err != nil {
		return ModeSelection{}, fmt.Errorf("store: record mode selection: %w", err)
	}
	return d.GetModeSelection(ctx, selection.SessionID, selection.TriggerCommentID)
}

// GetModeSelection reads the durable decision for one trigger.
func (d *DB) GetModeSelection(ctx context.Context, sessionID, triggerID int64) (ModeSelection, error) {
	var s ModeSelection
	err := d.sql.QueryRowContext(ctx, `SELECT session_id, trigger_comment_id, mode, label,
        label_applied, cleanup_pending, previous_label, previous_label_removed FROM mode_selections WHERE session_id = ? AND trigger_comment_id = ?`,
		sessionID, triggerID).Scan(&s.SessionID, &s.TriggerCommentID, &s.Mode, &s.Label, &s.LabelApplied, &s.CleanupPending, &s.PreviousLabel, &s.PreviousLabelRemoved)
	if err != nil {
		if err == sql.ErrNoRows {
			return ModeSelection{}, ErrNotFound
		}
		return ModeSelection{}, fmt.Errorf("store: read mode selection: %w", err)
	}
	return s, nil
}

// MarkModeLabelApplied records confirmation of the intended canonical state.
// Once confirmed, subsequent retries must not replay label effects.
func (d *DB) MarkModeLabelApplied(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE mode_selections SET label_applied = 1 WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: mark mode label applied: %w", err)
	}
	return requireAffected(res, "store: mark mode label applied")
}

// MarkModeCleanupPending makes journal retirement a restart-safe obligation.
// Existing pending rows from versions that removed labels use the same path.
func (d *DB) MarkModeCleanupPending(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE mode_selections SET cleanup_pending = 1 WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: mark mode cleanup pending: %w", err)
	}
	return requireAffected(res, "store: mark mode cleanup pending")
}

// DeleteModeSelection retires a completed trigger journal without a label effect.
func (d *DB) DeleteModeSelection(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `DELETE FROM mode_selections WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: delete mode selection: %w", err)
	}
	return requireAffected(res, "store: delete mode selection")
}

// PendingModeCleanup returns a selection explicitly pending cleanup or whose
// source delivery proves its report committed before a crash. Child refinement
// consumption is independent of the parent comment watermark.
func (d *DB) PendingModeCleanup(ctx context.Context, sessionID int64) (ModeSelection, bool, error) {
	var s ModeSelection
	err := d.sql.QueryRowContext(ctx, `SELECT m.session_id, m.trigger_comment_id, m.mode, m.label,
        m.label_applied, m.cleanup_pending, m.previous_label, m.previous_label_removed FROM mode_selections m
        LEFT JOIN watermarks w ON w.session_id = m.session_id
        WHERE m.session_id = ? AND (m.cleanup_pending = 1
        OR (m.trigger_comment_id > 0 AND w.last_comment_id >= m.trigger_comment_id
            AND NOT EXISTS (SELECT 1 FROM story_refinements f
                WHERE f.session_id = m.session_id AND f.comment_id = m.trigger_comment_id))
        OR EXISTS (SELECT 1 FROM story_refinements f
            WHERE f.session_id = m.session_id AND f.comment_id = m.trigger_comment_id AND f.consumed = 1)
        OR EXISTS (SELECT 1 FROM runs r WHERE r.session_id = m.session_id
                   AND r.trigger_comment_id = m.trigger_comment_id AND r.status = 'reported'))
        ORDER BY m.trigger_comment_id LIMIT 1`, sessionID).
		Scan(&s.SessionID, &s.TriggerCommentID, &s.Mode, &s.Label, &s.LabelApplied, &s.CleanupPending, &s.PreviousLabel, &s.PreviousLabelRemoved)
	if err == sql.ErrNoRows {
		return ModeSelection{}, false, nil
	}
	if err != nil {
		return ModeSelection{}, false, fmt.Errorf("store: pending mode cleanup: %w", err)
	}
	return s, true, nil
}

// RetireCompletedModeSelections retires recovery debt at startup, including
// legacy cleanup rows whose issues no longer appear in polling discovery.
// Issue-body admission plants watermark zero before execution, so that trigger
// requires a delivered run report rather than the watermark alone. Child
// refinements require their own delivery evidence, never the parent watermark.
func (d *DB) RetireCompletedModeSelections(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM mode_selections
        WHERE cleanup_pending = 1 OR (
            trigger_comment_id > 0 AND NOT EXISTS (
                SELECT 1 FROM story_refinements f WHERE f.session_id = mode_selections.session_id
                AND f.comment_id = mode_selections.trigger_comment_id
            ) AND EXISTS (
                SELECT 1 FROM watermarks w WHERE w.session_id = mode_selections.session_id
                AND w.last_comment_id >= mode_selections.trigger_comment_id
            )
        ) OR EXISTS (
            SELECT 1 FROM story_refinements f WHERE f.session_id = mode_selections.session_id
            AND f.comment_id = mode_selections.trigger_comment_id AND f.consumed = 1
        ) OR EXISTS (
            SELECT 1 FROM runs r WHERE r.session_id = mode_selections.session_id
            AND r.trigger_comment_id = mode_selections.trigger_comment_id AND r.status = 'reported'
        )`)
	if err != nil {
		return fmt.Errorf("store: retire completed mode selections: %w", err)
	}
	return nil
}

// MarkPreviousModeLabelRemoved records the transition boundary before addition.
// A later reappearance of the old label is an owner edit, not deletion debt.
func (d *DB) MarkPreviousModeLabelRemoved(ctx context.Context, sessionID, triggerID int64) error {
	res, err := d.sql.ExecContext(ctx, `UPDATE mode_selections SET previous_label_removed = 1 WHERE session_id = ? AND trigger_comment_id = ?`, sessionID, triggerID)
	if err != nil {
		return fmt.Errorf("store: mark previous mode label removed: %w", err)
	}
	return requireAffected(res, "store: mark previous mode label removed")
}
