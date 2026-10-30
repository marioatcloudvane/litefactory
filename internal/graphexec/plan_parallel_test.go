package graphexec

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"os"
	"testing"
)

func TestPlanFactComparisons(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"story.size", "S"}, {"story.sizing_valid", "true"}, {"story.fallback", "false"}} {
		attrs := map[string]string{"lf_field": tc.field, "lf_condition": tc.value}
		c, err := DecodeCondition(ResearchIfAction, attrs, PlanControlPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if got, err := c.EvaluatePlanFacts(map[string]string{tc.field: tc.value}); err != nil || got != "true" {
			t.Fatalf("%s: %s %v", tc.field, got, err)
		}
		if got, _ := c.EvaluatePlanFacts(nil); got != "false" {
			t.Fatal("missing fact advanced")
		}
		if _, err := DecodeCondition(ResearchIfAction, attrs, ResearchControlPolicy()); err == nil {
			t.Fatal("research admitted Plan fact")
		}
	}
}

func TestPlanParallelBoundsAndOwnership(t *testing.T) {
	p := &Profile{Mode: agent.ModePlan}
	n := Node{Action: PlanParallelAction, Attrs: map[string]string{"lf_members": "s,t,a", "lf_rounds": "3"}}
	for _, r := range []struct{ name, role string }{{"s", "story"}, {"t", "technical"}, {"a", "acceptance"}} {
		p.Nodes = append(p.Nodes, Node{Name: r.name, Action: "lf_agent", Attrs: map[string]string{"lf_plan_role": r.role, "lf_agent_markdown": "plan-" + r.role}})
	}
	if _, err := DecodePlanParallel(p, n); err != nil {
		t.Fatal(err)
	}
	n.Attrs["lf_members"] = "s,s,a"
	if _, err := DecodePlanParallel(p, n); err == nil {
		t.Fatal("duplicate role admitted")
	}
	n.Attrs["lf_members"] = "s,t,a"
	n.Attrs["lf_rounds"] = "0"
	if _, err := DecodePlanParallel(p, n); err == nil {
		t.Fatal("unbounded group admitted")
	}
}

func TestCanonicalParallelPlanGraph(t *testing.T) {
	raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanControlGraph(p); err != nil {
		t.Fatal(err)
	}
	r := Registry{}
	for _, spec := range []ActionSpec{
		ControlSpec(ResearchIfAction, PlanControlPolicy()), ControlSpec(ResearchLoopAction, PlanControlPolicy()),
		PlanParallelSpec(), PlanStorySpec(), PlanApproveSpec(), JudgeSpec(agent.ModePlan),
		{Name: "lf_agent", Mode: agent.ModePlan, Outcomes: []string{"success", "failed"}, Attributes: AgentAttributes()},
		{Name: "lf_ask", Mode: agent.ModePlan, Outcomes: []string{"published"}},
		{Name: "lf_comment", Mode: agent.ModePlan, Outcomes: []string{"published"}},
		{Name: PlanGateAction, Mode: agent.ModePlan, Outcomes: []string{"published"}},
	} {
		if err := r.Register(spec); err != nil {
			t.Fatal(err)
		}
	}
	if err := Validate(p, r); err != nil {
		t.Fatal(err)
	}
	if got, err := SlotCost(p, "parallel_group"); err != nil || got != 4 {
		t.Fatalf("capacity=%d err=%v", got, err)
	}
	for i := range p.Nodes {
		if p.Nodes[i].Name == "technical_agent" {
			p.Nodes[i].Attrs[slotBudgetAttribute] = "3"
		}
	}
	if got, err := SlotCost(p, "parallel_group"); err != nil || got != 6 {
		t.Fatalf("weighted capacity=%d err=%v", got, err)
	}
}

func TestParallelGraphRejectsGuardBypasses(t *testing.T) {
	raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, node, field, value string }{
		{"duplicate role", "parallel_group", "lf_members", "story_agent,story_agent,acceptance_agent"},
		{"unbounded reconciliation", "parallel_group", "lf_rounds", "0"},
		{"effect changes stage", "parallel_story_draft", "lf_stage", "acceptance"},
		{"approval changes stage", "parallel_technical_approve", "lf_stage", "story"},
		{"approval owner review changes stage", "parallel_acceptance_review", "lf_stage", "technical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParseProfile(raw)
			if err != nil {
				t.Fatal(err)
			}
			for i := range p.Nodes {
				if p.Nodes[i].Name == tc.node {
					p.Nodes[i].Attrs[tc.field] = tc.value
				}
			}
			if err := ValidatePlanControlGraph(p); err == nil {
				t.Fatal("unsafe graph admitted")
			}
		})
	}
	p, err := ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range p.Nodes {
		if p.Nodes[i].Name == "parallel_story_check" {
			delete(p.Nodes[i].Attrs, "lf_stage")
		}
	}
	if err := ValidateJudgeGraph(p); err == nil {
		t.Fatal("unbound parallel advisory stage admitted")
	}
}

func TestParallelCreationRoutingAndVisibleStops(t *testing.T) {
	raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanControlGraph(p); err != nil {
		t.Fatal(err)
	}
	next, ok := Decide(p, "story_pick", "true")
	if !ok {
		t.Fatal("creation pick lacks continuation")
	}
	facts := map[string]string{"story.sizing_valid": "true", "story.fallback": "false", "story.size": "S"}
	for steps := 0; steps < len(p.Nodes); steps++ {
		node, ok := p.Node(next)
		if !ok {
			t.Fatal("creation routing target missing")
		}
		if node.Action == PlanParallelAction {
			break
		}
		condition, err := DecodeCondition(node.Action, node.Attrs, PlanControlPolicy())
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := condition.EvaluatePlanFacts(facts)
		if err != nil {
			t.Fatal(err)
		}
		next, ok = Decide(p, node.Name, outcome)
		if !ok {
			t.Fatal("creation fact has no selected edge")
		}
	}
	if node, _ := p.Node(next); node.Action != PlanParallelAction {
		t.Fatalf("next small story reached %s", next)
	}
	if next, ok := Decide(p, "parallel_group", "stopped"); !ok || next != PlanFailureNode {
		t.Fatalf("stopped group lacks visible report: %s", next)
	}
	for i := range p.Edges {
		if p.Edges[i].From == "parallel_group" && p.Edges[i].On == "stopped" {
			p.Edges[i].To = "parallel_story_draft"
		}
	}
	if err := ValidatePlanControlGraph(p); err == nil {
		t.Fatal("stopped group resumed application")
	}
}

func TestParallelDiscussionBypassIsGraphOwned(t *testing.T) {
	raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanControlGraph(p); err != nil {
		t.Fatal(err)
	}
	first, ok := Decide(p, "select_set", "false")
	if !ok {
		t.Fatal("set bypass missing")
	}
	node, ok := p.Node(first)
	if !ok {
		t.Fatal("discussion decision missing")
	}
	condition, err := DecodeCondition(node.Action, node.Attrs, PlanControlPolicy())
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := condition.Evaluate("discussion", true)
	if err != nil || outcome != "true" {
		t.Fatalf("discussion comparison: %s %v", outcome, err)
	}
	normal, ok := Decide(p, node.Name, outcome)
	if !ok {
		t.Fatal("discussion has no normal route")
	}
	target, ok := p.Node(normal)
	if !ok || target.Attrs["lf_field"] != "response" || target.Attrs["lf_condition"] != "story" {
		t.Fatalf("discussion reached %s", normal)
	}
	outcome, err = condition.Evaluate("story", true)
	if err != nil || outcome != "false" {
		t.Fatalf("story comparison: %s %v", outcome, err)
	}
	work, ok := Decide(p, node.Name, outcome)
	if !ok {
		t.Fatal("story lacks sizing route")
	}
	target, ok = p.Node(work)
	if !ok || target.Attrs["lf_field"] != "story.sizing_valid" {
		t.Fatalf("story reached %s", work)
	}
}
