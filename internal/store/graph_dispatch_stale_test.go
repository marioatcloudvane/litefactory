package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// #1299: requeueing a reserved dispatch whose instance already finished wrote
// next_due_at onto a terminal instance, which the CHECK refused; the rollback
// left the dispatch reserved for good. It is now cancelled as stale and the
// instance is left untouched.
func TestRequeueOnAFinishedInstanceCancelsWithoutTouchingIt(t *testing.T) {
	t.Parallel()
	for _, status := range []string{GraphSucceeded, GraphCancelled, GraphFailed} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			db, instance := graphDispatchFixture(t)
			ctx, at := t.Context(), graphTestTime()
			dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := db.ReserveGraphDispatch(ctx, dispatch.ID, at); err != nil || !ok {
				t.Fatalf("reserve: %v %v", ok, err)
			}
			if err := db.SetGraphInstanceStatus(ctx, instance.ID, status, time.Time{}, at.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			before, err := db.GetGraphInstance(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.ValidateGraphDispatchTarget(ctx, instance.SessionID, dispatch.TriggerID()); !errors.Is(err, ErrGraphInvariant) {
				t.Fatalf("fence=%v, want the stale invariant", err)
			}
			requeued, stopped, err := db.RequeueGraphDispatchWithStop(ctx, dispatch.ID, at.Add(time.Hour), "dispatch no longer targets the current saved node")
			if err != nil || requeued || stopped != nil {
				t.Fatalf("requeue on %s instance: requeued=%v stopped=%+v err=%v", status, requeued, stopped, err)
			}
			got := readLaunchDispatch(t, db, dispatch.ID)
			if got.Status != "cancelled" || !strings.HasPrefix(got.CancellationReason, "stale") {
				t.Fatalf("dispatch=%+v, want it cancelled as stale", got)
			}
			after, err := db.GetGraphInstance(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != before.Status || !after.NextDueAt.IsZero() || !after.FinishedAt.Equal(before.FinishedAt) {
				t.Fatalf("finished instance changed: before=%+v after=%+v", before, after)
			}
			if dead, err := db.ListDeadGraphDispatches(ctx); err != nil || len(dead) != 0 {
				t.Fatalf("dead dispatches remain: %+v %v", dead, err)
			}
		})
	}
}

// A dispatch left reserved on a finished instance by older code is listed as
// dead and cancelled on recheck; a live one is never touched.
func TestDeadGraphDispatchesAreOnlyThoseOnFinishedInstances(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	ctx, at := t.Context(), graphTestTime()
	dispatch, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.ReserveGraphDispatch(ctx, dispatch.ID, at); err != nil || !ok {
		t.Fatalf("reserve: %v %v", ok, err)
	}
	if dead, err := db.ListDeadGraphDispatches(ctx); err != nil || len(dead) != 0 {
		t.Fatalf("live dispatch listed as dead: %+v %v", dead, err)
	}
	if cancelled, err := db.CancelDeadGraphDispatch(ctx, dispatch.ID, at); err != nil || cancelled {
		t.Fatalf("live dispatch cancelled: %v %v", cancelled, err)
	}
	if err := db.SetGraphInstanceStatus(ctx, instance.ID, GraphSucceeded, time.Time{}, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	dead, err := db.ListDeadGraphDispatches(ctx)
	if err != nil || len(dead) != 1 || dead[0].ID != dispatch.ID {
		t.Fatalf("dead=%+v err=%v", dead, err)
	}
	if cancelled, err := db.CancelDeadGraphDispatch(ctx, dispatch.ID, at.Add(2*time.Minute)); err != nil || !cancelled {
		t.Fatalf("dead dispatch not cancelled: %v %v", cancelled, err)
	}
	if got := readLaunchDispatch(t, db, dispatch.ID); got.Status != "cancelled" {
		t.Fatalf("dispatch=%+v", got)
	}
}
