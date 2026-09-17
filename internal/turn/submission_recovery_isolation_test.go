package turn

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// brokenPendingRun registers a run bound to story, deliberately leaving a
// pending receipt behind that recovery genuinely fails on: a receipt is
// decided directly (bypassing the broker, so story_progress's own
// last_sequence is never advanced), and a second receipt is left pending
// with a payload that fails contribution parsing. Recovering the pending
// receipt then finds a real sequence gap and fails for real, not a
// gracefully handled rejection — the same fixture shape
// TestRecoverSubmissionsForAttemptIsolatesUnrelatedRuns (issue #310) uses.
func brokenPendingRun(t *testing.T, f *fixture, comment int64, story store.ApprovedStoryRevision) (*SubmissionSession, store.SubmissionReceipt) {
	t.Helper()
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "broken-"+story.Key, store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: comment, LogPath: func(int) string { return "run.log" }})
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID, Stories: []agent.ApprovedStory{{Key: story.Key, IssueID: story.IssueID, IssueNumber: story.IssueNumber, ParentID: story.ParentID, Revision: story.Hash, Title: story.Title, Body: story.Body, State: story.State, Dependencies: story.Dependencies}}}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundle, worktree))
	session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: worktree, Bundle: bundle})
	mustProgress(t, err)

	decided, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: run.ID, Attempt: session.binding.Attempt, RequestID: "decided", Story: story.Key, Revision: story.Hash, InputState: "valid", Payload: []byte(`{}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	mustProgress(t, f.db.DecideSubmission(t.Context(), decided.ID, store.SubmissionDecision{Engineering: "not_applicable", Acceptance: "not_applicable", Effects: "not_applicable", Plan: "not_applicable"}))

	broken, err := f.db.AllocateSubmission(t.Context(), store.SubmissionRequest{RunID: run.ID, Attempt: session.binding.Attempt, RequestID: "broken", Story: story.Key, Revision: story.Hash, InputState: "valid", Payload: []byte(`{"not":"a contribution"}`), Diagnostics: []byte(`{"valid":true}`)})
	mustProgress(t, err)
	return session, broken
}

func approvedStoriesByKey(t *testing.T, f *fixture) map[string]store.ApprovedStoryRevision {
	t.Helper()
	revisions, err := f.db.ListApprovedStoryRevisions(t.Context(), f.session.TaskRef, 1)
	mustProgress(t, err)
	byKey := map[string]store.ApprovedStoryRevision{}
	for _, r := range revisions {
		byKey[r.Key] = r
	}
	if byKey["US-1"].Key == "" || byKey["US-2"].Key == "" {
		t.Fatal("fixture did not approve both scoped stories")
	}
	return byKey
}

// TestCheckpointLockedIsolatesUnrelatedBrokenReceipt is issue #310's AC-003
// for the foreground submission/checkpoint call site specifically (as
// opposed to session.Close, already covered by
// TestRecoverSubmissionsForAttemptIsolatesUnrelatedRuns): a genuinely broken
// pending receipt belonging to an unrelated run must never fail a healthy
// run's own live "submit" checkpoint, and that healthy run's own correlated
// receipt is unaffected.
func TestCheckpointLockedIsolatesUnrelatedBrokenReceipt(t *testing.T) {
	f := newFixture(t, nil)
	typedProgressScope(t, f, store.WorkflowSeparated)
	stories := approvedStoriesByKey(t, f)

	// An unrelated run, deliberately broken, sharing nothing with the run
	// under test but the same managed feature/session.
	brokenSession, _ := brokenPendingRun(t, f, 1, stories["US-1"])
	t.Cleanup(func() { _ = brokenSession.Close(t.Context()) })

	// The healthy run under test.
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "healthy", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "run-healthy.log" }})
	mustProgress(t, err)
	story := stories["US-2"]
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: run.ID, Stories: []agent.ApprovedStory{{Key: story.Key, IssueID: story.IssueID, IssueNumber: story.IssueNumber, ParentID: story.ParentID, Revision: story.Hash, Title: story.Title, Body: story.Body, State: story.State, Dependencies: story.Dependencies}}}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundle, worktree))
	session, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: run.ID, Worktree: worktree, Bundle: bundle})
	mustProgress(t, err)
	t.Cleanup(func() { _ = session.Close(t.Context()) })

	contribution := capturedContribution(t, worktree, story.Key, "engineering")
	payload, err := json.Marshal(contribution)
	mustProgress(t, err)
	result, err := session.checkpoint(t.Context(), submission.Request{Method: "submit", RequestID: "healthy-submit", Payload: payload})
	if err != nil {
		t.Fatalf("a healthy run's own foreground checkpoint failed because of an unrelated run's broken receipt: %v", err)
	}
	var receipt store.SubmissionReceipt
	mustProgress(t, json.Unmarshal(result, &receipt))
	if receipt.EngineeringState != "recorded" || receipt.RunID != run.ID {
		t.Fatalf("the isolated run's own correlated receipt was affected: %+v", receipt)
	}
}

// TestCheckpointLockedStillSurfacesItsOwnGenuineFailure is the other half of
// AC-003: scoping recovery narrows *whose* failures propagate, it does not
// suppress a run's own real failures. A run with its own genuinely broken
// pending receipt must still see that failure through its own foreground
// checkpoint call, correctly attributed to itself and not to any other run.
func TestCheckpointLockedStillSurfacesItsOwnGenuineFailure(t *testing.T) {
	f := newFixture(t, nil)
	typedProgressScope(t, f, store.WorkflowSeparated)
	stories := approvedStoriesByKey(t, f)

	// An unrelated broken run, present purely to prove the failure below is
	// attributed to the right one.
	unrelated, unrelatedBroken := brokenPendingRun(t, f, 1, stories["US-1"])
	t.Cleanup(func() { _ = unrelated.Close(t.Context()) })

	ownStory := stories["US-2"]
	ownSession, ownBroken := brokenPendingRun(t, f, 2, ownStory)
	t.Cleanup(func() { _ = ownSession.Close(t.Context()) })

	// The payload must select one of this session's own authorized stories,
	// so checkpointLocked reaches its RecoverSubmissionsForAttempt call
	// instead of returning early on an unrecognized selector.
	payload := json.RawMessage(`{"stage":"engineering","story":"` + ownStory.Key + `","outcome":"blocked","reason":"probe"}`)
	_, err := ownSession.checkpoint(t.Context(), submission.Request{Method: "submit", RequestID: "next", Payload: payload})
	if err == nil {
		t.Fatal("a run's own foreground checkpoint silently accepted its own broken pending receipt")
	}
	var recoveryErr *SubmissionRecoveryError
	if !errors.As(err, &recoveryErr) {
		t.Fatalf("own-run checkpoint error did not identify its origin: %v", err)
	}
	if recoveryErr.RunID != ownSession.binding.RunID || recoveryErr.ReceiptID != ownBroken.ID {
		t.Fatalf("own-run checkpoint attributed the failure to the wrong receipt: %+v (want run %d receipt %d)", recoveryErr, ownSession.binding.RunID, ownBroken.ID)
	}
	if recoveryErr.RunID == unrelated.binding.RunID || recoveryErr.ReceiptID == unrelatedBroken.ID {
		t.Fatalf("own-run checkpoint attributed the failure to the unrelated run instead: %+v", recoveryErr)
	}
}

// TestConsumeTerminalValidationIsolatesUnrelatedBrokenReceipt is issue
// #310's AC-003 for terminal Validate intake specifically: an unrelated
// run's genuinely broken pending receipt must never abort a healthy
// Validate session's terminal contribution intake.
func TestConsumeTerminalValidationIsolatesUnrelatedBrokenReceipt(t *testing.T) {
	f := newFixture(t, nil)
	typedProgressScope(t, f, store.WorkflowSeparated)
	stories := approvedStoriesByKey(t, f)

	brokenSession, _ := brokenPendingRun(t, f, 1, stories["US-1"])
	t.Cleanup(func() { _ = brokenSession.Close(t.Context()) })

	// A complete engineering->validate flow for the second, healthy story.
	story := stories["US-2"]
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "validate-healthy", store.AgentClaude)
	mustProgress(t, err)
	engRun, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 2, LogPath: func(int) string { return "run-eng.log" }})
	mustProgress(t, err)
	bundle := &agent.ApprovedBundle{WorkflowVersion: store.WorkflowSeparated, Parent: f.session.TaskRef, Generation: 1, RunID: engRun.ID, Stories: []agent.ApprovedStory{{Key: story.Key, IssueID: story.IssueID, IssueNumber: story.IssueNumber, ParentID: story.ParentID, Revision: story.Hash, Title: story.Title, Body: story.Body, State: story.State, Dependencies: story.Dependencies}}}
	worktree := t.TempDir()
	mustProgress(t, f.opts.StoryProgress.Register(t.Context(), bundle, worktree))
	engSession, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: engRun.ID, Worktree: worktree, Bundle: bundle})
	mustProgress(t, err)
	callContribution(t, engSession, "submit", capturedContribution(t, worktree, story.Key, "engineering"))
	retained, err := f.db.EngineeringEvidence(t.Context(), f.session.TaskRef, 1, story.Key)
	mustProgress(t, err)
	event, err := agent.ParseStoryEvent(retained.Payload)
	mustProgress(t, err)
	mustProgress(t, engSession.Close(t.Context()))
	_, err = f.db.FinishRun(t.Context(), engRun.ID, store.RunOutcome{Status: store.RunSucceeded})
	mustProgress(t, err)

	validateRun, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "validate", Agent: store.AgentClaude, TriggerCommentID: 3, LogPath: func(int) string { return "run-validate.log" }})
	mustProgress(t, err)
	validation := &agent.ValidationContext{Version: 1, Parent: f.session.TaskRef, Generation: 1, Stories: []agent.ValidationStory{{Story: bundle.Stories[0], Criteria: []string{"AC-001"}, Snapshot: event.Evidence.ImplementationDigest, Evidence: event.Evidence, EvidenceStatus: "satisfied"}}}
	validateSession, err := StartSubmissionSession(t.Context(), SubmissionSessionOptions{DB: f.db, Progress: f.opts.StoryProgress, Paths: f.opts.Paths, RunID: validateRun.ID, Worktree: worktree, Validation: validation})
	mustProgress(t, err)
	t.Cleanup(func() { _ = validateSession.Close(t.Context()) })

	finding := agent.ContributionFinding{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{event.Evidence.Tests[0].Name}, Summary: "Retained scoped execution passed."}}}
	c := agent.Contribution{Stage: "validate", Story: story.Key, Outcome: "assessed", Assessment: &agent.AssessmentContribution{SnapshotID: event.Evidence.ImplementationDigest, Criteria: []agent.ContributionFinding{finding}, Reviews: []agent.ContributionFinding{}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		f := finding
		f.Name = name
		c.Assessment.Reviews = append(c.Assessment.Reviews, f)
	}
	payload, err := json.Marshal(c)
	mustProgress(t, err)
	_, err = validateSession.checkpoint(t.Context(), submission.Request{Method: "submit", RequestID: "live-checkpoint", Payload: payload})
	mustProgress(t, err)

	terminal, err := json.Marshal(c)
	mustProgress(t, err)
	output := `{"contributions":[` + string(terminal) + `]}`
	if err := validateSession.consumeTerminalValidation(t.Context(), output); err != nil {
		t.Fatalf("terminal Validate intake failed because of an unrelated run's broken pending receipt: %v", err)
	}

	receipts, err := f.db.ValidationCheckpoints(t.Context(), validateRun.ID, validateSession.binding.Attempt, story.Key, story.Hash)
	mustProgress(t, err)
	if len(receipts) != 1 || receipts[0].AcceptanceState != "accepted" {
		t.Fatalf("the isolated Validate session's own authoritative decision was affected: %+v", receipts)
	}
}
