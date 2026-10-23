package store

import (
	"errors"
	"testing"
	"time"
)

func TestGraphCapacityYieldRetainsAttemptAndLimit(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	attempt := graphTestAttempt(t, db, instance, 1)
	at := graphTestTime().Add(time.Second)
	if _, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("running attempt reclaimed: %v", err)
	}
	if err := db.DeferGraphNodeAttempt(t.Context(), attempt.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := db.DeferGraphNodeAttempt(t.Context(), attempt.ID, at); err == nil {
		t.Fatal("duplicate yield accepted")
	}
	if _, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, "other", at); err == nil {
		t.Fatal("foreign node resumed")
	}
	if count, err := db.ReconcileWorkflowGraphs(t.Context(), "implement", at); err != nil || count != 0 {
		t.Fatalf("yield reconciliation count=%d err=%v", count, err)
	}
	resumed, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode, at)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != attempt.ID || resumed.Attempt != 1 || resumed.Status != GraphRunning {
		t.Fatalf("resumed=%+v", resumed)
	}
	attempts, err := db.ListGraphNodeAttempts(t.Context(), instance.ID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("attempts=%+v err=%v", attempts, err)
	}
	if _, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: at, Limit: 1}); !errors.Is(err, ErrGraphAttemptRunning) {
		t.Fatalf("concurrent start=%v", err)
	}
	if err := db.FinishGraphNodeAttempt(t.Context(), attempt.ID, GraphSucceeded, "done", at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("completed attempt reclaimed: %v", err)
	}
	if _, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: instance.CurrentNode, StartedAt: at, Limit: 1}); !errors.Is(err, ErrGraphAttemptLimit) {
		t.Fatalf("real attempt limit lost: %v", err)
	}
}

func TestGraphCapacityDispatchWaitDoesNotSpendStartRetries(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	at := graphTestTime().Add(time.Second)
	d, err := db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	for range MaxGraphDispatchAttempts + 2 {
		reserved, ok, err := db.ReserveGraphDispatch(t.Context(), d.ID, at)
		if err != nil || !ok || reserved.Attempts != 1 {
			t.Fatalf("reserved=%+v ok=%v err=%v", reserved, ok, err)
		}
		if err := db.DeferGraphDispatchForCapacity(t.Context(), d.ID, at); err != nil {
			t.Fatal(err)
		}
		retained := readLaunchDispatch(t, db, d.ID)
		if retained.Status != "pending" || retained.Attempts != 0 {
			t.Fatalf("capacity wait spent retries: %+v", retained)
		}
	}
	attempt := graphTestAttempt(t, db, instance, 1)
	if _, ok, err := db.ReserveGraphDispatch(t.Context(), d.ID, at); err != nil || !ok {
		t.Fatalf("reserve=%v %v", ok, err)
	}
	if err := db.DeferGraphDispatchForCapacity(t.Context(), d.ID, at); err == nil {
		t.Fatal("deferral while activity still running")
	}
	if err := db.DeferGraphNodeAttempt(t.Context(), attempt.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := db.DeferGraphDispatchForCapacity(t.Context(), d.ID, at); err != nil {
		t.Fatal(err)
	}
}

func TestGraphCapacityRecoveredStepReplacesItsPendingDispatch(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	at := graphTestTime().Add(time.Second)
	dispatch, err := db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.ReserveGraphDispatch(t.Context(), dispatch.ID, at); err != nil || !ok {
		t.Fatalf("reserve %v %v", ok, err)
	}
	attempt := graphTestAttempt(t, db, instance, 1)
	if err := db.DeferGraphNodeAttempt(t.Context(), attempt.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := db.DeferGraphDispatchForCapacity(t.Context(), dispatch.ID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode, at); err != nil {
		t.Fatal(err)
	}
	transition := GraphTransition{InstanceID: instance.ID, FromNode: instance.CurrentNode, ToNode: "next", OnOutcome: "done", At: at}
	if err := db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "done", FinishedAt: at, Transition: &transition}); err != nil {
		t.Fatal(err)
	}
	next, err := db.ParkGraphInstanceAfter(t.Context(), instance.ID, "next", at, at, dispatch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == dispatch.ID || next.Node != "next" || next.Status != "pending" || next.Attempts != 0 {
		t.Fatalf("next dispatch=%+v", next)
	}
	if old := readLaunchDispatch(t, db, dispatch.ID); old.Status != "finished" {
		t.Fatalf("old dispatch=%+v", old)
	}
}

func TestGraphCapacityTerminalRecoveryClosesPendingDispatch(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	at := graphTestTime().Add(time.Second)
	dispatch, err := db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	attempt := graphTestAttempt(t, db, instance, 1)
	if err := db.DeferGraphNodeAttempt(t.Context(), attempt.ID, at); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResumeDeferredGraphNodeAttempt(t.Context(), instance.ID, instance.CurrentNode, at); err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "done", FinishedAt: at, InstanceStatus: GraphSucceeded}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphDispatch(t.Context(), dispatch.ID, at); err != nil {
		t.Fatal(err)
	}
	if old := readLaunchDispatch(t, db, dispatch.ID); old.Status != "finished" {
		t.Fatalf("terminal dispatch=%+v", old)
	}
}
