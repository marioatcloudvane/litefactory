package planbrief

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The set lists a dependant before the story it depends on, so a key reserved in
// displayed order names a later-listed entry.
var promoteEntries = []PlanStorySetEntry{
	{ID: "s1", Name: "Storage", Intent: "Do storage", DependsOn: []string{"s2"}},
	{ID: "s2", Name: "Schema", Intent: "Do schema"},
	{ID: "s3", Name: "Screens", Intent: "Do screens", DependsOn: []string{"s1", "s2"}},
}

func proposedState(t *testing.T) (PlanState, string) {
	t.Helper()
	s := PlanState{SharedRevision: "initial"}
	rev, _, err := s.ProposeStorySet(promoteEntries)
	if err != nil {
		t.Fatal(err)
	}
	return s, rev
}

func TestPromoteStorySet_ReservesKeysInDisplayedOrderWithTranslatedDependencies(t *testing.T) {
	t.Parallel()
	s, rev := proposedState(t)
	changed, err := s.PromoteStorySet(rev)
	if err != nil || !changed {
		t.Fatalf("promote: %v %v", changed, err)
	}
	set := s.StorySet
	if !set.Promoted || set.PromotedRevision != rev || !reflect.DeepEqual(set.Keys, map[string]string{"s1": "story-001", "s2": "story-002", "s3": "story-003"}) {
		t.Fatalf("set after promotion: %+v", set)
	}
	if len(s.Stories) != 3 {
		t.Fatalf("stories %d", len(s.Stories))
	}
	want := [][]string{{"story-002"}, {}, {"story-001", "story-002"}}
	for i, st := range s.Stories {
		if st.Key != set.Keys[set.Entries[i].ID] || st.Stage != "story" || st.Candidate != "" || st.Draft.Key != st.Key || st.Approvals == nil {
			t.Fatalf("story %d %+v", i, st)
		}
		if !reflect.DeepEqual(st.Draft.Dependencies.StoryKeys, want[i]) || st.Draft.Dependencies.IssueRefs == nil {
			t.Fatalf("story %s dependencies %+v, want %v", st.Key, st.Draft.Dependencies, want[i])
		}
	}
	// Drafting picks dependencies first without renumbering anything.
	_, key, ok, err := s.NextSetStory()
	if err != nil || !ok || key != "story-002" {
		t.Fatalf("first draft is %q %v %v, want story-002", key, ok, err)
	}
	if got := s.NextStoryKey(); got != "story-004" {
		t.Fatalf("allocator continues at %q, want story-004", got)
	}
}

func TestPromoteStorySet_IsIdempotentForThePromotedRevision(t *testing.T) {
	t.Parallel()
	s, rev := proposedState(t)
	if _, err := s.PromoteStorySet(rev); err != nil {
		t.Fatal(err)
	}
	before := s.StorySet.Clone()
	stories := len(s.Stories)
	changed, err := s.PromoteStorySet(rev)
	if err != nil || changed || len(s.Stories) != stories || !reflect.DeepEqual(s.StorySet, before) {
		t.Fatalf("repeat: %v %v stories %d set %+v", changed, err, len(s.Stories), s.StorySet)
	}
}

func TestPromoteStorySet_RefusesStaleUnknownAndUnorderableSets(t *testing.T) {
	t.Parallel()
	s, rev := proposedState(t)
	for name, argument := range map[string]string{"older": strings.Repeat("a", 64), "empty": ""} {
		if changed, err := s.PromoteStorySet(argument); changed || !errors.Is(err, ErrPlanStorySetStaleRevision) || s.StorySet.Promoted || len(s.Stories) != 0 {
			t.Fatalf("%s revision: %v %v", name, changed, err)
		}
	}
	// A changed set has a new revision, and the old revision no longer promotes.
	changedSet := []PlanStorySetEntry{promoteEntries[1]}
	newRev, _, err := s.ProposeStorySet(changedSet)
	if err != nil || newRev == rev {
		t.Fatalf("changed set: %q %v", newRev, err)
	}
	if _, err = s.PromoteStorySet(rev); !errors.Is(err, ErrPlanStorySetStaleRevision) {
		t.Fatalf("an approval for the older revision was accepted: %v", err)
	}
	if _, err = s.PromoteStorySet(newRev); err != nil {
		t.Fatal(err)
	}
	// After promotion the older revision is still refused and nothing moves.
	if _, err = s.PromoteStorySet(rev); !errors.Is(err, ErrPlanStorySetStaleRevision) || len(s.Stories) != 1 {
		t.Fatalf("promoted set accepted the older revision: %v", err)
	}

	none := PlanState{SharedRevision: "initial"}
	if _, err = none.PromoteStorySet(rev); !errors.Is(err, ErrPlanStorySetMissing) {
		t.Fatalf("no set: %v", err)
	}
	cycle := []PlanStorySetEntry{{ID: "a", Name: "A", Intent: "A", DependsOn: []string{"b"}}, {ID: "b", Name: "B", Intent: "B", DependsOn: []string{"a"}}}
	loop := PlanState{SharedRevision: "initial"}
	loopRev, _, err := loop.ProposeStorySet(cycle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loop.PromoteStorySet(loopRev); !errors.Is(err, ErrPlanStorySetUnorderable) || loop.StorySet.Promoted || len(loop.Stories) != 0 {
		t.Fatalf("cycle: %v", err)
	}
	withStory := PlanState{SharedRevision: "initial"}
	revWithStory, _, err := withStory.ProposeStorySet(promoteEntries)
	if err != nil {
		t.Fatal(err)
	}
	withStory.Stories = []PlanStoryState{{Key: "story-001", Stage: "story"}}
	if _, err = withStory.PromoteStorySet(revWithStory); !errors.Is(err, ErrPlanStorySetHasStories) || withStory.StorySet.Promoted {
		t.Fatalf("existing stories: %v", err)
	}
	tampered, tamperedRev := proposedState(t)
	tampered.StorySet.Entries[0].Name = "Changed"
	if _, err = tampered.PromoteStorySet(tamperedRev); !errors.Is(err, ErrPlanStorySetRevisionMismatch) {
		t.Fatalf("tampered set: %v", err)
	}
}
