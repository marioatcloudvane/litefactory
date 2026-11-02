package daemon

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// staleDispatchOnFinishedInstance reproduces #1299: a Plan instance parked at
// parallel_group, a worker reserved the owed dispatch, and the instance then
// finished on another path, leaving the dispatch reserved on a succeeded
// instance.
func staleDispatchOnFinishedInstance(t *testing.T) (*researchRecoveryFixture, store.GraphDispatch) {
	t.Helper()
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.FinishGraphNodeAttempt(ctx, f.attempt.ID, store.GraphSucceeded, "success", f.at))
	_, err := f.db.FinishRun(ctx, f.run.ID, store.RunOutcome{Status: store.RunSucceeded})
	researchRecoveryCheck(t, err)
	definition, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan.dot", Digest: strings.Repeat("d", 64), CreatedAt: f.at})
	researchRecoveryCheck(t, err)
	f.instance, err = f.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: f.session.ID, DefinitionID: definition.ID, CurrentNode: "parallel_group", StartedAt: f.at, NextDueAt: f.at})
	researchRecoveryCheck(t, err)
	parked, err := f.db.ParkGraphInstance(ctx, f.instance.ID, "parallel_group", f.at, f.at)
	researchRecoveryCheck(t, err)
	reserved, ok, err := f.db.ReserveGraphDispatch(ctx, parked.ID, f.at)
	if err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	researchRecoveryCheck(t, f.db.SetGraphInstanceStatus(ctx, f.instance.ID, store.GraphSucceeded, time.Time{}, f.at.Add(time.Minute)))
	return f, reserved
}

func assertStaleDispatchCancelled(t *testing.T, f *researchRecoveryFixture, id int64) {
	t.Helper()
	got, err := f.db.LatestGraphDispatch(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if got.ID != id || got.Status != "cancelled" || !strings.HasPrefix(got.CancellationReason, "stale") {
		t.Fatalf("dispatch=%+v, want it cancelled as stale", got)
	}
	instance, err := f.db.GetGraphInstance(t.Context(), f.instance.ID)
	researchRecoveryCheck(t, err)
	if instance.Status != store.GraphSucceeded || !instance.NextDueAt.IsZero() {
		t.Fatalf("finished instance was changed: %+v", instance)
	}
}

// Running the owed stage of a finished instance must drop it without failing
// the worker, so the task's queue goes on to the next owner comment.
func TestRunGraphStepDropsADispatchOnAFinishedInstance(t *testing.T) {
	f, reserved := staleDispatchOnFinishedInstance(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{db: f.db, instanceID: "stale", log: log}
	worker := &workerState{}
	step := &task{session: f.session, log: log, tickAt: f.at.Add(2 * time.Minute), worker: worker}
	d.runGraphStep(t.Context(), step, reserved)
	if worker.status != workerCompleted {
		t.Fatalf("worker status=%s, want completed", worker.status)
	}
	assertStaleDispatchCancelled(t, f, reserved.ID)
}

// A dead row left by the old code (dispatch 1396) clears on the next tick's
// reconciliation, with no DB edit.
func TestReconcileCancelsADeadDispatchOnAFinishedInstance(t *testing.T) {
	f, reserved := staleDispatchOnFinishedInstance(t)
	d := &Daemon{db: f.db, instanceID: "stale", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	researchRecoveryCheck(t, d.reconcileGraphDispatches(t.Context(), f.at.Add(2*time.Minute)))
	assertStaleDispatchCancelled(t, f, reserved.ID)
	dead, err := f.db.ListDeadGraphDispatches(t.Context())
	researchRecoveryCheck(t, err)
	if len(dead) != 0 {
		t.Fatalf("dead dispatches remain: %+v", dead)
	}
}
