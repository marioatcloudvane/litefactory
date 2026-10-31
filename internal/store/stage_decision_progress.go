package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// StageDecisionRevisionProgress observes continuation independently of admission.
// It never changes the immutable receipt or interprets a revision as approval.
func (db *DB) StageDecisionRevisionProgress(ctx context.Context, execution string) (string, error) {
	var progress string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var targetRaw, state, status, reason string
		var continuationInstance int64
		if err := tx.QueryRowContext(ctx, `SELECT r.target,r.state,COALESCE(d.status,''),COALESCE(d.cancellation_reason,''),COALESCE(d.instance_id,0) FROM stage_decision_receipts r LEFT JOIN graph_dispatches d ON d.id=r.dispatch_id WHERE r.execution_id=?`, execution).Scan(&targetRaw, &state, &status, &reason, &continuationInstance); err != nil {
			return err
		}
		var t StageDecisionTarget
		if err := json.Unmarshal([]byte(targetRaw), &t); err != nil {
			return err
		}
		if state != "applied" || t.Operation != "revise_task" {
			progress = "not_applicable"
			return nil
		}
		progress = "admitted; replacement work pending"
		if status == "" {
			progress = "Continuation details are unavailable or archived; recorded admission is retained."
			return nil
		}
		if status == "cancelled" {
			progress = "continuation stopped: " + reason
			return nil
		}
		if t.Workflow == "plan" {
			var raw string
			if err := tx.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, t.PlanParent).Scan(&raw); err != nil {
				return err
			}
			var plan planbrief.PlanState
			if err := json.Unmarshal([]byte(raw), &plan); err != nil {
				return err
			}
			candidate := ""
			if t.StoryKey == "" {
				if plan.StorySet != nil {
					candidate = plan.StorySet.Revision
				}
			} else {
				for _, story := range plan.Stories {
					if story.Key == t.StoryKey {
						candidate = story.Candidate
						break
					}
				}
			}
			if candidate != "" && candidate != t.Candidate {
				progress = "replacement available; review the current saved proposal before any approval"
				return nil
			}
		} else {
			var key string
			err := tx.QueryRowContext(ctx, `SELECT delivery_key FROM graph_owner_waits WHERE instance_id=? ORDER BY rowid DESC LIMIT 1`, t.InstanceID).Scan(&key)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if key != "" && key != t.WaitKey {
				progress = "continuation returned new saved work or an owner question; inspect the current stage"
				return nil
			}
		}
		if status == "reserved" || status == "running" {
			progress = "continuation running; replacement work is not approved"
			return nil
		}
		if status == "finished" {
			var active bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running'))`, continuationInstance).Scan(&active); err != nil {
				return err
			}
			if active {
				progress = "continuation in progress; replacement work is not approved"
			} else {
				progress = "continuation finished; no replacement is recorded yet"
			}
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return progress, err
}
