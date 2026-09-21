package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ReconcilePlanStoryDraft retains a recognized owner edit against its expected
// local base. Observed bytes are immutable evidence, not a second editable source.
// The caller authenticates the owner and checks parent/issue/provenance and open
// state. This transaction never claims atomic compare-and-swap on the forge.
func (db *DB) ReconcilePlanStoryDraft(ctx context.Context, parent, key, expectedRevision, observedRevision, candidate, stage string, draft planbrief.PlanDraft, observedTitle, observedBody string) error {
	if parent == "" || key != draft.Key || candidate == "" || len(candidate) > 256 || len(expectedRevision) != 64 || !slices.Contains([]string{"story", "technical", "acceptance"}, stage) || len(observedTitle) > 256 || len(observedBody) > planbrief.MaxStoryBodyBytes {
		return errors.New("invalid Plan owner edit observation")
	}
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(observedTitle+"\x00"+observedBody)))
	if observedRevision != revision {
		return errors.New("plan observed revision does not match retained bytes")
	}
	raw, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	if len(raw) > 128*1024 {
		return errors.New("plan owner edit exceeds draft bound")
	}
	observationID := expectedRevision + ":" + observedRevision
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var base, observed, title, body, content, oldStage, oldCandidate string
		err := tx.QueryRowContext(ctx, `SELECT base_revision,observed_revision,observed_title,observed_body,draft_json,stage,candidate FROM plan_story_owner_edits WHERE parent=? AND story_key=? AND observation_id=?`, parent, key, observationID).Scan(&base, &observed, &title, &body, &content, &oldStage, &oldCandidate)
		if err == nil {
			if oldCandidate != candidate || base != expectedRevision || observed != observedRevision || title != observedTitle || body != observedBody || content != string(raw) || oldStage != stage {
				return errors.New("plan owner edit candidate reused with different content")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var pending bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM plan_story_candidates c WHERE c.parent=? AND c.story_key=? AND COALESCE((SELECT outcome FROM plan_story_effects e WHERE e.parent=c.parent AND e.request_id=c.request_id ORDER BY id DESC LIMIT 1),'pending') IN ('pending','uncertain'))`, parent, key).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return errors.New("plan owner edit waits for pending effect reconciliation")
		}
		result, err := tx.ExecContext(ctx, `UPDATE plan_story_drafts SET revision=?,candidate=?,stage=?,draft_json=? WHERE parent=? AND story_key=? AND revision=? AND retired=0 AND issue_id>0`, observedRevision, candidate, stage, string(raw), parent, key, expectedRevision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("plan owner edit base revision or active identity conflict")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO plan_story_owner_edits(parent,story_key,observation_id,candidate,base_revision,observed_revision,observed_title,observed_body,draft_json,stage) VALUES(?,?,?,?,?,?,?,?,?,?)`, parent, key, observationID, candidate, expectedRevision, observedRevision, observedTitle, observedBody, string(raw), stage)
		return err
	})
}

// PlanOwnerEditObservation preserves exact issue text for replay and diagnosis.
type PlanOwnerEditObservation struct {
	Parent, Key, Candidate, BaseRevision, ObservedRevision, Title, Body, DraftJSON, Stage string
}

// GetPlanOwnerEditObservation loads the latest immutable observation of a semantic candidate.
func (db *DB) GetPlanOwnerEditObservation(ctx context.Context, parent, key, candidate string) (PlanOwnerEditObservation, error) {
	out := PlanOwnerEditObservation{Parent: parent, Key: key, Candidate: candidate}
	err := db.sql.QueryRowContext(ctx, `SELECT base_revision,observed_revision,observed_title,observed_body,draft_json,stage FROM plan_story_owner_edits WHERE parent=? AND story_key=? AND candidate=? ORDER BY rowid DESC LIMIT 1`, parent, key, candidate).Scan(&out.BaseRevision, &out.ObservedRevision, &out.Title, &out.Body, &out.DraftJSON, &out.Stage)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}
