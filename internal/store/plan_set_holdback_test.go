package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

// Story-003 T-022/T-023 gap tests: the story set survives real SQLite, and Plan
// route admission is frozen per session (AC-012).

// A story set proposed, promoted and saved is what a later turn, or a restart,
// reads back: revisions, reserved keys, the promoted revision and the stories.
func TestPlanWorkflowStore_StorySetSurvivesSaveAndRestartReads(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	session := testSession(t, db, 377)
	entries := []planbrief.PlanStorySetEntry{
		{ID: "s1", Name: "Storage", Intent: "Keep the data", DependsOn: []string{"s2"}},
		{ID: "s2", Name: "Schema", Intent: "Define the tables"},
	}
	state := planbrief.PlanState{SharedRevision: "initial"}
	revision, _, err := state.ProposeStorySet(entries)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	proposed, rev, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil || rev != saved || proposed.StorySet == nil || proposed.StorySet.Revision != revision || proposed.StorySet.Promoted || len(proposed.Stories) != 0 {
		t.Fatalf("proposed set read back as %+v rev %d err %v", proposed.StorySet, rev, err)
	}
	if _, err = proposed.PromoteStorySet(revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, rev, proposed); err != nil {
		t.Fatal(err)
	}
	again, _, err := db.GetPlanWorkflowState(ctx, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string]string{"s1": "story-001", "s2": "story-002"}
	if !again.StorySet.Promoted || again.StorySet.PromotedRevision != revision || !reflect.DeepEqual(again.StorySet.Keys, wantKeys) || len(again.Stories) != 2 {
		t.Fatalf("promoted set read back as %+v with %d stories", again.StorySet, len(again.Stories))
	}
	if !reflect.DeepEqual(again.Stories[0].Draft.Dependencies.StoryKeys, []string{"story-002"}) {
		t.Fatalf("translated dependencies were lost: %+v", again.Stories[0].Draft.Dependencies)
	}
	// A read state repeats the promotion for the same revision as a no-op.
	if changed, err := again.PromoteStorySet(revision); err != nil || changed {
		t.Fatalf("repeat after a read: %v %v", changed, err)
	}
	// A stale writer cannot overwrite the promoted state.
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, rev, proposed); err == nil {
		t.Fatal("a save at a stale revision succeeded")
	}
}

// New sessions select the graph Plan route through the factory's admission seam;
// the choice is frozen on first admission, and a session already on legacy Plan
// stays there when the default is graph.
func TestAdmitPlanPath_NewSessionsGetGraphAndRoutesAreFrozen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)

	fresh := testSession(t, db, 501)
	if _, err := db.PlanExecutionPath(ctx, fresh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a session had a route before admission: %v", err)
	}
	for i := range 2 {
		if route, err := db.AdmitPlanPath(ctx, fresh.ID); err != nil || route != "graph" {
			t.Fatalf("admission %d selected %q, %v; new sessions must get the graph route", i, route, err)
		}
	}
	if route, err := db.AdmitPlanWorkflowPath(ctx, fresh.ID, "legacy"); err != nil || route != "graph" {
		t.Fatalf("a later legacy selection moved a graph session to %q, %v", route, err)
	}

	existing := testSession(t, db, 502)
	if route, err := db.AdmitPlanWorkflowPath(ctx, existing.ID, "legacy"); err != nil || route != "legacy" {
		t.Fatalf("setup: %q %v", route, err)
	}
	if route, err := db.AdmitPlanPath(ctx, existing.ID); err != nil || route != "legacy" {
		t.Fatalf("a session already on legacy Plan was moved to %q, %v", route, err)
	}
	if route, err := db.PlanExecutionPath(ctx, existing.ID); err != nil || route != "legacy" {
		t.Fatalf("persisted route %q, %v", route, err)
	}

	for _, bad := range []string{"", "Graph", "set", "legacy graph"} {
		if _, err := db.AdmitPlanWorkflowPath(ctx, fresh.ID, bad); err == nil {
			t.Errorf("route %q was admitted", bad)
		}
	}
	if _, err := db.AdmitPlanPath(ctx, 0); err == nil {
		t.Error("a missing session was admitted")
	}
}
