package turn

import (
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func routeRenderProfile(t *testing.T) *graphexec.Profile {
	t.Helper()
	raw, err := os.ReadFile("../../teams/source/base-team/plan/plan.dot")
	if err != nil {
		t.Fatal(err)
	}
	p, err := graphexec.ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlanSetRouteRenderingUsesCapturedComparisons(t *testing.T) {
	p := routeRenderProfile(t)
	r := planGraphRuntime{definition: &teamsource.WorkflowDefinition{Profile: p}}
	entries := []planbrief.PlanStorySetEntry{{ID: "small", Name: "Small", Intent: "Small task", Size: "S", SizeReason: "Few files"}, {ID: "medium", Name: "Medium", Intent: "Medium task", Size: "M", SizeReason: "Several parts"}, {ID: "large", Name: "Large", Intent: "Large task", Size: "L", SizeReason: "Many parts"}, {ID: "invalid", Name: "Unknown", Intent: "Unknown task", Size: "S"}}
	set := &planbrief.PlanStorySetState{Entries: entries}
	reviews := r.setRouteReviews(set)
	for i, want := range []string{"parallel", "normal", "normal", "normal"} {
		if reviews[i].Route != want || reviews[i].Size != entries[i].Size || reviews[i].SizeReason != entries[i].SizeReason {
			t.Fatalf("review[%d]=%+v", i, reviews[i])
		}
	}
	if !strings.Contains(reviews[0].Basis, "story.size=S") {
		t.Fatalf("missing graph fact evidence: %+v", reviews[0])
	}
	// A changed captured comparison changes the prediction; the renderer owns no size policy.
	for i := range p.Nodes {
		if p.Nodes[i].Attrs["lf_field"] == "story.size" {
			p.Nodes[i].Attrs["lf_condition"] = "M"
		}
	}
	revised := r.setRouteReviews(set)
	if revised[0].Route != "normal" || revised[1].Route != "parallel" {
		t.Fatalf("renderer ignored graph comparison: %+v", revised)
	}
	if set.Keys != nil {
		t.Fatal("prediction mutated unpromoted set")
	}
	r.definition = nil
	for _, review := range r.setRouteReviews(set) {
		if review.Route != "normal" {
			t.Fatal("legacy capture predicted parallel route")
		}
	}
}

func TestPlanSetReviewIncludesRouteFields(t *testing.T) {
	r := planGraphRuntime{definition: &teamsource.WorkflowDefinition{Profile: routeRenderProfile(t)}}
	proposal := &agentresult.PlanStorySet{Entries: []agentresult.PlanStorySetEntry{{ID: "s1", Name: "Export", Intent: "Export records", Size: "S", SizeReason: "One format"}}}
	s := planbrief.PlanState{}
	if _, _, err := s.ProposeStorySet(planSetEntries(proposal)); err != nil {
		t.Fatal(err)
	}
	text, err := r.setReviewText(s, planRetained{Set: proposal}, "Proposed records export")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"s1", "One format", "parallel", "story.size=S"} {
		if !strings.Contains(text, value) {
			t.Fatalf("set review omitted field %q: %s", value, text)
		}
	}
}

func TestPlanFinalReviewIncludesRetainedRoutes(t *testing.T) {
	f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	r := &planGraphRuntime{opts: f.opts, req: base, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue}
	s := graphPlanState(t, f)
	item := &s.Stories[0]
	s.StorySet = &planbrief.PlanStorySetState{Keys: map[string]string{"entry": item.Key}, Entries: []planbrief.PlanStorySetEntry{{ID: "entry", Size: "S", SizeReason: "Original scope"}}}
	item.Route = &planbrief.PlanRouteState{Current: "normal", Capture: "captured-reference", SourceRevision: "promoted-reference", Fallback: "Broader data model", Events: []planbrief.PlanRouteEvent{{Route: "parallel", Node: "sizing_small", Fact: "story.size", Value: "S"}, {Route: "normal", Node: "growth", Reason: "Broader data model"}}}
	approveStageBy(t, &s, item.Key, "story", planbrief.ApprovalByGraph)
	text, err := r.assembledText(t.Context(), s, "final-reference")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Original scope", "captured-reference", "promoted-reference", "Broader data model", "story.size=S", "final-reference"} {
		if !strings.Contains(text, value) {
			t.Fatalf("final review omitted retained field %q: %s", value, text)
		}
	}
}
