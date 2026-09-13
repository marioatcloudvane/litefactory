package store

import (
	"testing"
	"time"
)

func TestRetirementRealOwnerInventory(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	parent, err := db.GetOrCreateSession(t.Context(), repo, 213)
	if err != nil {
		t.Fatal(err)
	}
	parent, err = db.FreezeSessionSlug(t.Context(), parent.ID, "retire", "lf/213-retire")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedStory(t.Context(), ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: "US-001", IssueNumber: 255, IssueID: 2550, BodyHash: "frozen", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	child, err := db.GetOrCreateSession(t.Context(), repo, 255)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.FreezeSessionSlug(t.Context(), child.ID, "retire", parent.Branch); err != nil {
		t.Fatal(err)
	}
	otherRepo := testRepo(t, db, "other", "api")
	other, err := db.GetOrCreateSession(t.Context(), otherRepo, 255)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := db.RetirementBoundary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	late, err := db.GetOrCreateSession(t.Context(), repo, 999)
	if err != nil {
		t.Fatal(err)
	}
	owners, err := db.ListRetirementOwners(t.Context(), 0, boundary, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 || owners[0].Session.ID != parent.ID || owners[1].Session.ID != other.ID {
		t.Fatalf("owners=%+v, want parent and unrelated repository's same issue number", owners)
	}
	if !owners[0].Canonical || owners[0].Session.WorktreePath != "" {
		t.Fatalf("remote-only canonical owner lost: %+v", owners[0])
	}
	page, err := db.ListRetirementOwners(t.Context(), parent.ID, boundary, 1)
	if err != nil || len(page) != 1 || page[0].Session.ID != other.ID {
		t.Fatalf("stable page=%+v err=%v (late id %d must be excluded)", page, err, late.ID)
	}
	childOwner, err := db.GetRetirementOwner(t.Context(), child.ID)
	if err != nil || childOwner.Canonical {
		t.Fatalf("child must not own shared branch: %+v %v", childOwner, err)
	}
	run := testRun(t, db, child.ID, 1001)
	owner, err := db.GetRetirementOwner(t.Context(), parent.ID)
	if err != nil || !owner.Active || owner.RunCount != 1 {
		t.Fatalf("child run did not block parent: %+v %v", owner, err)
	}
	finishedAt := run.StartedAt.Add(time.Minute)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunFailed, FinishedAt: finishedAt}); err != nil {
		t.Fatal(err)
	}
	owner, err = db.GetRetirementOwner(t.Context(), parent.ID)
	if err != nil || !owner.LastFinishedAt.Equal(finishedAt) || !owner.Pending {
		t.Fatalf("failed child finish/report debt missing: %+v %v", owner, err)
	}
	// A read-only inventory must not clear the persisted resource identity.
	saved, err := db.GetSession(t.Context(), parent.ID)
	if err != nil || saved.Branch != parent.Branch {
		t.Fatalf("preview mutated identity: %+v %v", saved, err)
	}
}
