package graphexec

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
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
