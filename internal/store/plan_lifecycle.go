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
	return db.UpdatePlanWorkflowState(ctx, parent, func(state *planbrief.PlanState) (bool, error) {
		found := false
		for _, story := range state.Stories {
			if story.Key == key {
				found = true
				break
			}
		}
		if !found {
			binding, err := db.GetPlanStoryDraft(ctx, parent, key)
			if err != nil {
				return false, err
			}
			var draft planbrief.PlanDraft
			if binding.DraftJSON != "" {
				if err = json.Unmarshal([]byte(binding.DraftJSON), &draft); err != nil {
					return false, err
				}
			}
			stage := binding.Stage
			if stage == "" {
				stage = "story"
			}
			state.Stories = append(state.Stories, planbrief.PlanStoryState{Key: key, Candidate: binding.Candidate, Stage: stage, Draft: draft, Retired: binding.Retired})
		}
		changed, err := state.ObserveLifecycle(key, observation, hold)
		if err != nil {
			return false, err
		}
		return changed, nil
	})
}
