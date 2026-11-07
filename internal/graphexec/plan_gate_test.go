package graphexec_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

const gateAttrs = `lf_action="lf_gate",lf_outcomes="published",lf_command="/approve-plan",lf_args="story,stage,candidate"`

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
