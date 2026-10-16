package daemon_test

import (
	"strings"
	"testing"
)

func TestJudgeClosedManagedChildBypassesParentRefinement(t *testing.T) {
	f := managedRefinementFixture(t)
	f.forge.addComment(200, 61, owner(), "/judge")
	f.settle()
	if calls := f.turns.calls(); len(calls) != 0 {
		t.Fatalf("advisory became workflow input: %+v", calls)
	}
	parent := f.sessionOf(testIssue)
	pending, err := f.db.ListPendingStoryRefinements(t.Context(), parent.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("advisory became parent refinement: %+v %v", pending, err)
	}
	posts, _, _ := f.forge.snapshot()
	f.forge.mu.Lock()
	destinations := append([]int(nil), f.forge.postedIssues...)
	f.forge.mu.Unlock()
	found := false
	for i, p := range posts {
		if destinations[i] == 200 && strings.Contains(p, "Unable to assess saved work") {
			found = true
		}
	}
	if !found {
		t.Fatalf("child advisory explanation was not delivered: %+v", posts)
	}
	cursor, err := f.db.StoryDiscoveryCursor(t.Context(), parent.ID, 200)
	if err != nil || cursor < 61 {
		t.Fatalf("advisory did not advance parent discovery only: %d %v", cursor, err)
	}
}
