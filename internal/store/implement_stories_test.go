package store

import (
	"errors"
	"testing"
	"time"
)

var implementT0 = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func seedImplement(t *testing.T, db *DB, keys ...string) {
	t.Helper()
	seeds := make([]ImplementStorySeed, len(keys))
	for i, k := range keys {
		seeds[i] = ImplementStorySeed{Key: k, Position: i, Branch: "lf/" + k}
	}
	if err := db.EnsureImplementStories(t.Context(), "github:o/r#1", 1, seeds, implementT0); err != nil {
		t.Fatal(err)
	}
}

func move(t *testing.T, db *DB, key, from, to string, rev int64, extra func(*ImplementTransition)) (ImplementStory, error) {
	t.Helper()
	in := ImplementTransition{Parent: "github:o/r#1", Generation: 1, Key: key, From: from, To: to, ExpectedRevision: rev, At: implementT0.Add(time.Minute)}
	if extra != nil {
		extra(&in)
	}
	return db.TransitionImplementStory(t.Context(), in)
}

func TestEnsureImplementStoriesIsIdempotentAndKeepsPlanOrder(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "b", "a", "c")
	if _, err := move(t, db, "b", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatal(err)
	}
	seedImplement(t, db, "b", "a", "c")
	rows, err := db.ListImplementStories(t.Context(), "github:o/r#1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Key != "b" || rows[1].Key != "a" || rows[2].Key != "c" {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].State != ImplementRunning || rows[0].Revision != 2 {
		t.Fatalf("re-seeding must not reset an existing row: %+v", rows[0])
	}
	other, err := db.ListImplementStories(t.Context(), "github:o/r#1", 2)
	if err != nil || len(other) != 0 {
		t.Fatalf("another generation must be empty: %v %v", other, err)
	}
}

func TestTransitionImplementStoryLifecycle(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a", "b")
	got, err := move(t, db, "a", ImplementWaiting, ImplementRunning, 1, nil)
	if err != nil || got.State != ImplementRunning || got.Revision != 2 {
		t.Fatalf("running: %+v %v", got, err)
	}
	if n, err := db.CountImplementRunning(t.Context(), "github:o/r#1", 1); err != nil || n != 1 {
		t.Fatalf("count = %d %v", n, err)
	}
	if got, err = move(t, db, "a", ImplementRunning, ImplementDone, 2, nil); err != nil || got.State != ImplementDone {
		t.Fatalf("done: %+v %v", got, err)
	}
	got, err = move(t, db, "a", ImplementDone, ImplementMerged, 3, func(in *ImplementTransition) { in.MergeCommit = "abc" })
	if err != nil || got.State != ImplementMerged || got.MergeCommit != "abc" {
		t.Fatalf("merged: %+v %v", got, err)
	}
	if _, err = move(t, db, "a", ImplementMerged, ImplementParked, 4, func(in *ImplementTransition) { in.Reason = "x" }); !errors.Is(err, ErrImplementTransition) {
		t.Fatalf("merged is final, got %v", err)
	}
	seedImplement(t, db, "b")
	for _, step := range [][2]string{{ImplementWaiting, ImplementRunning}, {ImplementRunning, ImplementDone}} {
		if _, err := move(t, db, "b", step[0], step[1], map[string]int64{ImplementWaiting: 1, ImplementRunning: 2}[step[0]], nil); err != nil {
			t.Fatal(err)
		}
	}
	got, err = move(t, db, "b", ImplementDone, ImplementParked, 3, func(in *ImplementTransition) { in.Reason = "conflict" })
	if err != nil || got.State != ImplementParked || got.Reason != "conflict" {
		t.Fatalf("parked: %+v %v", got, err)
	}
}

func TestTransitionImplementStoryRefusesBadMoves(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a")
	for name, tc := range map[string]struct {
		from, to string
		extra    func(*ImplementTransition)
	}{
		"skip running":      {ImplementWaiting, ImplementDone, nil},
		"park from waiting": {ImplementWaiting, ImplementParked, func(in *ImplementTransition) { in.Reason = "r" }},
		"park no reason":    {ImplementDone, ImplementParked, nil},
		"merge no commit":   {ImplementDone, ImplementMerged, nil},
		"reason on running": {ImplementWaiting, ImplementRunning, func(in *ImplementTransition) { in.Reason = "r" }},
		"commit on done":    {ImplementRunning, ImplementDone, func(in *ImplementTransition) { in.MergeCommit = "c" }},
	} {
		if _, err := move(t, db, "a", tc.from, tc.to, 1, tc.extra); !errors.Is(err, ErrImplementTransition) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTransitionImplementStoryCompareAndSet(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a")
	if _, err := move(t, db, "a", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := move(t, db, "a", ImplementWaiting, ImplementRunning, 1, nil); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("second winner: %v", err)
	}
	if _, err := move(t, db, "a", ImplementRunning, ImplementDone, 1, nil); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err := move(t, db, "missing", ImplementWaiting, ImplementRunning, 1, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing row: %v", err)
	}
}

func TestTransitionImplementStoryEnforcesRunningCap(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a", "b", "c", "d", "e")
	for _, k := range []string{"a", "b", "c", "d"} {
		if _, err := move(t, db, k, ImplementWaiting, ImplementRunning, 1, nil); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	if _, err := move(t, db, "e", ImplementWaiting, ImplementRunning, 1, nil); !errors.Is(err, ErrImplementCap) {
		t.Fatalf("fifth running: %v", err)
	}
	if _, err := move(t, db, "a", ImplementRunning, ImplementDone, 2, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := move(t, db, "e", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatalf("a slot freed: %v", err)
	}
}

func TestEnsureImplementStoriesRefusesInvalidInput(t *testing.T) {
	db := newTestDB(t)
	if err := db.EnsureImplementStories(t.Context(), "", 1, nil, implementT0); err == nil {
		t.Error("blank parent accepted")
	}
	if err := db.EnsureImplementStories(t.Context(), "p", 1, []ImplementStorySeed{{Key: "a"}}, implementT0); err == nil {
		t.Error("story without branch accepted")
	}
}

// TestSetImplementDeviationsRoundTripsAndReplaces checks that a running story
// keeps its worker's deviations, a later answer replaces them, the revision is
// left alone and a story that is not running is refused.
func TestSetImplementDeviationsRoundTripsAndReplaces(t *testing.T) {
	db := newTestDB(t)
	seedImplement(t, db, "a", "b")
	if _, err := move(t, db, "a", ImplementWaiting, ImplementRunning, 1, nil); err != nil {
		t.Fatal(err)
	}
	const first = "Chose best-effort delivery (bot.go:12).\nKept the old route name (api.go:3)."
	if err := db.SetImplementDeviations(t.Context(), "github:o/r#1", 1, "a", first, implementT0); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a")
	if err != nil || got.Deviations != first || got.Revision != 2 {
		t.Fatalf("deviations kept without a revision bump: %+v %v", got, err)
	}
	if err := db.SetImplementDeviations(t.Context(), "github:o/r#1", 1, "a", "", implementT0); err != nil {
		t.Fatal(err)
	}
	if got, err = db.GetImplementStory(t.Context(), "github:o/r#1", 1, "a"); err != nil || got.Deviations != "" {
		t.Fatalf("a later answer replaces the list: %+v %v", got, err)
	}
	if err := db.SetImplementDeviations(t.Context(), "github:o/r#1", 1, "b", "x", implementT0); !errors.Is(err, ErrImplementConflict) {
		t.Fatalf("a waiting story is refused: %v", err)
	}
	if err := db.SetImplementDeviations(t.Context(), "", 1, "a", "x", implementT0); err == nil {
		t.Fatal("invalid scope accepted")
	}
	rows, err := db.ListImplementStories(t.Context(), "github:o/r#1", 1)
	if err != nil || len(rows) != 2 || rows[1].Deviations != "" {
		t.Fatalf("other stories carry none: %+v %v", rows, err)
	}
}
