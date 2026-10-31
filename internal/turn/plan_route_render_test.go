package turn

import (
	"encoding/json"
	"os"
	"regexp"
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
	wantReasons := []routeExpectation{{"parallel", planbrief.PlanRouteReasonFastTrack, ""}, {"normal", planbrief.PlanRouteReasonSize, "S"}, {"normal", planbrief.PlanRouteReasonSize, "S"}, {"normal", planbrief.PlanRouteReasonSizing, ""}}
	for i, want := range wantReasons {
		if reviews[i].Size != entries[i].Size || reviews[i].SizeReason != entries[i].SizeReason {
			t.Fatalf("review[%d]=%+v", i, reviews[i])
		}
		want.check(t, reviews[i])
	}
	// A changed captured comparison changes the prediction; the renderer owns no size policy.
	for i := range p.Nodes {
		if p.Nodes[i].Attrs["lf_field"] == "story.size" {
			p.Nodes[i].Attrs["lf_condition"] = "M"
		}
	}
	revised := r.setRouteReviews(set)
	routeExpectation{"normal", planbrief.PlanRouteReasonSize, "M"}.check(t, revised[0])
	routeExpectation{"parallel", planbrief.PlanRouteReasonFastTrack, ""}.check(t, revised[1])
	if set.Keys != nil {
		t.Fatal("prediction mutated unpromoted set")
	}
	r.definition = nil
	for _, review := range r.setRouteReviews(set) {
		if review.Route != "normal" || review.Reason != "" || review.FastTrackSize != "" {
			t.Fatalf("legacy capture predicted a route reason: %+v", review)
		}
	}
}

type routeExpectation struct{ route, reason, fastTrack string }

func (w routeExpectation) check(t *testing.T, r planbrief.PlanRouteReview) {
	t.Helper()
	if r.Route != w.route || r.Reason != w.reason || r.FastTrackSize != w.fastTrack {
		t.Fatalf("review %s = route %q reason %q fast track %q, want %+v", r.Story, r.Route, r.Reason, r.FastTrackSize, w)
	}
}

var rawGraphEvidence = regexp.MustCompile(`\S+=\S+ \[(true|false)\]`)

// assertNoGraphEvidence checks that owner text carries no capture digest,
// graph node name or raw field=value [bool] evaluation.
func assertNoGraphEvidence(t *testing.T, p *graphexec.Profile, text string, extra ...string) {
	t.Helper()
	if rawGraphEvidence.MatchString(text) {
		t.Fatalf("owner text shows raw graph evaluation: %s", text)
	}
	forbidden := append([]string{p.Digest, "route event", "Route basis", "Growth reason"}, extra...)
	for _, n := range p.Nodes {
		forbidden = append(forbidden, n.Name)
	}
	for _, value := range forbidden {
		if value != "" && regexp.MustCompile(`\b`+regexp.QuoteMeta(value)+`\b`).MatchString(text) {
			t.Fatalf("owner text shows graph evidence %q: %s", value, text)
		}
	}
}

func routeTableRows(t *testing.T, text string) (string, []string) {
	t.Helper()
	i := strings.Index(text, "| Story | Size | Size reason | Route |")
	if i < 0 {
		t.Fatalf("route table missing: %s", text)
	}
	lines := strings.Split(text[i:], "\n")
	var rows []string
	for _, line := range lines[2:] {
		if !strings.HasPrefix(line, "|") {
			break
		}
		rows = append(rows, line)
	}
	return lines[0], rows
}

func TestPlanSetReviewIncludesRouteFields(t *testing.T) {
	p := routeRenderProfile(t)
	r := planGraphRuntime{definition: &teamsource.WorkflowDefinition{Profile: p}}
	proposal := &agentresult.PlanStorySet{Entries: []agentresult.PlanStorySetEntry{{ID: "s1", Name: "Export", Intent: "Export records", Size: "S", SizeReason: "One format"}, {ID: "m1", Name: "Import", Intent: "Import records", Size: "M", SizeReason: "Several parsers"}, {ID: "l1", Name: "Sync", Intent: "Sync records", Size: "L", SizeReason: "Many systems"}, {ID: "x1", Name: "Audit", Intent: "Audit records", Size: "S"}}}
	s := planbrief.PlanState{}
	if _, _, err := s.ProposeStorySet(planSetEntries(proposal)); err != nil {
		t.Fatal(err)
	}
	text, err := r.setReviewText(s, planRetained{Set: proposal}, "Proposed records export")
	if err != nil {
		t.Fatal(err)
	}
	header, rows := routeTableRows(t, text)
	if header != "| Story | Size | Size reason | Route |" || len(rows) != len(proposal.Entries) {
		t.Fatalf("route table structure: %q %q", header, rows)
	}
	for i, entry := range proposal.Entries {
		if !strings.Contains(rows[i], entry.ID) || !strings.Contains(rows[i], "| "+entry.Size+" |") {
			t.Fatalf("route row %d omitted story or size: %s", i, rows[i])
		}
	}
	if !strings.Contains(text, "One format") {
		t.Fatalf("set review omitted size reason: %s", text)
	}
	assertNoGraphEvidence(t, p, text, "Graph-approved stages", "sizing_", "story.size=")
	reviews := r.setRouteReviews(s.StorySet)
	routeExpectation{"parallel", planbrief.PlanRouteReasonFastTrack, ""}.check(t, reviews[0])
	routeExpectation{"normal", planbrief.PlanRouteReasonSize, "S"}.check(t, reviews[1])
	routeExpectation{"normal", planbrief.PlanRouteReasonSize, "S"}.check(t, reviews[2])
	routeExpectation{"normal", planbrief.PlanRouteReasonSizing, ""}.check(t, reviews[3])
}

func TestPlanFinalRouteReviewsExplainStoredRoutes(t *testing.T) {
	p := routeRenderProfile(t)
	r := planGraphRuntime{definition: &teamsource.WorkflowDefinition{Profile: p}}
	route := func(current, fallback string) *planbrief.PlanRouteState {
		return &planbrief.PlanRouteState{Version: planbrief.PlanParallelVersion, Current: current, Capture: "captured", SourceRevision: "set", Fallback: fallback}
	}
	s := planbrief.PlanState{
		StorySet: &planbrief.PlanStorySetState{Keys: map[string]string{"e-s": "small", "e-m": "medium", "e-x": "invalid", "e-g": "grown"}, Entries: []planbrief.PlanStorySetEntry{{ID: "e-s", Size: "S", SizeReason: "Few files"}, {ID: "e-m", Size: "M", SizeReason: "Several parts"}, {ID: "e-x", Size: "S"}, {ID: "e-g", Size: "S", SizeReason: "Looked small"}}},
		Stories:  []planbrief.PlanStoryState{{Key: "small", Route: route("parallel", "")}, {Key: "medium", Route: route("normal", "")}, {Key: "invalid", Route: route("normal", "")}, {Key: "grown", Route: route("normal", "Broader data model")}},
	}
	before, _ := json.Marshal(s)
	reviews := r.finalRouteReviews(s)
	routeExpectation{"parallel", planbrief.PlanRouteReasonFastTrack, ""}.check(t, reviews[0])
	routeExpectation{"normal", planbrief.PlanRouteReasonSize, "S"}.check(t, reviews[1])
	routeExpectation{"normal", planbrief.PlanRouteReasonSizing, ""}.check(t, reviews[2])
	routeExpectation{"normal", planbrief.PlanRouteReasonGrowth, ""}.check(t, reviews[3])
	if reviews[3].Growth != "Broader data model" {
		t.Fatalf("growth reason lost: %+v", reviews[3])
	}
	for i, item := range s.Stories {
		if reviews[i].Route != item.Route.Current {
			t.Fatalf("final review overrode stored route: %+v", reviews[i])
		}
	}
	// The stored route stays authoritative: a disagreeing graph explains nothing.
	s.Stories[0].Route.Current = "normal"
	if got := r.finalRouteReviews(s)[0]; got.Route != "normal" || got.Reason != "" {
		t.Fatalf("disagreeing prediction replaced stored route: %+v", got)
	}
	s.Stories[0].Route.Current = "parallel"
	text := planbrief.RenderPlanRoutes(reviews)
	assertNoGraphEvidence(t, p, text, "captured", "Graph-approved stages")
	if after, _ := json.Marshal(s); string(after) != string(before) {
		t.Fatalf("rendering changed retained state:\n%s\n%s", before, after)
	}
}

func TestPlanFinalReviewIncludesRetainedRoutes(t *testing.T) {
	f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	r := &planGraphRuntime{opts: f.opts, req: base, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue}
	s := graphPlanState(t, f)
	item := &s.Stories[0]
	s.StorySet = &planbrief.PlanStorySetState{Keys: map[string]string{"entry": item.Key}, Entries: []planbrief.PlanStorySetEntry{{ID: "entry", Size: "S", SizeReason: "Original scope"}}}
	item.Route = &planbrief.PlanRouteState{Version: planbrief.PlanParallelVersion, Current: "normal", Capture: "captured-reference", SourceRevision: "promoted-reference", Fallback: "Broader data model", Events: []planbrief.PlanRouteEvent{{Route: "parallel", Node: "sizing_small", Fact: "story.size", Value: "S"}, {Route: "normal", Node: "growth", Reason: "Broader data model"}}}
	approveStageBy(t, &s, item.Key, "story", planbrief.ApprovalByGraph)
	before, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	text, err := r.assembledText(t.Context(), s, "final-reference")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"Original scope", "Broader data model", "| S |", "final-reference", "| Story | Size | Size reason | Route |"} {
		if !strings.Contains(text, value) {
			t.Fatalf("final review omitted field %q: %s", value, text)
		}
	}
	for _, value := range []string{"captured-reference", "promoted-reference", "route event"} {
		if strings.Contains(text, value) {
			t.Fatalf("final review shows retained evidence %q: %s", value, text)
		}
	}
	if r.definition != nil {
		assertNoGraphEvidence(t, r.definition.Profile, text)
	} else if rawGraphEvidence.MatchString(text) || strings.Contains(text, "sizing_small") {
		t.Fatalf("final review shows raw graph evidence: %s", text)
	}
	after, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || len(s.Stories[0].Route.Events) != 2 {
		t.Fatalf("rendering changed retained state:\n%s\n%s", before, after)
	}
}
