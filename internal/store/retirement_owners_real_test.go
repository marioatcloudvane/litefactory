package store

import (
	"fmt"
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
	if err != nil || !owner.LastFinishedAt.Equal(finishedAt) || owner.Pending {
		t.Fatalf("a finished run alone must not be pending: %+v %v", owner, err)
	}
	// Real open work still keeps the owner pending.
	if _, err := db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "undelivered", Version: 1, SessionID: child.ID, TriggerID: 100, Task: child.TaskRef, Destination: child.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "report", Marker: "marker"}); err != nil {
		t.Fatal(err)
	}
	owner, err = db.GetRetirementOwner(t.Context(), parent.ID)
	if err != nil || !owner.Pending {
		t.Fatalf("undelivered report debt missing: %+v %v", owner, err)
	}
	// A read-only inventory must not clear the persisted resource identity.
	saved, err := db.GetSession(t.Context(), parent.ID)
	if err != nil || saved.Branch != parent.Branch {
		t.Fatalf("preview mutated identity: %+v %v", saved, err)
	}
}

// Stories now get their own branch and worktree (#600). Such a story owns its
// checkout and is retired on its own; a story sharing the parent's branch or
// worktree stays part of the parent.
func TestRetirementOwnStoryCheckoutIsOwner(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "api")
	parent, err := db.GetOrCreateSession(t.Context(), repo, 300)
	if err != nil {
		t.Fatal(err)
	}
	if parent, err = db.FreezeSessionSlug(t.Context(), parent.ID, "feature", "lf/300-feature"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionWorktree(t.Context(), parent.ID, "/wt/task-300"); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	story := func(issue int, branch, worktree string) Session {
		t.Helper()
		if err := db.PutManagedStory(t.Context(), ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: fmt.Sprintf("US-%d", issue), IssueNumber: issue, IssueID: int64(issue) * 10, BodyHash: "frozen", State: "planning"}); err != nil {
			t.Fatal(err)
		}
		s, err := db.GetOrCreateSession(t.Context(), repo, issue)
		if err != nil {
			t.Fatal(err)
		}
		if branch != "" {
			if _, err := db.FreezeSessionSlug(t.Context(), s.ID, "story", branch); err != nil {
				t.Fatal(err)
			}
		}
		if worktree != "" {
			if err := db.SetSessionWorktree(t.Context(), s.ID, worktree); err != nil {
				t.Fatal(err)
			}
		}
		return s
	}
	own := story(301, "lf/301-story", "/wt/task-301")
	sharedBranch := story(302, parent.Branch, "")
	sharedWorktree := story(303, "lf/303-story", "/wt/task-300")
	noBranch := story(304, "", "")
	boundary, err := db.RetirementBoundary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	owners, err := db.ListRetirementOwners(t.Context(), 0, boundary, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 || owners[0].Session.ID != parent.ID || owners[1].Session.ID != own.ID {
		t.Fatalf("owners=%+v, want parent and the story with its own checkout", owners)
	}
	if !owners[0].Canonical || !owners[1].Canonical {
		t.Fatalf("parent and own-checkout story must be canonical: %+v", owners)
	}
	for _, s := range []Session{sharedBranch, sharedWorktree, noBranch} {
		o, err := db.GetRetirementOwner(t.Context(), s.ID)
		if err != nil || o.Canonical {
			t.Fatalf("story %d sharing the parent's checkout must not be canonical: %+v %v", s.IssueNumber, o, err)
		}
	}
	// The story's family is just itself: the parent's run does not touch it.
	testRun(t, db, parent.ID, 1001)
	o, err := db.GetRetirementOwner(t.Context(), own.ID)
	if err != nil || o.Active || o.RunCount != 0 {
		t.Fatalf("own-checkout story family must be just itself: %+v %v", o, err)
	}
}
