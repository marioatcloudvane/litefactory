package graphexec

import "github.com/cloudvane/litefactory/internal/agent"

// PlanApproveAction is the Plan-only approval a graph gives a stage that has no
// owner gate. It approves only when its guards hold at the moment of saving;
// any doubt leaves the stage to the owner stop the graph declares.
const PlanApproveAction = "lf_plan_approve"

// PlanApproveSpec declares the action. Only Plan registers it.
func PlanApproveSpec() ActionSpec {
	return ActionSpec{Name: PlanApproveAction, Mode: agent.ModePlan, Outcomes: []string{"approved", "review", "superseded", "blocked", "failed"}}
}
