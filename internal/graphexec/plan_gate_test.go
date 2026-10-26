package graphexec_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

const gateAttrs = `lf_action="lf_gate",lf_outcomes="published",lf_command="/approve-plan",lf_args="story,stage,candidate"`

// gatedStage replaces one stage's lf_if/lf_ask owner pair with the generic gate.
func gatedStage(t *testing.T, raw, stage string) string {
	t.Helper()
	gate := regexp.MustCompile(`;` + stage + `_gate \[[^\]]*\];`)
	review := regexp.MustCompile(`;` + stage + `_review \[[^\]]*\];`)
	if !gate.MatchString(raw) || !review.MatchString(raw) {
		t.Fatalf("fixture has no %s owner pair", stage)
	}
	raw = gate.ReplaceAllString(raw, ";")
	limit := "8"
	if stage == "story" {
		limit = "13"
	}
	raw = review.ReplaceAllString(raw, ";"+stage+`_review [`+gateAttrs+`,lf_source="`+stage+`_agent",lf_limit="`+limit+`"];`)
	for _, edge := range []string{`_gate -> promoted [lf_on="true"];`, `_gate -> ` + stage + `_review [lf_on="false"];`} {
		raw = strings.Replace(raw, stage+edge, "", 1)
	}
	return strings.Replace(raw, stage+`_draft -> `+stage+`_gate [lf_on="applied"];`, stage+`_draft -> `+stage+`_review [lf_on="applied"];`, 1)
}

func TestPlanGate_Validator(t *testing.T) {
	base := planPolicyGraph()
	if err := checkPlan(t, base); err != nil {
		t.Fatalf("old owner pair refused: %v", err)
	}
	gated := base
	for _, stage := range []string{"story", "technical", "acceptance"} {
		gated = gatedStage(t, gated, stage)
	}
	if err := checkPlan(t, gated); err != nil {
		t.Fatalf("gate refused: %v", err)
	}
	// set_review is an lf_ask owner review next to a set_gate; a gate in its place is refused.
	tests := []struct{ name, graph, want string }{
		{"gate on set stage", gatedStage(t, gated, "set"), "set stage"},
		{"unknown command", strings.Replace(gated, `lf_command="/approve-plan"`, `lf_command="/approve"`, 1), "lf_command"},
		{"wrong args order", strings.Replace(gated, `lf_args="story,stage,candidate"`, `lf_args="stage,story,candidate"`, 1), "lf_args"},
		{"gate reads another agent", strings.Replace(gated, `story_review [`+gateAttrs+`,lf_source="story_agent"`, `story_review [`+gateAttrs+`,lf_source="technical_agent"`, 1), "reviewed as"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.graph == gated {
				t.Fatal("mutation did not change the graph")
			}
			err := checkPlan(t, tt.graph)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want refusal mentioning %q", err, tt.want)
			}
		})
	}
}

func TestPlanGate_UnregisteredOutsidePlan(t *testing.T) {
	var r graphexec.Registry
	if err := r.Register(graphexec.ActionSpec{Name: "lf_agent", Mode: agent.ModeResearch, Outcomes: []string{"success", "failed"}}); err != nil {
		t.Fatal(err)
	}
	p, err := graphexec.ParseProfile([]byte(`digraph research { graph [team="builtin",mode="research",profile_version="1",start="work"];
work [lf_action="lf_agent",lf_outcomes="success,failed"];
gate [` + gateAttrs + `,lf_source="work"];
done [lf_terminal="true"];
work -> gate [lf_on="success"]; work -> done [lf_on="failed"]; gate -> done [lf_on="published"]; }`))
	if err != nil {
		t.Fatal(err)
	}
	if err := graphexec.Validate(p, r); err == nil || !strings.Contains(err.Error(), "lf_gate") {
		t.Fatalf("lf_gate admitted outside Plan: %v", err)
	}
	if spec := graphexec.PlanGateSpec(); spec.Mode != agent.ModePlan {
		t.Fatalf("gate spec mode %q, want plan", spec.Mode)
	}
}
