package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func runningStory(t *testing.T, db *DB, keys ...string) {
	t.Helper()
	seedImplement(t, db, keys...)
	for _, k := range keys {
		if _, err := move(t, db, k, ImplementWaiting, ImplementRunning, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecordImplementFailureCountsUpToTheBudgetOnly(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a")
	for want := 1; want <= MaxImplementAttempts+2; want++ {
		row, err := db.RecordImplementFailure(t.Context(), "github:o/r#1", 1, "a", "attempt broke", implementT0)
		if err != nil {
			t.Fatal(err)
		}
		if limit := min(want, MaxImplementAttempts); row.Attempts != limit || row.LastFailure != "attempt broke" {
			t.Fatalf("after failure %d: attempts=%d reason=%q", want, row.Attempts, row.LastFailure)
		}
	}
	if _, err := db.RecordImplementFailure(t.Context(), "github:o/r#1", 1, "a", " ", implementT0); err == nil {
		t.Fatal("a failure needs a reason")
	}
}

func TestRecordImplementFailureOnlyForARunningStory(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a")
	if _, err := db.RecordImplementFailure(t.Context(), "github:o/r#1", 1, "a", "x", implementT0); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("a waiting story cannot fail an attempt: %v", err)
	}
}

func TestWorkerParksAndOwnerResumeResetsTheStory(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", 77, implementT0); err != nil {
		t.Fatal(err)
	}
	for range MaxImplementAttempts {
		if _, err := db.RecordImplementFailure(t.Context(), "github:o/r#1", 1, "a", "broke", implementT0); err != nil {
			t.Fatal(err)
		}
	}
	row, _ := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
	parked, err := move(t, db, "a", ImplementRunning, ImplementParked, row.Revision, func(in *ImplementTransition) { in.Reason = "needs a decision" })
	if err != nil || parked.ParkOrigin != ImplementOriginWorker || parked.Attempts != MaxImplementAttempts || parked.WorkerSession != 77 {
		t.Fatalf("parked by the worker: %+v %v", parked, err)
	}
	if _, err := move(t, db, "a", ImplementParked, ImplementRunning, parked.Revision, nil); !errors.Is(err, ErrImplementTransition) {
		t.Fatalf("a parked story only resumes through waiting: %v", err)
	}
	if _, err := move(t, db, "a", ImplementRunning, ImplementWaiting, parked.Revision, nil); !errors.Is(err, ErrImplementTransition) {
		t.Fatalf("only a parked story resumes: %v", err)
	}
	resumed, err := move(t, db, "a", ImplementParked, ImplementWaiting, parked.Revision, func(in *ImplementTransition) { in.Guidance = "use the other API" })
	if err != nil || resumed.State != ImplementWaiting || resumed.Attempts != 0 || resumed.ParkOrigin != "" || resumed.Reason != "" || resumed.LastFailure != "" || resumed.WorkerSession != 0 || resumed.Guidance != "use the other API" || resumed.Branch != "lf/a" {
		t.Fatalf("a resumed story keeps its branch and starts over: %+v %v", resumed, err)
	}
	if _, err := move(t, db, "a", ImplementWaiting, ImplementRunning, resumed.Revision, func(in *ImplementTransition) { in.Guidance = "x" }); !errors.Is(err, ErrImplementTransition) {
		t.Fatalf("only a resume carries guidance: %v", err)
	}
}

func TestMergeParkRecordsItsOrigin(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a")
	if _, err := move(t, db, "a", ImplementRunning, ImplementDone, 2, nil); err != nil {
		t.Fatal(err)
	}
	parked, err := move(t, db, "a", ImplementDone, ImplementParked, 3, func(in *ImplementTransition) { in.Reason = "conflict" })
	if err != nil || parked.ParkOrigin != ImplementOriginMerge {
		t.Fatalf("%+v %v", parked, err)
	}
	if _, err := move(t, db, "a", ImplementParked, ImplementWaiting, 4, func(in *ImplementTransition) { in.Guidance = strings.Repeat("x", maxImplementGuidance+1) }); !errors.Is(err, ErrImplementTransition) {
		t.Fatalf("oversized guidance: %v", err)
	}
	if _, err := move(t, db, "a", ImplementParked, ImplementWaiting, 4, nil); err != nil {
		t.Fatalf("a merge-parked story resumes the same way: %v", err)
	}
}

func TestSetImplementWorkerSessionIsForRunningStoriesAndKeepsRevision(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a")
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", 5, implementT0); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("waiting: %v", err)
	}
	if _, err := move(t, db, "a", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", 5, implementT0); err != nil {
		t.Fatal(err)
	}
	row, err := db.ImplementStoryByWorkerSession(t.Context(), 5)
	if err != nil || row.Key != "a" || row.Revision != 2 {
		t.Fatalf("%+v %v", row, err)
	}
	if _, err := db.ImplementStoryByWorkerSession(t.Context(), 6); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
}

func TestListImplementStoriesNeedingWorkerSkipsLiveSessionsAndOtherStates(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a", "b", "c")
	seedImplement(t, db, "d")
	live := testSession(t, db, 41)
	idle := testSession(t, db, 42)
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", live.ID, implementT0); err != nil {
		t.Fatal(err)
	}
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "b", idle.ID, implementT0); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionState(t.Context(), live.ID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListImplementStoriesNeedingWorker(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	if strings.Join(keys, ",") != "b,c" {
		t.Fatalf("running stories without a live worker = %v", keys)
	}
}

func TestCountFailedImplementInstancesCountsOnlyFailedImplementRunsSince(t *testing.T) {
	db := newTestDB(t)
	instance := graphTestInstance(t, db)
	before := instance.StartedAt.Add(-time.Hour)
	if n, err := db.CountFailedImplementInstances(t.Context(), instance.SessionID, before); err != nil || n != 0 {
		t.Fatalf("a running instance is not a failure: %d %v", n, err)
	}
}

func TestWakeGraphInstanceLeavesExactlyOneOpenDispatch(t *testing.T) {
	db, instance := graphDispatchFixture(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	later := now.Add(5 * time.Minute)
	if woken, err := db.WakeGraphInstance(t.Context(), instance.ID, "elsewhere", now); err != nil || woken {
		t.Fatalf("an instance resting elsewhere is not woken: %v %v", woken, err)
	}
	if _, err := db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, later, now); err != nil {
		t.Fatal(err)
	}
	if due, _ := db.ListDueGraphDispatches(t.Context(), now, 10); len(due) != 0 {
		t.Fatalf("parked for later: %+v", due)
	}
	for range 3 {
		if _, err := db.WakeGraphInstance(t.Context(), instance.ID, instance.CurrentNode, now); err != nil {
			t.Fatal(err)
		}
	}
	due, err := db.ListDueGraphDispatches(t.Context(), now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("a wake brings the one open dispatch forward: %+v %v", due, err)
	}
	var open int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM graph_dispatches WHERE instance_id=? AND status IN ('pending','reserved','running')`, instance.ID).Scan(&open); err != nil || open != 1 {
		t.Fatalf("open dispatches = %d %v", open, err)
	}
	if _, ok, err := db.ReserveGraphDispatch(t.Context(), due[0].ID, now); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if woken, err := db.WakeGraphInstance(t.Context(), instance.ID, instance.CurrentNode, now); err != nil || woken {
		t.Fatalf("a dispatch already reserved is not duplicated: %v %v", woken, err)
	}
}

func TestWakeGraphInstanceCreatesTheDispatchOfARestingInstance(t *testing.T) {
	db, instance := graphDispatchFixture(t)
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	woken, err := db.WakeGraphInstance(t.Context(), instance.ID, instance.CurrentNode, now)
	if err != nil || !woken {
		t.Fatalf("%v %v", woken, err)
	}
	if due, err := db.ListDueGraphDispatches(t.Context(), now, 10); err != nil || len(due) != 1 || due[0].Node != instance.CurrentNode {
		t.Fatalf("%+v %v", due, err)
	}
}

func TestNextFreshGraphDispatchWaitsForItsDueTime(t *testing.T) {
	db, instance := graphDispatchFixture(t)
	now := time.Now().UTC()
	if _, err := db.ParkGraphInstance(t.Context(), instance.ID, instance.CurrentNode, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.NextFreshGraphDispatch(t.Context(), instance.SessionID); err != nil || ok {
		t.Fatalf("a dispatch parked for later is left to the tick: %v %v", ok, err)
	}
	if _, err := db.WakeGraphInstance(t.Context(), instance.ID, instance.CurrentNode, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.NextFreshGraphDispatch(t.Context(), instance.SessionID); err != nil || !ok {
		t.Fatalf("a woken dispatch is owed at once: %v %v", ok, err)
	}
}

func TestFailImplementWorkerInstanceRetainsCauseAtomically(t *testing.T) {
	db := newTestDB(t)
	runningStory(t, db, "a")
	session := testSession(t, db, 315)
	pin, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), TeamSnapshotDigest: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: pin.ID, CurrentNode: "worker_agent", StartedAt: implementT0})
	if err != nil {
		t.Fatal(err)
	}
	// Missing worker ownership must roll back without terminalizing the graph.
	if err := db.FailImplementWorkerInstance(t.Context(), instance.ID, "prepare broke", implementT0); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("%v", err)
	}
	got, err := db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || got.Status != GraphRunning {
		t.Fatalf("%+v %v", got, err)
	}
	if err := db.SetImplementWorkerSession(t.Context(), "github:o/r#1", 1, "a", session.ID, implementT0); err != nil {
		t.Fatal(err)
	}
	if err := db.FailImplementWorkerInstance(t.Context(), instance.ID, "prepare broke", implementT0); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil || got.Status != GraphFailed {
		t.Fatalf("%+v %v", got, err)
	}
	row, err := db.ImplementStoryByWorkerSession(t.Context(), session.ID)
	if err != nil || row.LastFailure != "prepare broke" || row.Attempts != 0 {
		t.Fatalf("%+v %v", row, err)
	}
}
