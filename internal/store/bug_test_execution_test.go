package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestBugTestExecutionResumesRetainedPhasesWithoutRelaunch(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "child-execution.db")
	db, e := store.Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = db.Close() }()
	if e = db.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	registry := graphexec.Registry{Policy: teamsource.ValidateBugTests}
	if e = registry.Register(turn.BugChecksSpec()); e != nil {
		t.Fatal(e)
	}
	definition, e := teamsource.CaptureBugTests(ctx, registry)
	if e != nil {
		t.Fatal(e)
	}
	call := store.BugChildExecutionFixture(t, db, definition.Profile.Digest)
	at := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	resolved := 0
	build := func() *turn.BugTestChild {
		child, e := turn.NewBugTestChild(turn.BugTestChildOptions{DB: db, Profile: definition.Profile, Checks: &turn.BugChecksActivity{}, Resolve: func(context.Context, store.BugTestCall, string) (turn.BugChecksInput, error) {
			resolved++
			return turn.BugChecksInput{}, errors.New("completed phase must not relaunch")
		}})
		if e != nil {
			t.Fatal(e)
		}
		return child
	}
	child := build()
	for n, phase := range []string{"regression", "focused", "full"} {
		// The check completed before the process died, while its graph attempt was
		// still running. Recovery must finish this very attempt, not execute again.
		attempt, e := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: call.ChildInstance, Node: phase, StartedAt: at, Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		store.BugChildRetainedPhaseFixture(t, db, call, phase)
		if n == 1 {
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			db, e = store.Open(ctx, path)
			if e != nil {
				t.Fatal(e)
			}
			child = build()
		}
		due, e := db.ListDueGraphDispatches(ctx, at, 10)
		if e != nil {
			t.Fatal(e)
		}
		var dispatch store.GraphDispatch
		for _, d := range due {
			if d.InstanceID == call.ChildInstance && d.Node == phase {
				dispatch = d
			}
		}
		if dispatch.ID == 0 {
			t.Fatalf("no child dispatch for %s: %+v", phase, due)
		}
		reserved, ok, e := db.ReserveGraphDispatch(ctx, dispatch.ID, at)
		if e != nil || !ok {
			t.Fatal(ok, e)
		}
		if e = child.Advance(ctx, reserved, at); e != nil {
			t.Fatal(e)
		}
		if e = child.Advance(ctx, reserved, at); e != nil {
			t.Fatal("duplicate dispatch", e)
		}
		completed, e := db.GetGraphNodeAttempt(ctx, attempt.ID)
		if e != nil || completed.Status != store.GraphSucceeded || completed.Outcome != "passed" {
			t.Fatalf("phase %s lost original attempt: %+v %v", phase, completed, e)
		}
		got, e := db.GetBugTestCall(ctx, call.ID)
		if e != nil {
			t.Fatal(e)
		}
		want := "pending"
		if n == 2 {
			want = "passed"
		}
		if got.Outcome != want {
			t.Fatalf("phase %s outcome %s want %s", phase, got.Outcome, want)
		}
	}
	if resolved != 0 {
		t.Fatalf("recovery requested %d fresh check executions", resolved)
	}
	parent, e := db.GetGraphNodeAttempt(ctx, call.Request.ParentAttempt)
	if e != nil || parent.Status != store.GraphRunning {
		t.Fatal("child completed parent prematurely", parent, e)
	}
	due, e := db.ListDueGraphDispatches(ctx, at, 10)
	if e != nil {
		t.Fatal(e)
	}
	wakeups := 0
	for _, d := range due {
		if d.InstanceID == call.Request.ParentInstance {
			wakeups++
		}
	}
	if wakeups != 1 {
		t.Fatalf("parent wakeups=%d want 1", wakeups)
	}
	if e = db.CompleteGraphNodeAttempt(ctx, parent.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "passed", FinishedAt: at, Transition: &store.GraphTransition{InstanceID: call.Request.ParentInstance, FromNode: "test", ToNode: "after", OnOutcome: "passed", At: at}}); e != nil {
		t.Fatal(e)
	}
	got, e := db.GetBugTestCall(ctx, call.ID)
	if e != nil || !got.Consumed {
		t.Fatal("parent failed consumption", got, e)
	}
}
