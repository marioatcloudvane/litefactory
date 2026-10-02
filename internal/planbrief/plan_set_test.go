package planbrief

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func promotedSet(entries ...PlanStorySetEntry) *PlanStorySetState {
	keys := map[string]string{}
	for i, e := range entries {
		keys[e.ID] = fmt.Sprintf("story-%03d", i+1)
	}
	return &PlanStorySetState{Revision: "set-1", Entries: entries, Keys: keys, Promoted: true, PromotedRevision: "set-1"}
}

func entry(id string, deps ...string) PlanStorySetEntry {
	return PlanStorySetEntry{ID: id, Name: "Name " + id, Intent: "Intent " + id, DependsOn: deps}
}

// draftAll drafts stories one pass at a time, as the creation cycle does, and
// returns the temporary ids in the order they were picked.
func draftAll(t *testing.T, s *PlanStorySetState) []string {
	t.Helper()
	drafted := map[string]bool{}
	var order []string
	for range len(s.Entries) + 1 {
		e, key, ok, err := s.NextUndrafted(func(k string) bool { return drafted[k] })
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return order
		}
		if key != s.Keys[e.ID] {
			t.Fatalf("picked %s with key %s, reserved %s", e.ID, key, s.Keys[e.ID])
		}
		drafted[key] = true
		order = append(order, e.ID)
	}
	t.Fatal("selection never ended")
	return nil
}

func TestPlanStorySet_SelectionIsStableTopologicalInDisplayedOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []PlanStorySetEntry
		want    []string
	}{
		{"independent stories keep displayed order", []PlanStorySetEntry{entry("c"), entry("a"), entry("b")}, []string{"c", "a", "b"}},
		{"a dependency listed later is drafted first", []PlanStorySetEntry{entry("a", "c"), entry("b"), entry("c")}, []string{"b", "c", "a"}},
		{"ties among ready stories follow displayed order", []PlanStorySetEntry{entry("s1", "s4"), entry("s2"), entry("s3", "s2"), entry("s4")}, []string{"s2", "s3", "s4", "s1"}},
		{"chain", []PlanStorySetEntry{entry("s3", "s2"), entry("s2", "s1"), entry("s1")}, []string{"s1", "s2", "s3"}},
		{"diamond", []PlanStorySetEntry{entry("d", "b", "c"), entry("b", "a"), entry("c", "a"), entry("a")}, []string{"a", "b", "c", "d"}},
		{"single", []PlanStorySetEntry{entry("only")}, []string{"only"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := promotedSet(tt.entries...)
			if got := draftAll(t, s); !slices.Equal(got, tt.want) {
				t.Fatalf("draft order %v, want %v", got, tt.want)
			}
			order, err := s.DraftOrder()
			if err != nil || !slices.Equal(order, tt.want) {
				t.Fatalf("DraftOrder %v, %v; want %v", order, err, tt.want)
			}
		})
	}
}

// Key reservation follows displayed order however the drafts are ordered.
func TestPlanStorySet_KeysStayInDisplayedOrderWhileDraftsFollowDependencies(t *testing.T) {
	t.Parallel()
	s := promotedSet(entry("a", "c"), entry("b"), entry("c"))
	e, key, ok, err := s.NextUndrafted(func(string) bool { return false })
	if err != nil || !ok || e.ID != "b" || key != "story-002" {
		t.Fatalf("first draft %s %s %v %v, want b under story-002", e.ID, key, ok, err)
	}
}

func TestPlanStorySet_PredecessorApprovalIsNotRequired(t *testing.T) {
	t.Parallel()
	// A draft is all a dependency needs; the predicate knows nothing of approval.
	s := promotedSet(entry("a"), entry("b", "a"))
	e, _, ok, err := s.NextUndrafted(func(key string) bool { return key == "story-001" })
	if err != nil || !ok || e.ID != "b" {
		t.Fatalf("dependant after a drafted (unapproved) dependency: %s %v %v", e.ID, ok, err)
	}
}

func TestPlanStorySet_EndsOnlyWhenEveryStoryHasADraft(t *testing.T) {
	t.Parallel()
	s := promotedSet(entry("a"), entry("b"), entry("c"))
	_, _, ok, err := s.NextUndrafted(func(key string) bool { return key != "story-003" })
	if err != nil || !ok {
		t.Fatalf("one story still owed a draft: ok=%v err=%v", ok, err)
	}
	_, _, ok, err = s.NextUndrafted(func(string) bool { return true })
	if err != nil || ok {
		t.Fatalf("every story drafted: ok=%v err=%v", ok, err)
	}
}

func TestPlanStorySet_NothingToPickBeforePromotion(t *testing.T) {
	t.Parallel()
	all := func(string) bool { return false }
	var absent *PlanStorySetState
	if _, _, ok, err := absent.NextUndrafted(all); ok || err != nil {
		t.Fatalf("absent set: %v %v", ok, err)
	}
	proposed := promotedSet(entry("a"))
	proposed.Promoted = false
	if _, _, ok, err := proposed.NextUndrafted(all); ok || err != nil {
		t.Fatalf("unpromoted set: %v %v", ok, err)
	}
}

func TestPlanStorySet_RefusesASetItCannotOrder(t *testing.T) {
	t.Parallel()
	all := func(string) bool { return false }
	cycle := promotedSet(entry("a", "b"), entry("b", "a"))
	if _, _, _, err := cycle.NextUndrafted(all); !errors.Is(err, ErrPlanStorySetUnorderable) {
		t.Fatalf("cycle: %v", err)
	}
	if _, err := cycle.DraftOrder(); !errors.Is(err, ErrPlanStorySetUnorderable) {
		t.Fatalf("cycle order: %v", err)
	}
	unknown := promotedSet(entry("a", "ghost"))
	if _, _, _, err := unknown.NextUndrafted(all); !errors.Is(err, ErrPlanStorySetUnorderable) {
		t.Fatalf("unknown reference: %v", err)
	}
	unkeyed := promotedSet(entry("a"))
	delete(unkeyed.Keys, "a")
	if _, _, _, err := unkeyed.NextUndrafted(all); err == nil || !strings.Contains(err.Error(), "no reserved key") {
		t.Fatalf("unreserved key: %v", err)
	}
}

func TestPlanStorySet_TwelveStoriesAreAllSelected(t *testing.T) {
	t.Parallel()
	var entries []PlanStorySetEntry
	for i := range MaxPlanStorySetEntries {
		if i == 0 {
			entries = append(entries, entry("s1", fmt.Sprintf("s%d", MaxPlanStorySetEntries)))
			continue
		}
		entries = append(entries, entry(fmt.Sprintf("s%d", i+1)))
	}
	got := draftAll(t, promotedSet(entries...))
	if len(got) != MaxPlanStorySetEntries || got[len(got)-1] != "s1" {
		t.Fatalf("picked %v", got)
	}
}

func TestPlanState_NextSetStoryReadsDraftsFromSavedStories(t *testing.T) {
	t.Parallel()
	s := PlanState{
		SharedRevision: "initial",
		StorySet:       promotedSet(entry("a"), entry("b", "a")),
		Stories: []PlanStoryState{
			{Key: "story-001", Stage: "story"},
			{Key: "story-002", Stage: "story"},
		},
	}
	e, key, ok, err := s.NextSetStory()
	if err != nil || !ok || e.ID != "a" || key != "story-001" {
		t.Fatalf("first pick %s %s %v %v", e.ID, key, ok, err)
	}
	s.Stories[0].Candidate = "plan-1-1"
	e, key, ok, err = s.NextSetStory()
	if err != nil || !ok || e.ID != "b" || key != "story-002" {
		t.Fatalf("second pick %s %s %v %v", e.ID, key, ok, err)
	}
	s.Stories[1].Candidate = "plan-1-2"
	if _, _, ok, err = s.NextSetStory(); err != nil || ok {
		t.Fatalf("all drafted: %v %v", ok, err)
	}
}

// A Plan saved before story sets existed loads unchanged, and a Plan without a
// set saves without the new member, so older readers see the bytes they knew.
func TestPlanState_SavedStateWithoutASetStaysCompatible(t *testing.T) {
	t.Parallel()
	const saved = `{"shared_revision":"shared-3","stories":[{"key":"story-001","stage":"technical","candidate":"plan-4-9","draft":{"key":"story-001"},"approvals":{},"retired":false}],"consumed_replies":[41]}`
	var s PlanState
	if err := json.Unmarshal([]byte(saved), &s); err != nil {
		t.Fatal(err)
	}
	if s.StorySet != nil || s.SharedRevision != "shared-3" {
		t.Fatalf("saved state misread: %+v", s)
	}
	if _, _, ok, err := s.NextSetStory(); ok || err != nil {
		t.Fatalf("a Plan with no set has a set pick: %v %v", ok, err)
	}
	raw, err := json.Marshal(s)
	if err != nil || strings.Contains(string(raw), "story_set") {
		t.Fatalf("a Plan with no set saved a set member: %s %v", raw, err)
	}
	s.StorySet = promotedSet(entry("a", "b"), entry("b"))
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back PlanState
	if err = json.Unmarshal(raw, &back); err != nil || back.StorySet == nil || back.StorySet.Keys["b"] != "story-002" || !back.StorySet.Promoted || back.StorySet.Entries[0].DependsOn[0] != "b" {
		t.Fatalf("set did not round trip: %+v %v", back.StorySet, err)
	}
}

func TestPlanStorySet_CloneSharesNothing(t *testing.T) {
	t.Parallel()
	var nothing *PlanStorySetState
	if nothing.Clone() != nil {
		t.Fatal("nil set cloned to a value")
	}
	s := promotedSet(entry("a"), entry("b", "a"))
	c := s.Clone()
	c.Entries[1].DependsOn[0] = "x"
	c.Keys["a"] = "changed"
	if s.Entries[1].DependsOn[0] != "a" || s.Keys["a"] != "story-001" {
		t.Fatal("clone shares state with the original")
	}
}
