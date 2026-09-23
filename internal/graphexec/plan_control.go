package graphexec

import (
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
)

// PlanControlPolicy includes factory-only scheduling/promotion responses. The
// dedicated Plan node decoder admits neither stage selectors nor approved.
func PlanControlPolicy() ControlPolicy {
	return ControlPolicy{Mode: agent.ModePlan, Responses: []string{"story", "technical", "acceptance", "ask", "review_ready", "approved"}, Defaults: []string{"story", "technical", "acceptance"}, Ask: "ask", Approved: "approved"}
}

// ValidatePlanControlGraph admits the three-stage Plan topology. Fixed source
// bindings prevent another story/stage output from satisfying an owner gate.
// Effect outcomes differ from Research document generation; no Research policy
// or evidence gate is changed to accommodate Plan.
func ValidatePlanControlGraph(p *Profile) error {
	if p == nil || p.Mode != agent.ModePlan || p.Start != "select_story" {
		return errors.New("invalid Plan graph entry")
	}
	var faults []error
	edge := func(from, out, to string) {
		actual, ok := Decide(p, from, out)
		if !ok || actual != to {
			faults = append(faults, fmt.Errorf("plan edge %s/%s must reach %s", from, out, to))
		}
	}
	attr := func(name, key, value string) {
		n, ok := p.Node(name)
		if !ok || n.Attrs[key] != value {
			faults = append(faults, fmt.Errorf("plan node %s requires %s=%s", name, key, value))
		}
	}
	for _, n := range p.Nodes {
		if !n.Terminal && n.Limit <= 0 {
			faults = append(faults, fmt.Errorf("plan node %s requires positive bound", n.Name))
		}
		if n.Action == ResearchLoopAction || n.Action == ResearchIfAction {
			_, err := DecodeCondition(n.Action, n.Attrs, PlanControlPolicy())
			faults = append(faults, err)
		}
	}
	if len(p.Nodes) != 28 {
		faults = append(faults, errors.New("plan graph must contain the closed three-stage node set"))
	}
	for _, stage := range []string{"story", "technical", "acceptance"} {
		attr("select_"+stage, "lf_action", "lf_if")
		attr("select_"+stage, "lf_condition", stage)
		edge("select_"+stage, "true", stage+"_loop")
		attr(stage+"_loop", "lf_action", "lf_loop")
		attr(stage+"_loop", "lf_condition", stage)
		attr(stage+"_agent", "lf_action", "lf_agent")
		attr(stage+"_agent", "lf_agent_markdown", "plan-"+stage)
		attr(stage+"_question", "lf_action", "lf_if")
		attr(stage+"_question", "lf_condition", "ask")
		attr(stage+"_question", "lf_source", stage+"_agent")
		attr(stage+"_draft", "lf_action", PlanStoryAction)
		attr(stage+"_draft", "lf_source", stage+"_agent")
		attr(stage+"_gate", "lf_action", "lf_if")
		attr(stage+"_gate", "lf_condition", "approved")
		for _, kind := range []string{"ask", "review"} {
			attr(stage+"_"+kind, "lf_action", "lf_ask")
			attr(stage+"_"+kind, "lf_source", stage+"_agent")
		}
		attr(stage+"_ask", "lf_kind", "question")
		attr(stage+"_review", "lf_kind", "review")
		edge(stage+"_loop", "true", stage+"_agent")
		edge(stage+"_loop", "false", "stopped")
		edge(stage+"_agent", "success", stage+"_question")
		edge(stage+"_agent", "failed", "failed")
		edge(stage+"_question", "true", stage+"_ask")
		edge(stage+"_question", "false", stage+"_draft")
		edge(stage+"_draft", "applied", stage+"_gate")
		edge(stage+"_draft", "failed", "failed")
		edge(stage+"_draft", "conflict", "stopped")
		edge(stage+"_draft", "uncertain", "stopped")
		edge(stage+"_gate", "true", "promoted")
		edge(stage+"_gate", "false", stage+"_review")
		edge(stage+"_ask", "published", "waiting")
		edge(stage+"_review", "published", "waiting")
	}
	edge("select_story", "false", "select_technical")
	edge("select_technical", "false", "select_acceptance")
	edge("select_acceptance", "false", "stopped")
	attr("failed", "lf_action", "lf_comment")
	attr("failed", "lf_kind", "failure")
	edge("failed", "published", "stopped")
	for _, name := range []string{"waiting", "promoted", "stopped"} {
		n, ok := p.Node(name)
		if !ok || !n.Terminal {
			faults = append(faults, fmt.Errorf("plan terminal %s missing", name))
		}
	}
	return errors.Join(faults...)
}
