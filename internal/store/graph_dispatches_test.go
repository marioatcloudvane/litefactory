package store

import (
	"testing"
	"time"
)

func graphDispatchFixture(t *testing.T) (*DB, GraphInstance) {
	t.Helper()
	db := newTestDB(t)
	return db, graphTestInstance(t, db)
}

// The three private trigger bands must stay disjoint and ordered: a graph stage,
// an automatic continuation and a manual schedule all mint run identities, and
// one band leaking into another would let two of them collide on the runs
// table's (session, trigger) uniqueness.
func TestGraphTriggerBandIsDisjoint(t *testing.T) {
	t.Parallel()
	d := GraphDispatch{ID: 7}
	id := d.TriggerID()
	if !IsGraphTrigger(id) {
		t.Fatalf("%d is not recognised as a graph trigger", id)
	}
	if IsAutomaticTrigger(id) || IsScheduleTrigger(id) {
		t.Error("a graph trigger must not be read as recovery or schedule work")
	}
	for _, foreign := range []int64{0, 1, 9001, Continuation{ID: 7}.TriggerID(), Schedule{ID: 7}.TriggerID()} {
		if IsGraphTrigger(foreign) {
			t.Errorf("%d must not be read as a graph trigger", foreign)
		}
	}
}

func TestGraphDispatchLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, instance := graphDispatchFixture(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	parked, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now)
	if err != nil {
		t.Fatalf("park: %v", err)
	}
	if parked.Status != "pending" || parked.Node != instance.CurrentNode {
		t.Fatalf("unexpected dispatch: %+v", parked)
	}

	// Parking twice is how a retried turn behaves. It must not queue a second
	// stage; the open dispatch already describes the same intent.
	again, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now)
	if err != nil {
		t.Fatalf("second park: %v", err)
	}
	if again.ID != parked.ID {
		t.Fatalf("a second park queued another dispatch: %d then %d", parked.ID, again.ID)
	}

	due, err := db.ListDueGraphDispatches(ctx, now, 10)
	if err != nil || len(due) != 1 || due[0].ID != parked.ID {
		t.Fatalf("due selection: %+v err=%v", due, err)
	}
	if early, err := db.ListDueGraphDispatches(ctx, now.Add(-time.Minute), 10); err != nil || len(early) != 0 {
		t.Fatalf("a dispatch must not be due before its time: %+v err=%v", early, err)
	}

	// Exactly one caller may claim it; that is what keeps two ticks from
	// starting the same stage twice.
	first, ok, err := db.ReserveGraphDispatch(ctx, parked.ID, now)
	if err != nil || !ok || first.Status != "reserved" {
		t.Fatalf("first reserve: ok=%v err=%v %+v", ok, err, first)
	}
	if _, ok, err := db.ReserveGraphDispatch(ctx, parked.ID, now); err != nil || ok {
		t.Fatalf("a reserved dispatch was claimed twice: ok=%v err=%v", ok, err)
	}

	// Losing a worker slot must not cost the instance its continuation.
	if err := db.RequeueGraphDispatch(ctx, parked.ID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if due, err := db.ListDueGraphDispatches(ctx, now, 10); err != nil || len(due) != 1 {
		t.Fatalf("requeued work is not due again: %+v err=%v", due, err)
	}

	if _, _, err := db.ReserveGraphDispatch(ctx, parked.ID, now); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishGraphDispatch(ctx, parked.ID, now); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if due, err := db.ListDueGraphDispatches(ctx, now, 10); err != nil || len(due) != 0 {
		t.Fatalf("a finished dispatch is still due: %+v err=%v", due, err)
	}
	after, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.NextDueAt.IsZero() {
		t.Errorf("the instance is still due with no open dispatch: %v", after.NextDueAt)
	}
}

func TestParkRefusesADisagreeingNode(t *testing.T) {
	t.Parallel()
	db, instance := graphDispatchFixture(t)
	now := time.Now().UTC()
	if _, err := db.ParkGraphInstance(t.Context(), instance.ID, "some-other-node", now, now); err == nil {
		t.Error("parking on a node the instance is not on was accepted")
	}
}
