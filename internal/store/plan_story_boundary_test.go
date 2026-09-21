package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestPlanOrdinalPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ordinals.db")
	open := func() *DB {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	defer func() { _ = db.Close() }()
	for i := 1; i <= 1001; i++ {
		key := fmt.Sprintf("story-%d", i)
		d, err := db.ReservePlanStory(ctx, "parent-one", key)
		if err != nil || d.Ordinal != int64(i) {
			t.Fatalf("reserve %d: %+v %v", i, d, err)
		}
	}
	isolated, err := db.ReservePlanStory(ctx, "parent-two", "story-1")
	if err != nil || isolated.Ordinal != 1 {
		t.Fatalf("parent sequence leaked %+v %v", isolated, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	retried, err := db.ReservePlanStory(ctx, "parent-one", "story-1000")
	if err != nil || retried.Ordinal != 1000 {
		t.Fatalf("restart renumbered %+v %v", retried, err)
	}
	next, err := db.ReservePlanStory(ctx, "parent-one", "next")
	if err != nil || next.Ordinal != 1002 {
		t.Fatalf("reserved number reused %+v %v", next, err)
	}
}
func TestPlanCandidateIntentImmutableAndPendingExclusive(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReservePlanStory(ctx, "parent", "story"); err != nil {
		t.Fatal(err)
	}
	candidate := PlanStoryCandidate{Parent: "parent", Key: "story", RequestID: "one", Candidate: "first", RequestJSON: `{"operation":"create"}`}
	if err = db.PutPlanStoryCandidate(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	changed := candidate
	changed.RequestJSON = `{"operation":"retire"}`
	if err = db.PutPlanStoryCandidate(ctx, changed); err == nil {
		t.Fatal("immutable request overwritten")
	}
	next := candidate
	next.RequestID = "two"
	next.Candidate = "second"
	if err = db.PutPlanStoryCandidate(ctx, next); err == nil {
		t.Fatal("pending candidate superseded")
	}
	effect := PlanStoryEffect{Parent: "parent", RequestID: "one", Phase: "create", Outcome: "uncertain", Detail: "lost reply"}
	if err = db.AppendPlanStoryEffect(ctx, effect); err != nil {
		t.Fatal(err)
	}
	if err = db.PutPlanStoryCandidate(ctx, next); err == nil {
		t.Fatal("uncertain effect superseded")
	}
	effect.Outcome = "conflict"
	if err = db.AppendPlanStoryEffect(ctx, effect); err != nil {
		t.Fatal(err)
	}
	if err = db.PutPlanStoryCandidate(ctx, next); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetPlanStoryCandidate(ctx, "parent", "one")
	if err != nil || retained != candidate {
		t.Fatalf("candidate history lost %+v %v", retained, err)
	}
}
