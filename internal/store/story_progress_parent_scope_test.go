package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestListStoryProgressRunsForParentScopesToOwningParent is store-level
// support for issue #310's AC-003/AC-004: [DB.ListStoryProgressRunsForParent]
// (used by turn.StoryProgress.Finish/PollRun, the scoped foreground paths)
// must return only the runs registered to the given parent, never a run
// registered to a different managed feature, while the pre-existing global
// [DB.ListStoryProgressRuns] (used by the daemon-wide sweep) keeps seeing
// every registered run. [DB.GetStoryProgressRun] must resolve each run's own
// true parent independently, and report [ErrNotFound] for an unregistered run.
func TestListStoryProgressRunsForParentScopesToOwningParent(t *testing.T) {
	t.Parallel()
	db, sessionA, revisionsA := approvedFixture(t)
	if err := db.ApproveManagedStories(t.Context(), sessionA.TaskRef, 1, revisionsA); err != nil {
		t.Fatal(err)
	}
	for _, r := range revisionsA {
		if err := db.DeliverStoryScopeNotice(t.Context(), sessionA.TaskRef, 1, r.Key, "freeze"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ActivateManagedScope(t.Context(), sessionA.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	segA, err := db.GetOrCreateSegment(t.Context(), sessionA.ID, FirstSegmentOrdinal, "run-a", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	runA, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: sessionA.ID, SegmentID: segA.ID, Mode: "implement", Agent: AgentClaude, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("/logs/a/%d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterStoryProgress(t.Context(), StoryProgressRun{RunID: runA.ID, Parent: sessionA.TaskRef, Generation: 1, Worktree: "/tmp/worktree-a", Active: true}); err != nil {
		t.Fatal(err)
	}

	// A wholly independent parent, on the same database, exercised the same
	// way real unrelated managed features are.
	sessionB := testSession(t, db, 654)
	if err := db.PutManagedFeature(t.Context(), ManagedFeature{ParentTask: sessionB.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	storyB := ManagedStory{ParentTask: sessionB.TaskRef, Generation: 1, Key: "US-1", IssueNumber: 700, IssueID: 7000, BodyHash: "old", State: "planning"}
	if err := db.PutManagedStory(t.Context(), storyB); err != nil {
		t.Fatal(err)
	}
	revisionB := ApprovedStoryRevision{ManagedStory: storyB, ParentID: 7770, Title: "Title B", Body: "Body B", Hash: "approved-b", FetchedAt: time.Now().UTC()}
	if err := db.SetManagedFeatureReady(t.Context(), sessionB.TaskRef, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := db.ApproveManagedStories(t.Context(), sessionB.TaskRef, 1, []ApprovedStoryRevision{revisionB}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeliverStoryScopeNotice(t.Context(), sessionB.TaskRef, 1, storyB.Key, "freeze"); err != nil {
		t.Fatal(err)
	}
	if err := db.ActivateManagedScope(t.Context(), sessionB.TaskRef, 1); err != nil {
		t.Fatal(err)
	}
	segB, err := db.GetOrCreateSegment(t.Context(), sessionB.ID, FirstSegmentOrdinal, "run-b", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	runB, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: sessionB.ID, SegmentID: segB.ID, Mode: "implement", Agent: AgentClaude, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("/logs/b/%d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RegisterStoryProgress(t.Context(), StoryProgressRun{RunID: runB.ID, Parent: sessionB.TaskRef, Generation: 1, Worktree: "/tmp/worktree-b", Active: true}); err != nil {
		t.Fatal(err)
	}

	forParentA, err := db.ListStoryProgressRunsForParent(t.Context(), sessionA.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(forParentA) != 1 || forParentA[0].RunID != runA.ID {
		t.Fatalf("parent A's scoped list leaked or lost a run: %+v", forParentA)
	}

	forParentB, err := db.ListStoryProgressRunsForParent(t.Context(), sessionB.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(forParentB) != 1 || forParentB[0].RunID != runB.ID {
		t.Fatalf("parent B's scoped list leaked or lost a run: %+v", forParentB)
	}

	all, err := db.ListStoryProgressRuns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the global daemon-wide list lost a registered run: %+v", all)
	}

	gotA, err := db.GetStoryProgressRun(t.Context(), runA.ID)
	if err != nil || gotA.Parent != sessionA.TaskRef {
		t.Fatalf("GetStoryProgressRun resolved the wrong parent for run A: %+v (err=%v)", gotA, err)
	}
	gotB, err := db.GetStoryProgressRun(t.Context(), runB.ID)
	if err != nil || gotB.Parent != sessionB.TaskRef {
		t.Fatalf("GetStoryProgressRun resolved the wrong parent for run B: %+v (err=%v)", gotB, err)
	}

	if _, err := db.GetStoryProgressRun(t.Context(), 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unregistered run did not report ErrNotFound: %v", err)
	}
}

// TestListPendingSubmissionsForAttemptScopesToOneRunAttempt is store-level
// support for issue #310's AC-003/AC-004:
// [DB.ListPendingSubmissionsForAttempt] (used by every scoped foreground
// recovery call site) must see only the given run's own attempt, while the
// pre-existing global [DB.ListPendingSubmissions] (used by the daemon-wide
// sweep) still sees every pending receipt regardless of run or attempt.
func TestListPendingSubmissionsForAttemptScopesToOneRunAttempt(t *testing.T) {
	t.Parallel()
	db, a := submissionFixture(t, "engineering")
	pendingA, err := db.AllocateSubmission(t.Context(), submissionRequest(a, "pending-a"))
	if err != nil {
		t.Fatal(err)
	}

	run1, err := db.GetRun(t.Context(), a.RunID)
	if err != nil {
		t.Fatal(err)
	}
	seg2, err := db.GetOrCreateSegment(t.Context(), run1.SessionID, 2, "second-attempt-segment", AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := db.AllocateRun(t.Context(), AllocateRunParams{SessionID: run1.SessionID, SegmentID: seg2.ID, Mode: "implement", Agent: AgentClaude, TriggerCommentID: 2, LogPath: func(n int) string { return fmt.Sprintf("/logs/2/%d.jsonl", n) }})
	if err != nil {
		t.Fatal(err)
	}
	attempt2 := SubmissionAttempt{RunID: run2.ID, Attempt: "attempt-2", Parent: a.Parent, Generation: a.Generation, Stage: "engineering"}
	for _, s := range a.Stories {
		attempt2.Stories = append(attempt2.Stories, SubmissionStoryBinding{Story: s.Story, IssueID: s.IssueID, Revision: s.Revision})
	}
	if err := db.BindSubmissionAttempt(t.Context(), attempt2); err != nil {
		t.Fatal(err)
	}
	pendingB, err := db.AllocateSubmission(t.Context(), submissionRequest(attempt2, "pending-b"))
	if err != nil {
		t.Fatal(err)
	}

	forA, err := db.ListPendingSubmissionsForAttempt(t.Context(), a.RunID, a.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	if len(forA) != 1 || forA[0].ID != pendingA.ID {
		t.Fatalf("run A's scoped pending list leaked or lost a receipt: %+v", forA)
	}

	forB, err := db.ListPendingSubmissionsForAttempt(t.Context(), run2.ID, attempt2.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	if len(forB) != 1 || forB[0].ID != pendingB.ID {
		t.Fatalf("run B's scoped pending list leaked or lost a receipt: %+v", forB)
	}

	all, err := db.ListPendingSubmissions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("the global pending list lost a receipt: %+v", all)
	}

	mismatched, err := db.ListPendingSubmissionsForAttempt(t.Context(), a.RunID, "no-such-attempt")
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatched) != 0 {
		t.Fatalf("a wrong attempt on the right run leaked another attempt's pending receipt: %+v", mismatched)
	}
}
