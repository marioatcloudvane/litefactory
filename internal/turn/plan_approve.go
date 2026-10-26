package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// planApproveEffect is the production lf_plan_approve handler. Like the story
// effect, it runs under the parent story-progress lock that ExecuteNodeAction
// takes before reserving the attempt.
type planApproveEffect struct {
	r      *planGraphRuntime
	locked bool
}

func (e planApproveEffect) Execute(ctx context.Context, _ Options, a ActivityRequest) (ActivityResult, error) {
	return e.r.approve(ctx, a)
}

func (e planApproveEffect) lockParent(ctx context.Context) (func() error, error) {
	return planStoryEffect{r: e.r}.lockParent(ctx)
}

func (e planApproveEffect) withParentLocked() ActivityHandler {
	e.locked = true
	return e
}

// approve records the graph's approval of the stage this run applied. The applied
// identity, every guard and the write happen in one saved-state update, so a
// guard that fails between the effect and now is seen when it matters.
func (r *planGraphRuntime) approve(ctx context.Context, a ActivityRequest) (ActivityResult, error) {
	if r.stage == planSetStage {
		return ActivityResult{}, errors.New("the set stage keeps its owner gate and cannot be approved by the graph")
	}
	node, _, err := r.load(ctx, a)
	if err != nil {
		return ActivityResult{}, err
	}
	if node.Set != nil || node.Request == nil {
		return ActivityResult{}, errors.New("plan approval lacks a story request in the retained result")
	}
	// A retirement always reaches the owner's confirmation.
	if node.Request.Operation == "retire" {
		return ActivityResult{Outcome: "review"}, nil
	}
	var result planbrief.GraphApproval
	err = r.opts.DB.UpdatePlanWorkflowState(ctx, r.parentRef.String(), func(s *planbrief.PlanState) (bool, error) {
		before := slices.Clone(s.GraphApprovedStages())
		var e error
		if result, e = s.ApproveByGraph(r.selected, r.stage, node.Request.Candidate, node.Request.RequestID); e != nil {
			return false, e
		}
		return result.Outcome == "approved" && !slices.Equal(before, s.GraphApprovedStages()), nil
	})
	if err != nil {
		return ActivityResult{}, err
	}
	out := ActivityResult{Outcome: result.Outcome}
	if result.Outcome == "blocked" {
		raw, e := json.Marshal(PlanStoryReceipt{Story: r.selected, Candidate: node.Request.Candidate, Outcome: "blocked", Detail: result.Reason})
		if e != nil {
			return ActivityResult{}, e
		}
		out.Kind, out.Output = store.GraphOutputJSON, raw
	}
	return out, nil
}
