package turn

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

func pickEntry(id string, deps ...string) planbrief.PlanStorySetEntry {
	return planbrief.PlanStorySetEntry{ID: id, Name: "Name " + id, Intent: "Intent " + id, DependsOn: deps}
}

// setState is a promoted set of three stories, reserved in displayed order, in
// which story-001 depends on the story displayed last.
func setState() planbrief.PlanState {
	return planbrief.PlanState{
		SharedRevision: "initial",
		StorySet: &planbrief.PlanStorySetState{
			Revision: "set-1", PromotedRevision: "set-1", Promoted: true,
			Entries: []planbrief.PlanStorySetEntry{pickEntry("a", "c"), pickEntry("b"), pickEntry("c")},
			Keys:    map[string]string{"a": "story-001", "b": "story-002", "c": "story-003"},
		},
		Stories: []planbrief.PlanStoryState{
			{Key: "story-001", Stage: "story", Approvals: map[string]string{}},
			{Key: "story-002", Stage: "story", Approvals: map[string]string{}},
			{Key: "story-003", Stage: "story", Approvals: map[string]string{}},
		},
	}
}

func TestPlanPick_ScopeWithoutStoriesOrSetStartsAtTheSetStage(t *testing.T) {
	t.Parallel()
	got, err := pickPlanStep(planbrief.PlanState{SharedRevision: "initial"}, "", nil)
	if err != nil || got != (planPick{Stage: "set"}) {
		t.Fatalf("pick %+v %v", got, err)
	}
}

// An unpromoted set is proposed again on the owner's reply, which is how a
// changed set gets a new revision; a promoted one is never proposed again.
func TestPlanPick_UnpromotedSetIsProposedAgainAndPromotedSetNever(t *testing.T) {
	t.Parallel()
	s := planbrief.PlanState{SharedRevision: "initial", StorySet: setState().StorySet}
	s.StorySet.Promoted = false
	got, err := pickPlanStep(s, "", nil)
	if err != nil || got != (planPick{Stage: "set"}) {
		t.Fatalf("an unpromoted set picked %+v %v", got, err)
	}
	s.StorySet.Promoted = true
	s.StorySet.Keys = nil
	if got, err = pickPlanStep(s, "", nil); err == nil && got.Stage == "set" {
		t.Fatalf("a promoted set was proposed again: %+v", got)
	}
}

func TestPlanPick_PromotedStoriesAreDraftedDependenciesFirst(t *testing.T) {
	t.Parallel()
	s := setState()
	var order []string
	for range 4 {
		got, err := pickPlanStep(s, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Create {
			break
		}
		if got.Stage != "story" {
			t.Fatalf("creation pick at stage %q", got.Stage)
		}
		order = append(order, got.Key)
		for i := range s.Stories {
			if s.Stories[i].Key == got.Key {
				s.Stories[i].Candidate = "plan-1-" + got.Key
			}
		}
	}
	// story-001 depends on story-003, so it is drafted last, though it is displayed first.
	want := []string{"story-002", "story-003", "story-001"}
	if len(order) != len(want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order %v, want %v", order, want)
		}
	}
}

func TestPlanPick_DraftedStoriesFallBackToOrdinaryEligibility(t *testing.T) {
	t.Parallel()
	s := setState()
	for i := range s.Stories {
		s.Stories[i].Candidate = "plan-1-1"
	}
	got, err := pickPlanStep(s, "", nil)
	if err != nil || got.Create {
		t.Fatalf("creation pick after every draft: %+v %v", got, err)
	}
}

func TestPlanPick_UnorderableSetIsAnError(t *testing.T) {
	t.Parallel()
	s := setState()
	s.StorySet.Entries = []planbrief.PlanStorySetEntry{pickEntry("a", "b"), pickEntry("b", "a"), pickEntry("c")}
	s.StorySet.Keys = map[string]string{"a": "story-001", "b": "story-002", "c": "story-003"}
	s.Stories[2].Candidate = "plan-1-3"
	if _, err := pickPlanStep(s, "", nil); !errors.Is(err, planbrief.ErrPlanStorySetUnorderable) {
		t.Fatalf("cycle: %v", err)
	}
}

func TestPlanRuntime_SetStageControls(t *testing.T) {
	t.Parallel()
	r := planGraphRuntime{stage: "set"}
	for condition, want := range map[string]string{graphexec.PlanSetCondition: "true", "story": "false", "technical": "false", "approved": "false"} {
		got, err := r.control(context.Background(), Options{}, ActivityRequest{Attrs: map[string]string{"lf_condition": condition}})
		if err != nil || got.Outcome != want {
			t.Fatalf("condition %q = %q, %v; want %q", condition, got.Outcome, err, want)
		}
	}
}

// Each creation pass selects freshly: the pick made at admission never decides
// the next pass, because drafts made in between change which stories are owed.
func TestPlanRuntime_CreationPassSelectsFreshlyEachTime(t *testing.T) {
	f, _, _, _ := graphPlanFixture(t, "agent")
	state := setState()
	_, current, err := f.db.GetPlanWorkflowState(t.Context(), f.ref().String())
	mustProgress(t, err)
	revision, err := f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), current, state)
	mustProgress(t, err)
	r := planGraphRuntime{opts: f.opts, req: f.request(), result: Result{Ref: f.ref()}, parentRef: f.ref(), parentNumber: f.ref().Issue}
	pass := func() (string, string) {
		t.Helper()
		got, err := r.control(t.Context(), Options{}, ActivityRequest{Attrs: map[string]string{"lf_condition": graphexec.PlanCreateCondition}})
		mustProgress(t, err)
		return got.Outcome, r.selected
	}
	for i, want := range []struct{ outcome, key string }{{"true", "story-002"}, {"true", "story-003"}, {"true", "story-001"}} {
		outcome, key := pass()
		if outcome != want.outcome || key != want.key {
			t.Fatalf("pass %d picked %s %s, want %s %s", i+1, outcome, key, want.outcome, want.key)
		}
		state.Stories[map[string]int{"story-001": 0, "story-002": 1, "story-003": 2}[key]].Candidate = "plan-1-" + key
		revision, err = f.db.SavePlanWorkflowState(t.Context(), f.ref().String(), revision, state)
		mustProgress(t, err)
	}
	if outcome, _ := pass(); outcome != "false" {
		t.Fatalf("a pass with every promoted story drafted continued: %q", outcome)
	}
}
