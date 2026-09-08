package store

import (
	"testing"
	"time"
)

func approvedFixture(t *testing.T) (*DB, Session, []ApprovedStoryRevision) {
	t.Helper()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	var revisions []ApprovedStoryRevision
	for i, key := range []string{"US-1", "US-2"} {
		s := ManagedStory{ParentTask: session.TaskRef, Generation: 1, Key: key, IssueNumber: 200 + i, IssueID: int64(2000 + i), BodyHash: "old", State: "planning"}
		if err := db.PutManagedStory(t.Context(), s); err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, ApprovedStoryRevision{ManagedStory: s, ParentID: 1230, Title: "Title", Body: "Exact body", Hash: "approved", FetchedAt: time.Now().UTC()})
	}
	if err := db.SetManagedFeatureReady(t.Context(), session.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	return db, session, revisions
}
func TestApprovalIsAtomicAndExcludesPendingRefinements(t *testing.T) {
	db, s, rs := approvedFixture(t)
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, rs[:1]); err == nil {
		t.Fatal("partial approval accepted")
	}
	got, err := db.ListApprovedStoryRevisions(t.Context(), s.TaskRef, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("partial scope persisted: %v %v", got, err)
	}
	r := StoryRefinement{SessionID: s.ID, CommentID: 555, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}
	admitted := make(chan error, 1)
	go func() { admitted <- db.AdmitStoryRefinement(t.Context(), r) }()
	if err := <-admitted; err != nil {
		t.Fatal(err)
	}
	// Readiness can be restored by another publication while discussion is queued.
	if err := db.SetManagedFeatureReady(t.Context(), s.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, rs); err == nil {
		t.Fatal("promotion overtook admitted refinement")
	}
	if err := db.ConsumeStoryRefinement(t.Context(), s.ID, r.CommentID); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, rs); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateManagedScope(t.Context(), s.TaskRef, 1); err == nil {
		t.Fatal("launched before freeze notices")
	}
	for _, r := range rs {
		if err := db.DeliverStoryScopeNotice(t.Context(), s.TaskRef, 1, r.Key, "freeze"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ActivateManagedScope(t.Context(), s.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, rs); err == nil {
		t.Fatal("approved revision overwritten")
	}
}
func TestReplanPreservesCompletedAndOldApprovedScope(t *testing.T) {
	db, s, rs := approvedFixture(t)
	if err := db.ApproveManagedStories(t.Context(), s.TaskRef, 1, rs); err != nil {
		t.Fatal(err)
	}
	done := rs[0].ManagedStory
	done.State = "completed"
	if err := db.CompareManagedStory(t.Context(), done, "frozen"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSessionState(t.Context(), s.ID, SessionRunning); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplanManagedFeature(t.Context(), s.TaskRef, 1); err == nil {
		t.Fatal("active parent replanned")
	}
	if err := db.SetSessionState(t.Context(), s.ID, SessionIdle); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplanManagedFeature(t.Context(), s.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	current, err := db.ListManagedStories(t.Context(), s.TaskRef, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 2 || current[0].State != "completed" || current[1].State != "planning" {
		t.Fatalf("wrong next generation: %+v", current)
	}
	old, err := db.ListApprovedStoryRevisions(t.Context(), s.TaskRef, 1)
	if err != nil || len(old) != 2 || old[0].Body != "Exact body" {
		t.Fatalf("approval changed: %+v %v", old, err)
	}
	next, err := db.ListApprovedStoryRevisions(t.Context(), s.TaskRef, 2)
	if err != nil || len(next) != 0 {
		t.Fatalf("old approval reused for new scope: %+v %v", next, err)
	}
	// Invalidation changes the lifecycle alone, retaining exact approved input.
	done.State = "frozen"
	if err := db.CompareManagedStory(t.Context(), done, "completed"); err != nil {
		t.Fatal(err)
	}
	old, err = db.ListApprovedStoryRevisions(t.Context(), s.TaskRef, 1)
	if err != nil || old[0].Hash != "approved" || old[0].State != "frozen" {
		t.Fatalf("invalidation lost frozen input: %+v %v", old, err)
	}
}
