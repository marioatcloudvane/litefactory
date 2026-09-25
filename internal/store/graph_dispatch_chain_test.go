package store

import (
	"testing"
	"time"
)

func TestGraphTriggerDispatchRoundTrips(t *testing.T) {
	t.Parallel()
	d := GraphDispatch{ID: 41}
	if id, ok := GraphTriggerDispatch(d.TriggerID()); !ok || id != 41 {
		t.Fatalf("dispatch %d recovered as %d ok=%v", d.ID, id, ok)
	}
	for _, foreign := range []int64{0, 9001, Continuation{ID: 41}.TriggerID(), Schedule{ID: 41}.TriggerID()} {
		if _, ok := GraphTriggerDispatch(foreign); ok {
			t.Errorf("%d was read as a graph dispatch", foreign)
		}
	}
}

// A run a dispatch allocated is inside that dispatch when it parks the instance
// again. The plain park would find the dispatch still open and schedule nothing,
// stranding an instance that has a third stage. The parking from inside the run
// closes the spent dispatch and schedules the next in one write, so a chain of
// stages continues as long as work remains, one open dispatch at a time.
func TestParkGraphInstanceAfterChainsStages(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, instance := graphDispatchFixture(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	first, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now)
	if err != nil {
		t.Fatal(err)
	}
	reserved, ok, err := db.ReserveGraphDispatch(ctx, first.ID, now)
	if err != nil || !ok {
		t.Fatalf("reserve: ok=%v err=%v", ok, err)
	}
	// The plain park, made from inside the running stage, returns the spent dispatch.
	if same, err := db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, now, now); err != nil || same.ID != reserved.ID {
		t.Fatalf("plain park inside a running stage: %+v err=%v", same, err)
	}

	next, err := db.ParkGraphInstanceAfter(ctx, instance.ID, instance.CurrentNode, now, now, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == first.ID || next.Status != "pending" {
		t.Fatalf("the next stage was not scheduled: %+v", next)
	}
	spent, err := scanGraphDispatch(db.sql.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, first.ID))
	if err != nil || spent.Status != "finished" {
		t.Fatalf("the spent dispatch stayed %q: %v", spent.Status, err)
	}
	// Closing the spent dispatch afterwards, as the daemon does, keeps the new one.
	if err = db.FinishGraphDispatch(ctx, first.ID, now); err != nil {
		t.Fatal(err)
	}
	due, err := db.ListDueGraphDispatches(ctx, now.Add(time.Minute), 10)
	if err != nil || len(due) != 1 || due[0].ID != next.ID {
		t.Fatalf("exactly the next dispatch must be owed: %+v err=%v", due, err)
	}
	after, err := db.GetGraphInstance(ctx, instance.ID)
	if err != nil || after.NextDueAt.IsZero() {
		t.Fatalf("the instance lost its due time: %+v err=%v", after, err)
	}

	// A dispatch of another instance is never closed by this one's park.
	if _, err = db.ParkGraphInstanceAfter(ctx, instance.ID, instance.CurrentNode, now, now, next.ID+1000); err != nil {
		t.Fatal(err)
	}
	if still, err := scanGraphDispatch(db.sql.QueryRowContext(ctx, `SELECT `+graphDispatchColumns+` FROM graph_dispatches WHERE id=?`, next.ID)); err != nil || still.Status != "pending" {
		t.Fatalf("an unrelated dispatch id closed the open one: %+v err=%v", still, err)
	}
}
