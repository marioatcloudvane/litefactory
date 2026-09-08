package store

import "testing"

func TestRefinementReportDoesNotConsumeParentComment(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session := testSession(t, db, 123)
	if _, _, err := db.BootstrapWatermark(t.Context(), session.ID, 20); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitStoryRefinement(t.Context(), StoryRefinement{SessionID: session.ID, CommentID: 100, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
		t.Fatal(err)
	}
	run := testRun(t, db, session.ID, 100)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListPendingStoryRefinements(t.Context(), session.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("admission consumed before delivery: %v %v", pending, err)
	}
	if _, err := db.CompleteRunReport(t.Context(), run.ID, 500); err != nil {
		t.Fatal(err)
	}
	pending, err = db.ListPendingStoryRefinements(t.Context(), session.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("delivery not consumed: %v %v", pending, err)
	}
	wm, exists, err := db.GetWatermark(t.Context(), session.ID)
	if err != nil || !exists || wm.LastCommentID != 20 {
		t.Fatalf("parent watermark changed: %+v %v", wm, err)
	}
	if _, err := db.CompleteRunReport(t.Context(), run.ID, 500); err != nil {
		t.Fatal(err)
	}
}

func TestRefinementAdmissionInvalidatesReadinessOnlyOnce(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	s := testSession(t, db, 123)
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), s.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	r := StoryRefinement{SessionID: s.ID, CommentID: 100, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}
	if err := db.AdmitStoryRefinement(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	feature, err := db.GetManagedFeature(t.Context(), s.TaskRef)
	if err != nil || feature.Ready {
		t.Fatalf("admission left ready: %+v %v", feature, err)
	}
	if err := db.ConsumeStoryRefinement(t.Context(), s.ID, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.SetManagedFeatureReady(t.Context(), s.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitStoryRefinement(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	feature, err = db.GetManagedFeature(t.Context(), s.TaskRef)
	if err != nil || !feature.Ready {
		t.Fatalf("rediscovery invalidated later plan: %+v %v", feature, err)
	}
}
