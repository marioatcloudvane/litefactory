package daemon_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestFrozenScopeOverridesLabelsExceptExplicitParentPlan(t *testing.T) {
	for _, tc := range []struct {
		body string
		want agent.Mode
	}{{"continue", agent.ModeImplement}, {"replan", agent.ModePlan}, {"/plan revise unfinished scope", agent.ModePlan}} {
		t.Run(tc.body, func(t *testing.T) {
			f := managedRefinementFixture(t)
			parent := f.sessionOf(testIssue)
			if err := f.db.CompareManagedFeature(t.Context(), store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "implementing"}, "planning"); err != nil {
				t.Fatal(err)
			}
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Plan")})
			f.forge.addComment(testIssue, 80, owner(), tc.body)
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Mode != tc.want {
				t.Fatalf("durable mode selection: %+v\n%s", calls, f.logs)
			}
		})
	}
}
func TestCompletedStoryStaysLockedWhileParentPlanning(t *testing.T) {
	f := managedRefinementFixture(t)
	parent := f.sessionOf(testIssue)
	story := store.ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 200, IssueID: 2000, State: "completed"}
	if err := f.db.CompareManagedStory(t.Context(), story, "planning"); err != nil {
		t.Fatal(err)
	}
	f.forge.addComment(200, 60, owner(), "replan")
	f.settle()
	if len(f.turns.calls()) != 0 {
		t.Fatal("completed child launched planning")
	}
	bodies, _, _ := f.forge.snapshot()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "successor") {
		t.Fatalf("completed refusal missing: %v", bodies)
	}
	f.settle()
	bodies, _, _ = f.forge.snapshot()
	if len(bodies) != 1 {
		t.Fatalf("late comment reported twice: %v", bodies)
	}
}
