package daemon

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type timeoutSettlementExecutor struct{ settled int }

func (*timeoutSettlementExecutor) Execute(context.Context, turn.Request) (turn.Result, error) {
	panic("timeout recovery invoked a worker")
}
func (e *timeoutSettlementExecutor) RecoverImplementTimeout(context.Context, store.ImplementStory) error {
	e.settled++
	return nil
}

func TestImplementTimeoutRecoveryCancelsOrphanedRunningGraph(t *testing.T) {
	f := newImplementDaemonFixture(t)
	db := f.d.db
	child, err := db.GetOrCreateSession(t.Context(), f.repo, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SetImplementWorkerSession(t.Context(), implementDaemonParent, 1, "story-001", child.ID, implementDaemonClock); err != nil {
		t.Fatal(err)
	}
	pin, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: testDigest("a")})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: child.ID, DefinitionID: pin.ID, CurrentNode: "budget", StartedAt: implementDaemonClock})
	if err != nil {
		t.Fatal(err)
	}
	row, err := db.HoldImplementStoryTimeout(t.Context(), child.ID, 11, "expired parent deadline", implementDaemonClock)
	if err != nil {
		t.Fatal(err)
	}
	executor := &timeoutSettlementExecutor{}
	f.d.turn = executor
	f.d.instanceID = "timeout-recovery"
	task := implementWorkerTask(t, f, child.ID)
	// Durable session ownership blocks cancellation while a process still runs.
	if err = db.SetSessionState(t.Context(), child.ID, store.SessionRunning); err != nil {
		t.Fatal(err)
	}
	if err = f.d.recoverImplementTimeoutForTask(t.Context(), task); err == nil {
		t.Fatal("live session was not deferred")
	}
	if executor.settled != 0 {
		t.Fatal("live process notice settled early")
	}
	if err = db.SetSessionState(t.Context(), child.ID, store.SessionIdle); err != nil {
		t.Fatal(err)
	}
	if err = f.d.recoverImplementTimeoutForTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	settled, err := db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != store.GraphCancelled || !settled.NextDueAt.IsZero() || executor.settled != 1 {
		t.Fatalf("orphan timeout not settled: %+v calls=%d", settled, executor.settled)
	}
	held, err := db.GetImplementStory(t.Context(), row.Parent, row.Generation, row.Key)
	if err != nil {
		t.Fatal(err)
	}
	if held.TimeoutAttemptID != row.TimeoutAttemptID || held.Attempts != 0 {
		t.Fatalf("recovery lost timeout authority: %+v", held)
	}
	acquired, err := db.AcquireSessionLock(t.Context(), child.ID, "next-recovery")
	if err != nil || !acquired {
		t.Fatalf("recovery retained session lock: %v %v", acquired, err)
	}
	if err = db.ReleaseSessionLock(t.Context(), child.ID, "next-recovery"); err != nil {
		t.Fatal(err)
	}
}
