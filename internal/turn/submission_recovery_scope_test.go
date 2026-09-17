package turn

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestRecoverSubmissionsForAttemptIsolatesUnrelatedRuns is a self-verification
// for issue #310 (AC-003/AC-004): run A carries a genuinely broken pending
// receipt (abandoning it hits a real story-transport sequence gap, not a
// gracefully handled invalid contribution), while run B is healthy. Scoped
// foreground recovery for run B must never see or fail on run A's receipt;
// the daemon-wide sweep and run A's own scoped recovery must still surface
// run A's genuine failure, correctly attributed.
func TestRecoverSubmissionsForAttemptIsolatesUnrelatedRuns(t *testing.T) {
	f := newFixture(t, nil)
	typedProgressScope(t, f, store.WorkflowSeparated)

	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	var storyA, storyB store.ApprovedStoryRevision
	for _, r := range revisions {
		switch r.Key {
		case "US-1":
			storyA = r
		case "US-2":
			storyB = r
		}
	}
	if storyA.Key == "" || storyB.Key == "" {
		t.Fatal("fixture did not approve both scoped stories")
	}

	// ---- Run A: engineering session bound to US-1, deliberately broken. ----
	segA, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "submission-a", store.AgentClaude)
	mustProgress(t, err)
	runA, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segA.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run-a.log" }})
	mustProgress(t, err)
	bundleA := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: runA.ID, Stories: []agent.ApprovedStory{{Key: storyA.Key, IssueID: storyA.IssueID, IssueNumber: storyA.IssueNumber, ParentID: storyA.ParentID, Revision: storyA.Hash, Title: storyA.Title, Body: storyA.Body, State: storyA.State, Dependencies: storyA.Dependencies}}}
	worktreeA := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundleA, worktreeA))
	sessionA, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: runA.ID, Worktree: worktreeA, Bundle: bundleA})
	mustProgress(t, err)
	t.Cleanup(func() { _ = sessionA.Close(t.Context()) })

	// Receipt 1 consumes run A's first sequence slot and is decided directly
	// (bypassing the broker), so it never advances story_progress_runs'
	// last_sequence the way an accepted engineering event would.
	decided, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: runA.ID, Attempt: sessionA.binding.Attempt, RequestID: "a-decided", Story: storyA.Key, Revision: storyA.Hash, InputState: "valid", Payload: []byte(`{}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	mustProgress(t, f.db.DecideSubmission(t.Context(), decided.ID, store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "not_applicable", Effects: "not_applicable", Plan: "not_applicable"}))

	// Receipt 2 consumes the next sequence slot and is left genuinely pending
	// with a payload that fails contribution parsing. Recovering it calls
	// AbandonSubmission, which now finds a sequence gap left by receipt 1
	// above (last_sequence is still 0) and fails for real — not a graceful
	// rejection.
	broken, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: runA.ID, Attempt: sessionA.binding.Attempt, RequestID: "a-broken", Story: storyA.Key, Revision: storyA.Hash, InputState: "valid", Payload: []byte(`{"not":"a contribution"}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)

	// ---- Run B: independent, healthy engineering session bound to US-2. ----
	segB, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "submission-b", store.AgentClaude)
	mustProgress(t, err)
	runB, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segB.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "run-b.log" }})
	mustProgress(t, err)
	bundleB := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: runB.ID, Stories: []agent.ApprovedStory{{Key: storyB.Key, IssueID: storyB.IssueID, IssueNumber: storyB.IssueNumber, ParentID: storyB.ParentID, Revision: storyB.Hash, Title: storyB.Title, Body: storyB.Body, State: storyB.State, Dependencies: storyB.Dependencies}}}
	worktreeB := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundleB, worktreeB))
	sessionB, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: runB.ID, Worktree: worktreeB, Bundle: bundleB})
	mustProgress(t, err)

	contributionB := capturedContribution(t, worktreeB, storyB.Key, "engineering")
	response := callContribution(t, sessionB, "submit", contributionB)
	var receiptB store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(response.Result, &receiptB))
	if receiptB.EngineeringState != "recorded" {
		t.Fatalf("run B's own healthy submission was not recorded: %+v", receiptB)
	}

	// Run B's own Close scopes recovery to its own run/attempt (AC-003): run
	// A's broken pending receipt must not fail it.
	if err := sessionB.Close(t.Context()); err != nil {
		t.Fatalf("run B's Close failed because of an unrelated run's broken receipt: %v", err)
	}

	// The daemon-wide sweep remains global (AC-004) and still finds and
	// correctly attributes run A's genuine failure.
	sweepErr := f.opts.StoryProgress.RecoverSubmissions(t.Context())
	if sweepErr == nil {
		t.Fatal("daemon-wide recovery silently accepted run A's broken pending receipt")
	}
	var recoveryErr *SubmissionRecoveryError
	if !errors.As(sweepErr, &recoveryErr) {
		t.Fatalf("daemon-wide recovery error did not identify its origin: %v", sweepErr)
	}
	if recoveryErr.RunID != runA.ID || recoveryErr.Attempt != sessionA.binding.Attempt || recoveryErr.ReceiptID != broken.ID || recoveryErr.Story != storyA.Key {
		t.Fatalf("daemon-wide recovery error identified the wrong origin: %+v", recoveryErr)
	}

	// Run A's own scoped recovery still surfaces its own genuine failure:
	// this is narrowing whose failures propagate, not suppressing a run's
	// own real errors.
	if err := f.opts.StoryProgress.RecoverSubmissionsForAttempt(t.Context(), runA.ID, sessionA.binding.Attempt); err == nil {
		t.Fatal("run A's own scoped recovery silently accepted its own broken receipt")
	}
}
