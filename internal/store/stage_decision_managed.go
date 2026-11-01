package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

type stageDecisionManagedBinding struct {
	Present    bool
	Generation int
	State      string
}

// stageDecisionGuardTx includes the separate managed lifecycle, which can
// change without changing the retained semantic Plan or historical graph.
func stageDecisionGuardTx(ctx context.Context, tx *sql.Tx, t StageDecisionTarget) (string, stageDecisionManagedBinding, error) {
	var binding stageDecisionManagedBinding
	guard, err := taskAdvisoryGuardTx(ctx, tx, t.TaskRef)
	if err != nil {
		return "", binding, err
	}
	if t.Workflow != "plan" {
		return guard, binding, nil
	}
	err = tx.QueryRowContext(ctx, `SELECT generation,state FROM managed_features WHERE parent_task=?`, t.PlanParent).Scan(&binding.Generation, &binding.State)
	if err == nil {
		binding.Present = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", binding, err
	}
	raw, err := json.Marshal(struct {
		Workflow string
		Managed  stageDecisionManagedBinding
	}{guard, binding})
	if err != nil {
		return "", binding, err
	}
	return submissionDigest(raw), binding, nil
}

// stageDecisionPlanContinuationTx checks the admitted source before the first
// worker launch. The consumed semantic wait is intentionally absent by then.
func stageDecisionPlanContinuationTx(ctx context.Context, tx *sql.Tx, t StageDecisionTarget) error {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, t.PlanParent).Scan(&raw); err != nil {
		return err
	}
	var state planbrief.PlanState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return err
	}
	state.ClearStaleWaits()
	if t.StoryKey == "" {
		if t.Stage != "set" || state.StorySet == nil || state.StorySet.Promoted || state.StorySet.Revision != t.ReviewBinding {
			return ErrStageDecisionTarget
		}
		return nil
	}
	for _, item := range state.Stories {
		if item.Key != t.StoryKey {
			continue
		}
		if !item.Active() || item.Blocked() || item.Stage != t.Stage || item.Candidate != t.Candidate {
			return ErrStageDecisionTarget
		}
		revision, err := state.ReviewRevision(item.Key, t.Stage)
		if err != nil {
			return errors.Join(ErrStageDecisionTarget, err)
		}
		if revision != t.ReviewBinding {
			return ErrStageDecisionTarget
		}
		return nil
	}
	return ErrStageDecisionTarget
}
