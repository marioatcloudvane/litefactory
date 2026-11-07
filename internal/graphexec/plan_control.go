package graphexec

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanFailureNode names the shipped failure comment node every failing Plan
// path reaches. The graph checker pins it, so factory code may rely on the name.
const PlanFailureNode = "failed"

// PlanCreationBound is the dispatch bound of every node on a Plan cycle. The
// story creation cycle visits each of its nodes once per story of a full set,
// plus one pass for an owner-requested revision that runs before the cycle.
const PlanCreationBound = planbrief.MaxPlanStorySetEntries + 1

// Plan stage-decision and cycle conditions. They compare against the factory's
// fresh selection, never against anything an agent returned.
const (
	// PlanSetCondition selects a set proposal or parent discussion.
	PlanSetCondition = "set"
	// PlanCreateCondition is true while a promoted story has no draft yet.
	PlanCreateCondition = "create"
)

// planReviewReady is a response the runtime reports for an owner gate. It can
// never be a declared condition: review-ready is not approval.
const planReviewReady = "review_ready"

// PlanControlPolicy includes factory-only scheduling/promotion responses. The
// dedicated Plan node decoder admits neither stage selectors nor approved.
func PlanControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModePlan, Responses: []string{PlanSetCondition, "story", "technical", "acceptance", PlanCreateCondition, "ask", "answer", planReviewReady, "approved"}, Defaults: []string{PlanSetCondition, "story", "technical", "acceptance"}, Ask: "ask", Approved: "approved"}
}
