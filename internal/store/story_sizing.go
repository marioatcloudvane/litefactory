package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ApprovedStorySizing retains source facts, not a routing policy. Availability
// records whether the publication could prove their association to this story.
type ApprovedStorySizing struct {
	Version            int    `json:"version"`
	Available          bool   `json:"available"`
	Explanation        string `json:"explanation,omitempty"`
	Parent             string `json:"parent"`
	Generation         int    `json:"generation"`
	Story              string `json:"story"`
	IssueID            int64  `json:"issue_id"`
	ProjectionRevision string `json:"projection_revision"`
	PlanRevision       string `json:"plan_revision"`
	SetRevision        string `json:"set_revision,omitempty"`
	EntryID            string `json:"entry_id,omitempty"`
	Size               string `json:"size,omitempty"`
	SizeReason         string `json:"size_reason,omitempty"`
}

// RetainPublishedStorySizing joins exact publication identities to the retained
// Plan state under its optimistic revision. An identical replay is harmless.
func (db *DB) RetainPublishedStorySizing(ctx context.Context, parent string, generation int, key string, issueID int64, projectionRevision string, expectedPlanRevision int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var revision int64
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT revision,state_json FROM plan_workflow_states WHERE parent=?`, parent).Scan(&revision, &raw); err != nil {
			return err
		}
		if revision != expectedPlanRevision {
			return ErrPlanStateConflict
		}
		var state planbrief.PlanState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return fmt.Errorf("store: decode sizing Plan: %w", err)
		}
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM managed_stories s JOIN submission_projections p ON p.parent_task=s.parent_task AND p.generation=s.generation AND p.story_key=s.story_key AND p.issue_id=s.issue_id WHERE s.parent_task=? AND s.generation=? AND s.story_key=? AND s.issue_id=? AND s.body_hash=? AND p.revision=? AND EXISTS(SELECT 1 FROM plan_story_drafts d WHERE d.parent=s.parent_task AND d.story_key=s.story_key AND d.issue_id=s.issue_id AND d.issue_number=s.issue_number AND d.revision=p.revision AND d.retired=0)`, parent, generation, key, issueID, projectionRevision, projectionRevision).Scan(&found); err != nil {
			return err
		}
		facts := ApprovedStorySizing{Version: 1, Parent: parent, Generation: generation, Story: key, IssueID: issueID, ProjectionRevision: projectionRevision, PlanRevision: state.PlanSetRevision(), Explanation: "promoted sizing association unavailable"}
		if state.AssembledApproval != facts.PlanRevision {
			return fmt.Errorf("store: sizing publication assembled approval changed")
		}
		facts = derivePublishedSizing(state, facts, found == 1)
		encoded, err := json.Marshal(facts)
		if err != nil {
			return err
		}
		var old string
		err = tx.QueryRowContext(ctx, `SELECT facts_json FROM published_story_sizing WHERE parent_task=? AND generation=? AND story_key=? AND projection_revision=?`, parent, generation, key, projectionRevision).Scan(&old)
		if err == nil {
			// Re-encoding the retained facts drops fields that are no longer
			// kept (route_version of the retired parallel fast track), so an
			// identical replay of an older publication stays harmless.
			var retained ApprovedStorySizing
			if e := json.Unmarshal([]byte(old), &retained); e == nil {
				if normalized, e := json.Marshal(retained); e == nil {
					old = string(normalized)
				}
			}
			if old != string(encoded) {
				return fmt.Errorf("store: immutable published sizing conflict")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO published_story_sizing(parent_task,generation,story_key,projection_revision,facts_json) VALUES(?,?,?,?,?)`, parent, generation, key, projectionRevision, string(encoded))
		return err
	})
}

func derivePublishedSizing(state planbrief.PlanState, facts ApprovedStorySizing, association bool) ApprovedStorySizing {
	facts.Available = false
	facts.Explanation = "promoted sizing association unavailable"
	facts.SetRevision, facts.EntryID, facts.Size, facts.SizeReason = "", "", "", ""
	set := state.StorySet
	if set == nil || !set.Promoted || set.Revision == "" || set.Revision != set.PromotedRevision || set.Revision != planbrief.PlanStorySetRevision(set.Entries) {
		return facts
	}
	facts.SetRevision = set.Revision
	matches := 0
	for _, entry := range set.Entries {
		if set.Keys[entry.ID] == facts.Story {
			matches++
			facts.EntryID = entry.ID
			facts.Size = entry.Size
			facts.SizeReason = entry.SizeReason
		}
	}
	stories := 0
	for _, story := range state.Stories {
		if story.Key == facts.Story && story.Active() {
			stories++
		}
	}
	if matches == 1 && stories == 1 && association {
		facts.Available = true
		facts.Explanation = ""
	}
	if !association {
		facts.Explanation = "publication identity or projection association unavailable"
	}
	return facts
}

func sizingSourceUnchanged(facts ApprovedStorySizing, state planbrief.PlanState) bool {
	fresh := derivePublishedSizing(state, facts, true)
	return fresh.Available && fresh.SetRevision == facts.SetRevision && fresh.EntryID == facts.EntryID && fresh.Size == facts.Size && fresh.SizeReason == facts.SizeReason
}

func sizingSourceComplete(facts ApprovedStorySizing) bool {
	return strings.TrimSpace(facts.PlanRevision) != "" && strings.TrimSpace(facts.SetRevision) != "" && strings.TrimSpace(facts.EntryID) != ""
}

func approvedStorySizingTx(ctx context.Context, tx *sql.Tx, r ApprovedStoryRevision) (string, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT facts_json FROM published_story_sizing WHERE parent_task=? AND generation=? AND story_key=? AND projection_revision=?`, r.ParentTask, r.Generation, r.Key, r.Hash).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var facts ApprovedStorySizing
	if err = json.Unmarshal([]byte(raw), &facts); err != nil {
		return "", fmt.Errorf("store: decode published sizing: %w", err)
	}
	var stateRaw string
	if err = tx.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, r.ParentTask).Scan(&stateRaw); err != nil {
		return "", err
	}
	var state planbrief.PlanState
	if err = json.Unmarshal([]byte(stateRaw), &state); err != nil {
		return "", err
	}
	if facts.Version != 1 || (facts.Available && !sizingSourceComplete(facts)) || facts.Parent != r.ParentTask || facts.Generation != r.Generation || facts.Story != r.Key || facts.IssueID != r.IssueID || facts.ProjectionRevision != r.Hash || facts.PlanRevision != state.PlanSetRevision() || state.AssembledApproval != facts.PlanRevision || state.SavedRevision != facts.PlanRevision || state.PublishedRevision != facts.PlanRevision || (facts.Available && !sizingSourceUnchanged(facts, state)) {
		facts.Available = false
		facts.Explanation = "publication does not match approved Plan identities or revision"
		encoded, e := json.Marshal(facts)
		return string(encoded), e
	}
	return raw, nil
}

// GetApprovedStorySizing reads only the immutable approved revision. Old records
// have no facts; database, malformed JSON and cancellation failures remain errors.
func (db *DB) GetApprovedStorySizing(ctx context.Context, parent string, generation int, key, approvedDigest string) (*ApprovedStorySizing, error) {
	var raw, digest string
	var issueID int64
	err := db.sql.QueryRowContext(ctx, `SELECT r.sizing_json,r.digest,s.issue_id FROM approved_story_revisions r JOIN managed_stories s USING(parent_task,generation,story_key) WHERE r.parent_task=? AND r.generation=? AND r.story_key=?`, parent, generation, key).Scan(&raw, &digest, &issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if digest != approvedDigest {
		return nil, fmt.Errorf("store: approved sizing digest mismatch")
	}
	if raw == "" {
		return nil, nil
	}
	var facts ApprovedStorySizing
	if err = json.Unmarshal([]byte(raw), &facts); err != nil {
		return nil, fmt.Errorf("store: decode approved sizing: %w", err)
	}
	if facts.Version != 1 || (facts.Available && !sizingSourceComplete(facts)) || facts.Parent != parent || facts.Generation != generation || facts.Story != key || facts.ProjectionRevision != digest || facts.IssueID != issueID {
		facts.Available = false
		facts.Explanation = "approved sizing source identity, version or binding incomplete"
	}
	return &facts, nil
}
