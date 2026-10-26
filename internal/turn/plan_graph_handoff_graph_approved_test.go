package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func approveStageBy(t *testing.T, s *planbrief.PlanState, key, stage, by string) {
	t.Helper()
	ref, err := s.ReviewRevision(key, stage)
	mustProgress(t, err)
	item := &s.Stories[0]
	if item.Approvals == nil {
		item.Approvals = map[string]string{}
	}
	if item.ApprovedBy == nil {
		item.ApprovedBy = map[string]string{}
	}
	item.Approvals[stage] = ref
	item.ApprovedBy[stage] = by
}

func TestAssembledText_GraphApprovedStagesLine(t *testing.T) {
	f, _, base, _, _ := graphPlanFixtureWithForge(t, "agent")
	executeGraphPlan(t, f, base)
	r := &planGraphRuntime{opts: f.opts, req: base, result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue}

	owner := graphPlanState(t, f)
	key := owner.Stories[0].Key
	approveStageBy(t, &owner, key, "story", planbrief.ApprovalByOwner)
	text, err := r.assembledText(t.Context(), owner, "ref")
	mustProgress(t, err)
	if strings.Contains(text, "Approved by the graph") {
		t.Fatalf("owner-only approvals produced a graph line: %s", text)
	}

	graph := graphPlanState(t, f)
	approveStageBy(t, &graph, key, "story", planbrief.ApprovalByOwner)
	approveStageBy(t, &graph, key, "technical", planbrief.ApprovalByGraph)
	text, err = r.assembledText(t.Context(), graph, "ref")
	mustProgress(t, err)
	want := "Approved by the graph, not by the owner: `" + key + "` technical."
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in: %s", want, text)
	}

	approveStageBy(t, &graph, key, "story", planbrief.ApprovalByGraph)
	text, err = r.assembledText(t.Context(), graph, "ref")
	mustProgress(t, err)
	both := "Approved by the graph, not by the owner: `" + key + "` story, `" + key + "` technical."
	if !strings.Contains(text, both) {
		t.Fatalf("missing %q in: %s", both, text)
	}
}
