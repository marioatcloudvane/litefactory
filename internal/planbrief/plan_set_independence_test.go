package planbrief

import (
	"slices"
	"testing"
)

// waitingStory is a drafted story at the given stage with an owner wait open.
func waitingStory(key, stage string, deps ...string) PlanStoryState {
	return PlanStoryState{Key: key, Stage: stage, Candidate: "plan-" + key, Approvals: map[string]string{}, Wait: &PlanWait{ID: "w-" + key, Stage: stage, Candidate: "plan-" + key, Kind: "review", IssueID: 1}, Draft: PlanDraft{Key: key, Dependencies: DependencyContent{StoryKeys: deps, IssueRefs: []string{}}}}
}

// AC-047: a story that depends on another and is still waiting on its own story
// review is not moved or cleared when the other advances to a later stage. Its
// review at the earlier stage is unaffected by that change.
func TestPlanState_InvalidationLeavesDependantsAtEarlierStagesAlone(t *testing.T) {
	s := PlanState{SharedRevision: "initial", Stories: []PlanStoryState{waitingStory("story-001", "story", "story-002"), waitingStory("story-002", "technical"), waitingStory("story-003", "story")}}
	s.Stories[1].Wait = nil
	before := slices.Clone(s.Stories)
	s.Invalidate("story-002", "technical", "")
	for _, i := range []int{0, 2} {
		if s.Stories[i].Stage != "story" || s.Stories[i].Wait == nil || *s.Stories[i].Wait != *before[i].Wait {
			t.Fatalf("%s was moved or lost its wait: %+v", s.Stories[i].Key, s.Stories[i])
		}
	}
	if s.Stories[1].Stage != "technical" {
		t.Fatalf("the changed story left its stage: %+v", s.Stories[1])
	}
}

// A dependant that has reached the invalidated stage is still reopened there, and
// an unknown impact still holds every dependant.
func TestPlanState_InvalidationStillReopensDependantsThatReachedTheStage(t *testing.T) {
	reached := waitingStory("story-001", "acceptance", "story-002")
	reached.Approvals = map[string]string{"story": "a", "technical": "b"}
	early := waitingStory("story-003", "story", "story-002")
	s := PlanState{SharedRevision: "initial", Stories: []PlanStoryState{reached, waitingStory("story-002", "technical"), early}}
	s.Invalidate("story-002", "technical", "")
	if got := s.Stories[0]; got.Stage != "technical" || got.Wait != nil || got.Approvals["story"] != "a" || got.Approvals["technical"] != "" {
		t.Fatalf("a dependant past the stage was not reopened there: %+v", got)
	}
	if s.Stories[2].Wait == nil || s.Stories[2].Stage != "story" {
		t.Fatalf("an earlier dependant was disturbed: %+v", s.Stories[2])
	}
	s.Invalidate("story-002", "technical", "impact unknown")
	if s.Stories[2].Hold != "impact unknown" || s.Stories[2].Wait != nil {
		t.Fatalf("an unknown impact must hold every dependant: %+v", s.Stories[2])
	}
}

func TestPlanState_PromotedStoryReadsTheSeedUntilTheFirstDraft(t *testing.T) {
	set := promotedSet(entry("a", "b"), entry("b"))
	s := PlanState{SharedRevision: "initial", StorySet: set, Stories: []PlanStoryState{{Key: "story-001", Stage: "story", Approvals: map[string]string{}, Draft: PlanDraft{Key: "story-001", Dependencies: DependencyContent{StoryKeys: []string{"story-002"}, IssueRefs: []string{}}}}, {Key: "story-002", Stage: "story", Approvals: map[string]string{}, Draft: PlanDraft{Key: "story-002"}}}}
	got, ok := s.PromotedStory("story-001")
	if !ok || got.Entry.ID != "a" || !slices.Equal(got.Dependencies, []string{"story-002"}) || !slices.Equal(got.Entry.DependsOn, []string{"b"}) {
		t.Fatalf("promoted story %+v ok=%v", got, ok)
	}
	got.Dependencies[0] = "changed"
	if again, _ := s.PromotedStory("story-001"); again.Dependencies[0] != "story-002" {
		t.Fatal("the seed shares its slice with saved state")
	}
	if _, ok = s.PromotedStory("story-009"); ok {
		t.Fatal("an unknown key was reported as promoted")
	}
	s.Stories[0].Candidate = "plan-1-1"
	if _, ok = s.PromotedStory("story-001"); ok {
		t.Fatal("a drafted story was reported as still owed its first draft")
	}
	s.Stories[1].Candidate = "x"
	s.StorySet.Promoted = false
	s.Stories[1].Candidate = ""
	if _, ok = s.PromotedStory("story-002"); ok {
		t.Fatal("an unpromoted set seeded a story")
	}
}
