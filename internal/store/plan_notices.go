package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanStoryNotice identifies one retained material change awaiting reporting.
type PlanStoryNotice struct {
	ID          int64
	Parent, Key string
	State       planbrief.PlanNoticeState
}

// retainPlanNoticesTx records material changes in the same transaction as their
// workflow snapshot. Reporting cannot discard or coalesce intervening changes.
func retainPlanNoticesTx(ctx context.Context, tx *sql.Tx, parent string, state planbrief.PlanState) error {
	for _, story := range state.Stories {
		material := story.NoticeState()
		raw, err := json.Marshal(material)
		if err != nil {
			return err
		}
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT material_json FROM plan_story_notice_states WHERE parent=? AND story_key=?`, parent, story.Key).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if previous == string(raw) {
			continue
		}
		var prior planbrief.PlanNoticeState
		if previous != "" {
			if err = json.Unmarshal([]byte(previous), &prior); err != nil {
				return err
			}
		}
		if material.Significant() || prior.Significant() {
			if _, err = tx.ExecContext(ctx, `INSERT INTO plan_story_notices(parent,story_key,material_json) VALUES(?,?,?)`, parent, story.Key, string(raw)); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO plan_story_notice_states(parent,story_key,material_json) VALUES(?,?,?) ON CONFLICT(parent,story_key) DO UPDATE SET material_json=excluded.material_json`, parent, story.Key, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

// CapturePlanStoryNotices backfills the current material state after upgrade.
func (db *DB) CapturePlanStoryNotices(ctx context.Context, parent string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, parent).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		var state planbrief.PlanState
		if err = json.Unmarshal([]byte(raw), &state); err != nil {
			return err
		}
		return retainPlanNoticesTx(ctx, tx, parent, state)
	})
}

// PendingPlanStoryNotices returns a bounded batch in durable fair recovery order.
func (db *DB) PendingPlanStoryNotices(ctx context.Context, parent string) ([]PlanStoryNotice, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,story_key,material_json FROM plan_story_notices WHERE parent=? AND delivered=0 ORDER BY attempts,id LIMIT 16`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanStoryNotice
	for rows.Next() {
		n := PlanStoryNotice{Parent: parent}
		var raw string
		if err = rows.Scan(&n.ID, &n.Key, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &n.State); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// PlanReplyExplanation binds immutable explanation text to its original child input.
type PlanReplyExplanation struct {
	DeliveryKey, Parent, Key string
	ChildNumber              int
	CommentID                int64
	Reason                   string
}

// PlanReplyExplanation reads the retained disposition of one original child reply.
func (db *DB) PlanReplyExplanation(ctx context.Context, parent string, commentID int64) (PlanReplyExplanation, error) {
	var e PlanReplyExplanation
	e.Parent = parent
	e.CommentID = commentID
	err := db.sql.QueryRowContext(ctx, `SELECT delivery_key,story_key,child_number,reason FROM plan_reply_explanations WHERE parent=? AND comment_id=?`, parent, commentID).Scan(&e.DeliveryKey, &e.Key, &e.ChildNumber, &e.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// RetainPlanReplyExplanation preserves the first explanation and rejects identity drift.
func (db *DB) RetainPlanReplyExplanation(ctx context.Context, e PlanReplyExplanation) (PlanReplyExplanation, error) {
	if e.Parent == "" || e.Key == "" || e.DeliveryKey == "" || e.ChildNumber < 1 || e.CommentID < 1 || e.Reason == "" {
		return e, errors.New("invalid Plan reply explanation")
	}
	_, err := db.sql.ExecContext(ctx, `INSERT INTO plan_reply_explanations(delivery_key,parent,story_key,child_number,comment_id,reason) VALUES(?,?,?,?,?,?) ON CONFLICT(parent,comment_id) DO NOTHING`, e.DeliveryKey, e.Parent, e.Key, e.ChildNumber, e.CommentID, e.Reason)
	if err != nil {
		return e, err
	}
	retained, err := db.PlanReplyExplanation(ctx, e.Parent, e.CommentID)
	if err != nil {
		return e, err
	}
	if retained.DeliveryKey != e.DeliveryKey || retained.ChildNumber != e.ChildNumber || retained.Key != e.Key {
		return e, errors.New("plan reply identity changed")
	}
	return retained, nil
}

// PlanNoticeNamespace marks a factory-authored graph Plan comment to the owner
// (stories created, plan ready, owner waits, a stopped run). Its identity is its
// purpose alone, so a repeat on any later tick resumes the one delivery; it
// answers no input, so completing it consumes nothing.
const PlanNoticeNamespace = "plan-notice"

// completePlanNoticeTx replaces ordinary parent input consumption for semantic
// notices and child explanations. A confirmed reply receipt advances only its
// own child's discovery cursor, atomically with delivery completion.
func completePlanNoticeTx(ctx context.Context, tx *sql.Tx, d ReportDelivery) error {
	switch d.TriggerNamespace {
	case PlanNoticeNamespace:
		return nil
	case "plan-observation":
		var id int64
		if _, err := fmt.Sscanf(d.Purpose, "plan-observation:%d", &id); err != nil || id < 1 {
			return errors.New("invalid Plan notice identity")
		}
		result, err := tx.ExecContext(ctx, `UPDATE plan_story_notices SET delivered=1 WHERE id=? AND parent=?`, id, d.Task)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("plan notice missing")
		}
	case "plan-reply":
		var child int
		var comment int64
		var parent string
		if err := tx.QueryRowContext(ctx, `SELECT parent,child_number,comment_id FROM plan_reply_explanations WHERE delivery_key=?`, d.Key).Scan(&parent, &child, &comment); err != nil {
			return err
		}
		if parent != d.Task || comment != d.TriggerID {
			return errors.New("plan reply delivery mismatch")
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO story_discovery(session_id,child_number,comment_id) VALUES(?,?,?) ON CONFLICT(session_id,child_number) DO UPDATE SET comment_id=MAX(comment_id,excluded.comment_id)`, d.SessionID, child, comment)
		return err
	default:
		return errors.New("invalid Plan report namespace")
	}
	return nil
}

// AttemptPlanStoryNotice rotates bounded recoveries past permanently uncertain receipts.
func (db *DB) AttemptPlanStoryNotice(ctx context.Context, parent string, id int64) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE plan_story_notices SET attempts=attempts+1 WHERE parent=? AND id=? AND delivered=0`, parent, id)
	return err
}
