package graphexec_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

const approveAttrs = `lf_action="lf_plan_approve",lf_outcomes="approved,review,superseded,blocked,failed"`

// approvedStage replaces one stage's owner pair gate with a graph approval. The
// review node stays as the fallback owner stop.
func approvedStage(t *testing.T, raw, stage string) string {
	t.Helper()
	gate := regexp.MustCompile(`;` + stage + `_gate \[[^\]]*\];`)
	if !gate.MatchString(raw) {
		t.Fatalf("fixture has no %s gate", stage)
	}
	limit := "8"
	if stage == "story" {
		limit = "13"
	}
	raw = gate.ReplaceAllString(raw, ";"+stage+`_approve [`+approveAttrs+`,lf_source="`+stage+`_agent",lf_limit="`+limit+`"];`)
	for _, edge := range []string{`_gate -> promoted [lf_on="true"];`, `_gate -> ` + stage + `_review [lf_on="false"];`} {
		raw = strings.Replace(raw, stage+edge, "", 1)
	}
	raw = strings.Replace(raw, stage+`_draft -> `+stage+`_gate [lf_on="applied"];`, stage+`_draft -> `+stage+`_approve [lf_on="applied"];`, 1)
	edges := `approved -> stopped;review -> ` + stage + `_review;superseded -> stopped;blocked -> failed;failed -> failed;`
	var b strings.Builder
	for _, e := range strings.Split(strings.TrimSuffix(edges, ";"), ";") {
		on, to, _ := strings.Cut(e, " -> ")
		b.WriteString(stage + `_approve -> ` + to + ` [lf_on="` + on + `"];`)
	}
	return strings.Replace(raw, "failed [lf_action", b.String()+"failed [lf_action", 1)
}

func TestPlanApprove_Validator(t *testing.T) {
	base := planPolicyGraph()
	approved := base
	for _, stage := range []string{"story", "technical", "acceptance"} {
		approved = approvedStage(t, approved, stage)
	}
	if err := checkPlan(t, approved); err != nil {
		t.Fatalf("graph approval refused: %v", err)
	}
	if err := checkPlan(t, approvedStage(t, base, "story")); err != nil {
		t.Fatalf("one approved stage next to owner pairs refused: %v", err)
	}
	// An ask-review fallback and an lf_gate fallback are both owner stops.
	withGate := gatedStage(t, approvedStage(t, base, "story"), "technical")
	if err := checkPlan(t, withGate); err != nil {
		t.Fatalf("approve beside lf_gate refused: %v", err)
	}
	if err := checkPlan(t, base); err != nil {
		t.Fatalf("old pair refused: %v", err)
	}
	gated := base
	for _, stage := range []string{"story", "technical", "acceptance"} {
		gated = gatedStage(t, gated, stage)
	}
	if err := checkPlan(t, gated); err != nil {
		t.Fatalf("lf_gate graph refused: %v", err)
	}
	// lf_gate as the fallback owner stop of an approval.
	gateStop := strings.Replace(approved, `story_review [lf_action="lf_ask",lf_outcomes="published",lf_kind="review",lf_source="story_agent",lf_limit="13"]`, `story_review [`+gateAttrs+`,lf_source="story_agent",lf_limit="13"]`, 1)
	if gateStop == approved {
		t.Fatal("gate fallback mutation did not change the graph")
	}
	if err := checkPlan(t, gateStop); err != nil {
		t.Fatalf("lf_gate fallback refused: %v", err)
	}

	tests := []struct{ name, graph string }{
		{"set stage", approvedStage(t, base, "set")},
		{"approved reaches promotion", strings.Replace(approved, `story_approve -> stopped [lf_on="approved"]`, `story_approve -> promoted [lf_on="approved"]`, 1)},
		{"review reaches promotion", strings.Replace(approved, `story_approve -> story_review [lf_on="review"]`, `story_approve -> promoted [lf_on="review"]`, 1)},
		{"superseded reaches promotion", strings.Replace(approved, `story_approve -> stopped [lf_on="superseded"]`, `story_approve -> promoted [lf_on="superseded"]`, 1)},
		{"blocked reaches promotion", strings.Replace(approved, `story_approve -> failed [lf_on="blocked"]`, `story_approve -> promoted [lf_on="blocked"]`, 1)},
		{"review skips the owner stop", strings.Replace(approved, `story_approve -> story_review [lf_on="review"]`, `story_approve -> stopped [lf_on="review"]`, 1)},
		{"review reaches another agent's stop", strings.Replace(approved, `story_approve -> story_review [lf_on="review"]`, `story_approve -> technical_review [lf_on="review"]`, 1)},
		{"superseded not a terminal", strings.Replace(approved, `story_approve -> stopped [lf_on="superseded"]`, `story_approve -> story_review [lf_on="superseded"]`, 1)},
		{"blocked misses the failure node", strings.Replace(approved, `story_approve -> failed [lf_on="blocked"]`, `story_approve -> stopped [lf_on="blocked"]`, 1)},
		{"failed misses the failure node", strings.Replace(approved, `story_approve -> failed [lf_on="failed"]`, `story_approve -> stopped [lf_on="failed"]`, 1)},
		{"reads another agent", strings.Replace(approved, `story_approve [`+approveAttrs+`,lf_source="story_agent"`, `story_approve [`+approveAttrs+`,lf_source="technical_agent"`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.graph == approved || tt.graph == base {
				t.Fatal("mutation did not change the graph")
			}
			err := checkPlan(t, tt.graph)
			if err == nil {
				t.Fatal("graph accepted, want refusal")
			}
			t.Log(err)
		})
	}
}

func TestPlanApprove_OnlyInPlan(t *testing.T) {
	spec := graphexec.PlanApproveSpec()
	if spec.Name != graphexec.PlanApproveAction {
		t.Fatalf("spec name %q", spec.Name)
	}
}
