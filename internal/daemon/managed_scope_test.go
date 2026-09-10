package daemon_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/daemon"
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
			label := "Mode: Implement"
			if tc.want == agent.ModePlan {
				label = "Mode: Plan"
			}
			assertRetentionLabels(t, f, label)
			if calls[0].ToolProfile != string(tc.want) {
				t.Fatalf("effective permissions: %+v", calls[0])
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

// A managed feature out of planning pins Implement for plain comments. That pin
// used to sit above the explicit-command case, which made Validate unreachable
// on exactly the features that have engineering to validate: the owner's
// /validate was rewritten to Implement and the Mode: Validate label was replaced.
func TestManagedValidateCommandOutranksImplementPin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		want    agent.Mode
		profile string
	}{
		{"explicit command transitions", "/validate review the engineering evidence", agent.ModeValidate, "validate"},
		{"prose asking for validation does not", "now validate the results!", agent.ModeImplement, "implement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.daemon = f.newDaemon(daemon.Options{Forge: refinementForge{Client: f.client, parent: testIssue}})
			f.plantCursor()
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger()})
			f.settle()
			s := f.sessionOf(testIssue)
			feature := store.ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}
			if err := f.db.PutManagedFeature(t.Context(), feature); err != nil {
				t.Fatal(err)
			}
			feature.State = "implementing"
			if err := f.db.CompareManagedFeature(t.Context(), feature, "planning"); err != nil {
				t.Fatal(err)
			}
			f.forge.addIssue(fakeIssue{ID: 1230, Number: testIssue, Title: "Parent feature", User: stranger(), Labels: toFakeLabels("Mode: Implement")})
			f.forge.addComment(testIssue, 80, owner(), tc.body)
			f.settle()
			calls := f.turns.calls()
			if len(calls) != 1 || calls[0].Mode != tc.want {
				t.Fatalf("mode selection: %+v\n%s", calls, f.logs)
			}
			if calls[0].ToolProfile != tc.profile {
				t.Fatalf("effective permissions: %+v", calls[0])
			}
			assertRetentionLabels(t, f, tc.want.Label())
		})
	}
}
