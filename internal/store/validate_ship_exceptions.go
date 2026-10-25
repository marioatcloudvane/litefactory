package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ValidateShipWaiver is the immutable shipping expansion in the owner exception
// ledger. Evidence remains attributed to TestedCommit, never DeliveryCommit.
type ValidateShipWaiver struct {
	Task             string           `json:"task"`
	InstanceID       int64            `json:"instance_id"`
	TestedCommit     string           `json:"tested_commit"`
	DeliveryCommit   string           `json:"delivery_commit"`
	EvidenceRevision string           `json:"evidence_revision"`
	ReportRevision   string           `json:"report_revision"`
	Blockers         []string         `json:"blockers"`
	Evidence         json.RawMessage  `json:"evidence"`
	DeliveryEffects  []ValidateEffect `json:"delivery_effects,omitempty"`
	ActorID          int64            `json:"actor_id"`
	CommentID        int64            `json:"comment_id"`
	Body             string           `json:"body"`
	Reason           string           `json:"reason"`
	CreatedAt        time.Time        `json:"created_at"`
}

// RetainValidateShipWaiver saves an exact expansion before any delivery intent.
// Replaying the source never re-expands it or adopts edited comment bytes.
func (db *DB) RetainValidateShipWaiver(ctx context.Context, w ValidateShipWaiver) error {
	if w.Task == "" || w.InstanceID < 1 || !validCommit(w.TestedCommit) || !validCommit(w.DeliveryCommit) || w.ActorID < 1 || w.CommentID < 1 || w.Reason == "" || w.EvidenceRevision == "" || w.CreatedAt.IsZero() || !json.Valid(w.Evidence) {
		return fmt.Errorf("invalid Validate shipping exception")
	}
	raw, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var old string
		err := tx.QueryRowContext(ctx, `SELECT expansion_json FROM owner_exceptions WHERE parent_task=? AND comment_id=?`, w.Task, w.CommentID).Scan(&old)
		if err == nil {
			if old != string(raw) {
				return fmt.Errorf("conflicting shipping exception replay")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var task string
		if err := tx.QueryRowContext(ctx, `SELECT s.task_ref FROM graph_instances i JOIN sessions s ON s.id=i.session_id WHERE i.id=?`, w.InstanceID).Scan(&task); err != nil {
			return err
		}
		if task != w.Task {
			return fmt.Errorf("shipping exception task mismatch")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO owner_exceptions(parent_task,generation,comment_id,actor_id,request_json,expansion_json,validate_instance_id) VALUES(?,0,?,?,?,?,?)`, w.Task, w.CommentID, w.ActorID, string(raw), string(raw), w.InstanceID)
		return err
	})
}

// LatestValidateShipWaiver reads the retained shipping decision for one run.
func (db *DB) LatestValidateShipWaiver(ctx context.Context, instanceID int64) (ValidateShipWaiver, error) {
	var w ValidateShipWaiver
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT expansion_json FROM owner_exceptions WHERE validate_instance_id=? ORDER BY id DESC LIMIT 1`, instanceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	if err != nil {
		return w, err
	}
	err = json.Unmarshal([]byte(raw), &w)
	return w, err
}
