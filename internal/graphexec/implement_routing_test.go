package graphexec

import (
	"maps"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestImplementFactConditionsAreClosed(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"story.size", "S"}, {"story.size", "M"}, {"story.size", "L"}, {"story.sizing_valid", "true"}, {"story.sizing_valid", "false"}} {
		attrs := map[string]string{"lf_field": tc.field, "lf_condition": tc.value}
		condition, err := DecodeCondition(ResearchIfAction, attrs, ImplementControlPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if outcome, err := condition.EvaluateImplementFacts(map[string]string{tc.field: tc.value}); err != nil || outcome != "true" {
			t.Fatalf("matching fact = %s, %v", outcome, err)
		}
		if outcome, err := condition.EvaluateImplementFacts(nil); err != nil || outcome != "false" {
			t.Fatalf("missing fact = %s, %v", outcome, err)
		}
		for _, key := range []string{"lf_source", "lf_default"} {
			forbidden := maps.Clone(attrs)
			forbidden[key] = ""
			if _, err := DecodeCondition(ResearchIfAction, forbidden, ImplementControlPolicy()); err == nil {
				t.Fatalf("accepted %s", key)
			}
		}
		if _, err := DecodeCondition(ResearchLoopAction, attrs, ImplementControlPolicy()); err == nil {
			t.Fatal("fact loop admitted")
		}
	}
	for _, tc := range []struct{ field, value string }{{"story.size", "s"}, {"story.size", ""}, {"story.size", "XL"}, {"story.size", "true"}, {"story.sizing_valid", "S"}, {"story.sizing_valid", "TRUE"}, {"story.fallback", "false"}, {"response", "completed"}} {
		attrs := map[string]string{"lf_field": tc.field, "lf_condition": tc.value}
		if _, err := DecodeCondition(ResearchIfAction, attrs, ImplementControlPolicy()); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	for _, policy := range []ControlPolicy{ResearchControlPolicy(), PlanControlPolicy(), {Mode: agent.ModeValidate}, {Workflow: agent.WorkflowBug}} {
		if _, err := DecodeCondition(ResearchIfAction, map[string]string{"lf_field": "story.size", "lf_condition": "S"}, policy); err == nil {
			t.Fatalf("cross-mode fact admitted: %s", policy.Mode)
		}
	}
}
