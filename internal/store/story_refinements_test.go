package store

import (
	"errors"
	"sort"
	"testing"
)

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

func TestListDeliveryParentsOmitsClosedReadyFeatures(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	for _, p := range []string{"closed", "open", "closed-unfinished"} {
		if err := db.PutManagedFeature(ctx, ManagedFeature{ParentTask: p, Generation: 1, State: "planning", WorkflowVersion: 2}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.ExecContext(ctx, `UPDATE managed_features SET engineering_succeeded=1 WHERE parent_task=?`, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO delivery_proposals(proposal_key,parent_task,generation,parent_id,branch,head,target,readiness,title,body) VALUES('k','closed-unfinished',1,1,'b','h','main','r','t','b')`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"closed", "closed-unfinished"} {
		if err := db.SetManagedParentClosed(ctx, p, true); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ListDeliveryParents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "closed-unfinished" || got[1] != "open" {
		t.Fatalf("delivery parents = %v, want [closed-unfinished open]", got)
	}
}

func TestListGraphPlanSessionsOmitsClosedParents(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	s := testSession(t, db, 123)
	if err := db.PutManagedFeature(ctx, ManagedFeature{ParentTask: s.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'graph')`, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReservePlanStory(ctx, s.TaskRef, "story-1"); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		got, err := db.ListGraphPlanSessions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	if n := count(); n != 1 {
		t.Fatalf("open parent listed %d times, want 1", n)
	}
	if err := db.SetManagedParentClosed(ctx, s.TaskRef, true); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 0 {
		t.Fatalf("closed parent listed %d times, want 0", n)
	}
	if err := db.SetManagedParentClosed(ctx, s.TaskRef, false); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("reopened parent listed %d times, want 1", n)
	}
}

func TestGraphPlanParentForChildAndDiscoveryOwed(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := t.Context()
	s := testSession(t, db, 123)
	if _, err := db.ReservePlanStory(ctx, s.TaskRef, "story-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.BindPlanStoryIssue(ctx, s.TaskRef, "story-1", 2000, 200); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GraphPlanParentForChild(ctx, s.RepoID, 200); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a non-graph Plan's child resolved: %v", err)
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'graph')`, s.ID); err != nil {
		t.Fatal(err)
	}
	parent, err := db.GraphPlanParentForChild(ctx, s.RepoID, 200)
	if err != nil || parent != s.TaskRef {
		t.Fatalf("parent = %q %v, want %q", parent, err, s.TaskRef)
	}
	if _, err := db.GraphPlanParentForChild(ctx, s.RepoID, 201); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unbound number resolved: %v", err)
	}

	owed := func() bool {
		t.Helper()
		o, err := db.StoryDiscoveryOwed(ctx, s.ID, 200)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	if !owed() {
		t.Fatal("a never-swept child is not owed")
	}
	if err := db.AdvanceStoryDiscovery(ctx, s.ID, 200, 0); err != nil {
		t.Fatal(err)
	}
	if owed() {
		t.Fatal("a swept child with no debt is owed")
	}
	if err := db.MarkStoryDiscoveryPending(ctx, s.ID, 200); err != nil {
		t.Fatal(err)
	}
	if !owed() {
		t.Fatal("a marked child is not owed")
	}
	if err := db.ClearStoryDiscoveryPending(ctx, s.ID, 200); err != nil {
		t.Fatal(err)
	}
	if owed() {
		t.Fatal("a cleared child is still owed")
	}
}
