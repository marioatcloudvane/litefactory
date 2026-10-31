package planbrief_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func parallelParts() map[string]planbrief.PlanParallelContribution {
	d := draftFixture("acceptance")
	story := d
	story.Architecture = nil
	story.Guidance = nil
	story.Tasks = nil
	story.Criteria = nil
	return map[string]planbrief.PlanParallelContribution{
		"story":      {Draft: story},
		"technical":  {Draft: planbrief.PlanDraft{Architecture: d.Architecture, Guidance: d.Guidance}},
		"acceptance": {Draft: planbrief.PlanDraft{Tasks: d.Tasks, Criteria: d.Criteria}},
	}
}
func TestPlanParallelCompletionOrdersAndSealing(t *testing.T) {
	orders := [][]string{{"story", "technical", "acceptance"}, {"story", "acceptance", "technical"}, {"technical", "story", "acceptance"}, {"technical", "acceptance", "story"}, {"acceptance", "technical", "story"}, {"acceptance", "story", "technical"}}
	var expected string
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	for _, order := range orders {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			g, err := planbrief.NewPlanParallelGroup("group", "basis", 3, planbrief.PlanDraft{Key: "story-a"})
			if err != nil {
				t.Fatal(err)
			}
			parts := parallelParts()
			for i, role := range order {
				if err = g.Collect("basis", role, int64(i+1), "r1", parts[role]); err != nil {
					t.Fatal(err)
				}
				if i < 2 {
					if ready, err := g.Reconcile(scope); ready || err != nil {
						t.Fatalf("premature result %v %v", ready, err)
					}
				}
			}
			ready, err := g.Reconcile(scope)
			if err != nil || ready {
				t.Fatalf("agreement missing: %v %v", ready, err)
			}
			if expected != "" && expected != g.Digest {
				t.Fatal("completion order changed digest")
			}
			expected = g.Digest
			for i, role := range order {
				c := parts[role]
				c.Agreement = g.Digest
				if err = g.Collect("basis", role, int64(i+10), "r2", c); err != nil {
					t.Fatal(err)
				}
			}
			if err = g.Seal("basis", scope); err != nil {
				t.Fatal(err)
			}
			if _, err = g.Projection("basis", "technical"); err == nil {
				t.Fatal("skipped story stage")
			}
			d, err := g.Projection("basis", "story")
			if err != nil || d.Architecture != nil || len(d.Tasks) != 0 {
				t.Fatalf("story projection: %+v %v", d, err)
			}
			g.Applied["story"] = "receipt-story"
			if _, err = g.Projection("changed", "technical"); err == nil {
				t.Fatal("accepted stale basis")
			}
			if _, err = g.Projection("basis", "technical"); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(g)
			var resumed planbrief.PlanParallelGroup
			if err = json.Unmarshal(raw, &resumed); err != nil {
				t.Fatal(err)
			}
			resumedRaw, _ := json.Marshal(&resumed)
			if !bytes.Equal(raw, resumedRaw) {
				t.Fatal("group lost on resume")
			}
		})
	}
}
func TestPlanParallelOwnershipDuplicatesAndDurableBlockers(t *testing.T) {
	parts := parallelParts()
	if err := planbrief.ValidateParallelContribution("technical", parts["story"]); err == nil {
		t.Fatal("cross-role intent accepted")
	}
	if err := planbrief.ValidateParallelContribution("story", parts["acceptance"]); err == nil {
		t.Fatal("cross-role acceptance accepted")
	}
	g, _ := planbrief.NewPlanParallelGroup("g", "b", 2, planbrief.PlanDraft{Key: "story-a"})
	c := parts["story"]
	c.Questions = []string{"Scope?"}
	if err := g.Collect("b", "story", 2, "r", c); err != nil {
		t.Fatal(err)
	}
	if err := g.Collect("b", "story", 2, "r", c); err != nil {
		t.Fatal("duplicate not idempotent", err)
	}
	c.Draft.InScope[0] = "caller mutation"
	if g.Members["story"].Contribution.Draft.InScope[0] == "caller mutation" {
		t.Fatal("retained contribution shares caller memory")
	}

	if err := g.Collect("b", "story", 1, "late", parts["story"]); err == nil {
		t.Fatal("expired attempt accepted")
	}
	if err := g.Collect("b", "story", 3, "r2", parts["story"]); err != nil {
		t.Fatal(err)
	}
	if len(g.Blockers) != 1 {
		t.Fatal("question erased by success")
	}
	if _, err := g.Reconcile(planbrief.ContentScope{}); err == nil {
		t.Fatal("question did not block")
	}
}
func TestPlanParallelChangedProposalInvalidatesAgreementAndExhausts(t *testing.T) {
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	g, _ := planbrief.NewPlanParallelGroup("g", "b", 1, planbrief.PlanDraft{Key: "story-a"})
	parts := parallelParts()
	for i, role := range []string{"story", "technical", "acceptance"} {
		if err := g.Collect("b", role, int64(i+1), "r", parts[role]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.Reconcile(scope); err != nil {
		t.Fatal(err)
	}
	c := parts["story"]
	c.Draft.Problem = "Revised problem"
	c.Agreement = g.Digest
	if err := g.Collect("b", "story", 5, "r2", c); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Reconcile(scope); err == nil {
		t.Fatal("continuation exhaustion ignored")
	}
	if g.Sealed || len(g.Blockers) == 0 {
		t.Fatal("exhaustion not retained")
	}
}
func TestPlanParallelGrowthAndRouteReview(t *testing.T) {
	s := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "a", Stage: "acceptance", Approvals: map[string]string{"story": "s", "technical": "t"}, ApprovedBy: map[string]string{"story": "graph"}}, {Key: "b", Stage: "technical", Approvals: map[string]string{"story": "sibling"}}}}
	if err := s.RecordPlanRoute("a", planbrief.PlanRouteState{Cycle: "cycle", Capture: "captured", SourceRevision: "set"}, planbrief.PlanRouteEvent{Route: "parallel", Node: "size", Fact: "story.size", Value: "S"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPlanGrowth("a", "technical", "Needs broader architecture", "technical-member"); err != nil {
		t.Fatal(err)
	}
	if s.Stories[0].Approvals["story"] != "s" || s.Stories[0].Approvals["technical"] != "" || s.Stories[1].Approvals["story"] != "sibling" {
		t.Fatal("growth changed unaffected approvals")
	}
	if err := s.RecordPlanRoute("a", planbrief.PlanRouteState{}, planbrief.PlanRouteEvent{Route: "parallel"}); err == nil {
		t.Fatal("sticky fallback cleared")
	}
	reviews := s.RouteReviews()
	if reviews[0].Growth == "" || reviews[0].Route != "normal" || len(reviews[0].History) != 2 || len(reviews[0].GraphApproved) != 1 {
		t.Fatalf("missing review facts: %+v", reviews[0])
	}
	rendered := planbrief.RenderPlanRoutes(reviews)
	if !strings.Contains(rendered, "Needs broader architecture") || !strings.Contains(rendered, "story.size=S") {
		t.Fatal("route evidence missing from rendering")
	}
	var legacy planbrief.PlanState
	if err := json.Unmarshal([]byte(`{"shared_revision":"old","stories":[{"key":"a","stage":"story"}]}`), &legacy); err != nil || legacy.Stories[0].Route != nil || legacy.Stories[0].Parallel != nil {
		t.Fatal("legacy state changed", err)
	}
}

func TestPlanParallelSparseContributionPreservesBasisSections(t *testing.T) {
	base := draftFixture("acceptance")
	base.Dependencies.StoryKeys = []string{"story-upstream"}
	base.Frontend = true
	base.Design = []string{"https://example.com/design"}
	g, err := planbrief.NewPlanParallelGroup("g", "basis", 3, base)
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]planbrief.PlanParallelContribution{
		"story":      {Draft: planbrief.PlanDraft{Name: "Rename only"}},
		"technical":  {Draft: planbrief.PlanDraft{Guidance: []string{"agent-setup"}}},
		"acceptance": {},
	}
	for i, role := range []string{"story", "technical", "acceptance"} {
		if err := g.Collect("basis", role, int64(i+1), "r", parts[role]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.Reconcile(planbrief.ContentScope{Roles: []string{"engineer"}}); err != nil {
		t.Fatal(err)
	}
	if g.Proposal.Name != "Rename only" || len(g.Proposal.Dependencies.StoryKeys) != 1 || !g.Proposal.Frontend || len(g.Proposal.Design) != 1 || len(g.Proposal.Tasks) != 1 {
		t.Fatal("omission erased basis sections", g.Proposal)
	}
}

func TestPlanParallelAgreementOnlyRetainsRoleProjections(t *testing.T) {
	scope := planbrief.ContentScope{Roles: []string{"engineer"}}
	g, err := planbrief.NewPlanParallelGroup("g", "basis", 3, planbrief.PlanDraft{Key: "story-a", Decisions: []string{"Retain approved constraint"}})
	if err != nil {
		t.Fatal(err)
	}
	parts := parallelParts()
	for i, role := range []string{"story", "technical", "acceptance"} {
		if err := g.Collect("basis", role, int64(i+1), "r1", parts[role]); err != nil {
			t.Fatal(err)
		}
	}
	if ready, err := g.Reconcile(scope); err != nil || ready {
		t.Fatal("initial contribution unexpectedly ready", ready, err)
	}
	digest := g.Digest
	for i, role := range []string{"story", "technical", "acceptance"} {
		if err := g.Collect("basis", role, int64(i+4), "r2", planbrief.PlanParallelContribution{Agreement: digest}); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Seal("basis", scope); err != nil {
		t.Fatal(err)
	}
	if g.Digest != digest || len(g.Proposal.Tasks) != 1 || g.Proposal.Architecture == nil || len(g.Proposal.Decisions) != 1 {
		t.Fatal("agreement-only round erased proposal", g.Proposal)
	}
}

func TestPlanParallelDurableSignalsSurviveLaterSuccess(t *testing.T) {
	for _, signal := range []string{"question", "conflict", "growth", "failure"} {
		t.Run(signal, func(t *testing.T) {
			g, _ := planbrief.NewPlanParallelGroup("g", "basis", 3, planbrief.PlanDraft{Key: "story-a"})
			c := parallelParts()["story"]
			switch signal {
			case "question":
				c.Questions = []string{"Needs owner answer"}
			case "conflict":
				c.Conflicts = []string{"Decisions disagree"}
			case "growth":
				c.Growth = "Requires wider scope"
			case "failure":
				c.Failure = "Technical reasoning interrupted"
			}
			if err := g.Collect("basis", "story", 1, "r1", c); err != nil {
				t.Fatal(err)
			}
			if err := g.Collect("basis", "story", 2, "r2", planbrief.PlanParallelContribution{Agreement: "later"}); err != nil {
				t.Fatal(err)
			}
			if len(g.Blockers) != 1 {
				t.Fatal("durable signal erased")
			}
			if _, err := g.Reconcile(planbrief.ContentScope{}); err == nil {
				t.Fatal("later success cleared blocker")
			}
			if signal == "growth" && g.Members["story"].Contribution.Growth == "" {
				t.Fatal("growth source lost")
			}
		})
	}
}

func TestPlanParallelUnknownStateVersionFailsClosed(t *testing.T) {
	g, _ := planbrief.NewPlanParallelGroup("g", "basis", 3, draftFixture("acceptance"))
	g.Version = planbrief.PlanParallelVersion + 1
	g.Sealed = true
	g.Digest = g.Proposal.Revision()
	if _, err := g.Reconcile(planbrief.ContentScope{}); err == nil {
		t.Fatal("unknown version reconciled")
	}
	if err := g.Seal("basis", planbrief.ContentScope{}); err == nil {
		t.Fatal("unknown version sealed")
	}
	if _, err := g.Projection("basis", "story"); err == nil {
		t.Fatal("unknown version projected")
	}
}

func TestPlanParallelUnknownRouteVersionRefusesMutation(t *testing.T) {
	s := planbrief.PlanState{SharedRevision: "shared", Stories: []planbrief.PlanStoryState{{Key: "story-a", Route: &planbrief.PlanRouteState{Version: planbrief.PlanParallelVersion + 1, Cycle: "cycle", Capture: "capture", Current: "normal"}}}}
	before, _ := json.Marshal(s)
	if err := s.RecordPlanRoute("story-a", planbrief.PlanRouteState{}, planbrief.PlanRouteEvent{Route: "parallel", Node: "sizing"}); err == nil {
		t.Fatal("unknown route version changed")
	}
	if err := s.RecordPlanGrowth("story-a", "technical", "broader scope", "member"); err == nil {
		t.Fatal("growth rewrote unknown route version")
	}
	after, _ := json.Marshal(s)
	if !bytes.Equal(before, after) {
		t.Fatal("refused route mutation changed retained evidence")
	}
}

func TestPlanParallelGrowthStagePreservesOnlyProvenUnaffectedApprovals(t *testing.T) {
	tests := []struct {
		name, current, want string
		parts               map[string]planbrief.PlanParallelContribution
	}{
		{"technical growth preserves unchanged story", "technical", "technical", map[string]planbrief.PlanParallelContribution{"story": {}, "technical": {Growth: "More architecture needed", Draft: planbrief.PlanDraft{Architecture: &planbrief.PlanArchitecture{General: []planbrief.GeneralDecision{{Area: "New boundary", Decision: "Split adapter", Rationale: "Growth"}}}}}}},
		{"changed earlier intent invalidates story", "technical", "story", map[string]planbrief.PlanParallelContribution{"story": {Draft: planbrief.PlanDraft{Problem: "Broader problem"}}, "technical": {Growth: "More architecture needed"}}},
		{"missing story evidence is uncertain", "technical", "story", map[string]planbrief.PlanParallelContribution{"technical": {Growth: "More architecture needed"}}},
		{"acceptance growth preserves exact earlier semantics", "acceptance", "acceptance", map[string]planbrief.PlanParallelContribution{"story": {}, "technical": {}, "acceptance": {Growth: "More proof needed", Draft: planbrief.PlanDraft{Tasks: []planbrief.TaskContent{{ID: "T-002", Description: "More proof"}}}}}},
		{"missing technical evidence starts technical review", "acceptance", "technical", map[string]planbrief.PlanParallelContribution{"story": {}, "acceptance": {Growth: "More proof needed"}}},
		{"later shared decisions affect technical semantics", "acceptance", "technical", map[string]planbrief.PlanParallelContribution{"story": {}, "technical": {}, "acceptance": {Growth: "More proof needed", Draft: planbrief.PlanDraft{Decisions: []string{"Change the interface contract"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := planbrief.NewPlanParallelGroup("g", "basis", 3, draftFixture("acceptance"))
			if err != nil {
				t.Fatal(err)
			}
			for i, role := range []string{"story", "technical", "acceptance"} {
				if c, ok := tt.parts[role]; ok {
					if err := g.Collect("basis", role, int64(i+1), "accepted", c); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, _ := json.Marshal(g)
			if got := g.GrowthStage(tt.current); got != tt.want {
				t.Fatalf("growth stage %s, want %s", got, tt.want)
			}
			after, _ := json.Marshal(g)
			if !bytes.Equal(before, after) {
				t.Fatal("growth analysis changed retained state")
			}
		})
	}
}
