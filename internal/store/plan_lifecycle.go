package store

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// ObservePlanStoryLifecycle journals verified lifecycle and local hold changes
// using the workflow's expected-revision guard. The draft binding and all prior
// snapshots remain intact, including for externally closed children.
func (db *DB) ObservePlanStoryLifecycle(ctx context.Context, parent, key string, observation planbrief.PlanLifecycle, hold string) error {
	state, revision, err := db.GetPlanWorkflowState(ctx, parent)
	if err != nil {
		return err
	}
	found := false
	for _, story := range state.Stories {
		if story.Key == key {
			found = true
			break
		}
	}
	if !found {
		binding, loadErr := db.GetPlanStoryDraft(ctx, parent, key)
		if loadErr != nil {
			return loadErr
		}
		var draft planbrief.PlanDraft
		if binding.DraftJSON != "" {
			if loadErr = json.Unmarshal([]byte(binding.DraftJSON), &draft); loadErr != nil {
				return loadErr
			}
		}
		stage := binding.Stage
		if stage == "" {
			stage = "story"
		}
		state.Stories = append(state.Stories, planbrief.PlanStoryState{Key: key, Candidate: binding.Candidate, Stage: stage, Draft: draft, Retired: binding.Retired})
		if state.SharedRevision == "" {
			state.SharedRevision = "initial"
		}
	}
	changed, err := state.ObserveLifecycle(key, observation, hold)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	_, err = db.SavePlanWorkflowState(ctx, parent, revision, state)
	return err
}
