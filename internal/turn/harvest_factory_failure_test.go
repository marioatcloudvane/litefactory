package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestDescribeFactoryFailureIsPhaseAware is a self-verification for issue
// #311 (AC-005): a local factory fault that surfaces only after the
// subprocess already produced a RunResult must be attributed to the phase it
// actually happened in — [SubmissionRecoveryError]'s scoped submission
// recovery (US-002) versus ordinary session/resource cleanup — never
// mislabeled as the agent never starting. The original fault stays reachable
// through errors.As/errors.Is.
func TestDescribeFactoryFailureIsPhaseAware(t *testing.T) {
	t.Parallel()

	genericCleanup := errors.New("session broker close failed")
	recoveryFault := &SubmissionRecoveryError{
		RunID: 7, Attempt: "attempt-a", ReceiptID: 42, Story: "US-1", Phase: "recover receipt",
		Err: errors.New("store: pending submissions must retire in sequence"),
	}

	tests := []struct {
		name         string
		factoryErr   error
		wantPhase    string
		wantRecovery bool
	}{
		{
			name:       "generic cleanup failure",
			factoryErr: genericCleanup,
			wantPhase:  "local cleanup/finalization failed",
		},
		{
			name:         "scoped submission recovery failure",
			factoryErr:   recoveryFault,
			wantPhase:    "local submission recovery failed",
			wantRecovery: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := describeFactoryFailure(tc.factoryErr)
			if got == nil {
				t.Fatal("describeFactoryFailure returned nil")
			}
			if !strings.Contains(got.Error(), tc.wantPhase) {
				t.Errorf("error %q does not contain phase %q", got.Error(), tc.wantPhase)
			}
			if !strings.Contains(got.Error(), tc.factoryErr.Error()) {
				t.Errorf("error %q lost the original fault text %q", got.Error(), tc.factoryErr.Error())
			}
			if strings.Contains(got.Error(), "did not start") {
				t.Errorf("post-execution error must never say the agent did not start: %q", got.Error())
			}
			if !errors.Is(got, tc.factoryErr) {
				t.Errorf("describeFactoryFailure(%v) does not wrap the original fault", tc.factoryErr)
			}
			var recoveryErr *SubmissionRecoveryError
			if errors.As(got, &recoveryErr) != tc.wantRecovery {
				t.Errorf("errors.As(*SubmissionRecoveryError) = %t, want %t", !tc.wantRecovery, tc.wantRecovery)
			}
		})
	}
}

// TestSubmissionRecoveryErrorSurfacesThroughSessionCloseAndJoin exercises the
// real production seam (issue #311, AC-005): a genuinely broken pending
// receipt scoped to a session's own run/attempt (the same fixture shape as
// TestRecoverSubmissionsForAttemptIsolatesUnrelatedRuns for issue #310) makes
// SubmissionSession.Close fail for real; joinSessionCleanup (submission_runner.go)
// combines that with the runner's own nil error exactly as runSubmissionBound's
// defer does, and describeFactoryFailure (harvest.go) must still resolve the
// joined result to a *SubmissionRecoveryError rather than a generic cleanup
// fault, correctly attributing US-002's now-isolated recovery error instead of
// misreporting it as the agent never starting.
func TestSubmissionRecoveryErrorSurfacesThroughSessionCloseAndJoin(t *testing.T) {
	f := newFixture(t, nil)
	typedProgressScope(t, f, store.WorkflowSeparated)

	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	var story store.ApprovedStoryRevision
	for _, r := range revisions {
		if r.Key == "US-1" {
			story = r
		}
	}
	if story.Key == "" {
		t.Fatal("fixture did not approve story US-1")
	}

	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "submission-recovery", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID, Stories: []agent.ApprovedStory{{Key: story.Key, IssueID: story.IssueID, IssueNumber: story.IssueNumber, ParentID: story.ParentID, Revision: story.Hash, Title: story.Title, Body: story.Body, State: story.State, Dependencies: story.Dependencies}}}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundle, worktree))
	session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: worktree, Bundle: bundle})
	mustProgress(t, err)

	// Receipt 1 is decided directly, bypassing the broker's own sequence
	// bookkeeping (last_sequence stays 0).
	decided, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: run.ID, Attempt: session.binding.Attempt, RequestID: "decided", Story: story.Key, Revision: story.Hash, InputState: "valid", Payload: []byte(`{}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	mustProgress(t, f.db.DecideSubmission(t.Context(), decided.ID, store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "not_applicable", Effects: "not_applicable", Plan: "not_applicable"}))

	// Receipt 2 consumes the next sequence slot and is left genuinely
	// pending with a payload that fails contribution parsing, so recovering
	// it at Close hits the sequence gap left by receipt 1 and fails for real.
	broken, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: run.ID, Attempt: session.binding.Attempt, RequestID: "broken", Story: story.Key, Revision: story.Hash, InputState: "valid", Payload: []byte(`{"not":"a contribution"}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)

	closeErr := session.Close(t.Context())
	if closeErr == nil {
		t.Fatal("session.Close silently accepted a genuinely broken pending receipt")
	}
	var recoveryErr *SubmissionRecoveryError
	if !errors.As(closeErr, &recoveryErr) {
		t.Fatalf("session.Close error did not carry a *SubmissionRecoveryError: %v", closeErr)
	}
	if recoveryErr.RunID != run.ID || recoveryErr.Attempt != session.binding.Attempt || recoveryErr.ReceiptID != broken.ID || recoveryErr.Story != story.Key {
		t.Fatalf("recovery error identified the wrong origin: %+v", recoveryErr)
	}

	// runSubmissionBound's defer joins exactly this way (submission_runner.go);
	// the runner itself had no error of its own, because the process already
	// produced a real result — this is the "successful execution, cleanup
	// failed" shape (AC-005), reachable here without needing the full
	// subprocess-simulation pipeline.
	joined := joinSessionCleanup(nil, closeErr)
	if !errors.As(joined, &recoveryErr) {
		t.Fatalf("joinSessionCleanup lost the *SubmissionRecoveryError: %v", joined)
	}

	described := describeFactoryFailure(joined)
	switch {
	case described == nil:
		t.Fatal("describeFactoryFailure returned nil")
	case strings.Contains(described.Error(), "did not start"):
		t.Errorf("a run that actually executed must never be reported as never having started: %q", described.Error())
	case !strings.Contains(described.Error(), "local submission recovery"):
		t.Errorf("US-002's recovery error must be attributed to the recovery phase: %q", described.Error())
	}
	var attributed *SubmissionRecoveryError
	if !errors.As(described, &attributed) || attributed.ReceiptID != broken.ID {
		t.Fatalf("describeFactoryFailure lost the original recovery error's identity: %+v", attributed)
	}
}
