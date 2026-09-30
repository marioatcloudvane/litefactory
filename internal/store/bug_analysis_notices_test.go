package store

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// TestApplyBugAnalysis_PolicyNoticeRecordsFactWithoutBlocking is story-002's
// downgraded-validator contract: a policy-class finding (here, boundaries
// listing a duplicate path) does not refuse the analysis on its success edge.
// It records exactly one fact and leaves the pass/analysis-used bookkeeping
// exactly where a clean analysis would have left it.
func TestApplyBugAnalysis_PolicyNoticeRecordsFactWithoutBlocking(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 470)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))

	invocation := "invocation-1"
	inputDigest := submissionDigest([]byte("analysis-input-1"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), BugAnalysisPass{SessionID: session.ID, Invocation: invocation, InputDigest: inputDigest, GraphAttemptID: attempt.ID}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), session.ID, invocation, inputDigest, run.ID); err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}

	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Workflow: "bug", Task: session.TaskRef, WorkContext: "bug-analysis", Attempt: invocation, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	analysis := bugFixtureAnalysis()
	// A duplicate allowed path is policy, not integrity: Validate returns a
	// Notice for it rather than an error (buganalysis.boundaries_duplicate).
	analysis.Boundaries = &buganalysis.BugRepairBoundaries{AllowedPaths: []string{"internal/widgets/handler.go", "internal/widgets/handler.go"}}
	var snapshot bugFixtureSnapshot
	snapshot.Result.Outcome = "completed"
	snapshot.Result.Payload.Bug = bugFixtureSnapshotBug{Response: "done", Invocation: invocation, BaseRevision: 0, Analysis: &analysis}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.RecordResult(t.Context(), ResultRecord{Binding: binding, RequestID: "req-1", Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: raw, Retired: []byte(`null`), Accepted: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}

	exit := int64(0)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exit, FinishedAt: bugFixtureTime(0)}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, invocation, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	outputPayload, err := json.Marshal(struct {
		ResultID       int64 `json:"result_id"`
		ResultRevision int64 `json:"result_revision"`
	}{binding.ResultID, receipt.AppliedRevision})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: bugFixtureTime(0), Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: outputPayload}}}); err != nil {
		t.Fatal(err)
	}

	stateBefore, _, _, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}

	revision, err := db.ApplyBugAnalysis(t.Context(), BugAnalysisApplication{
		SessionID: session.ID, ExpectedRevision: bugCurrentRevision(t, db, session.ID), Invocation: invocation, Operation: "apply-1",
		InputDigest: submissionDigest([]byte("apply-1")), ResultID: binding.ResultID, ResultRevision: receipt.AppliedRevision,
	})
	if err != nil {
		t.Fatalf("apply bug analysis with a policy notice: %v", err)
	}
	if !revision.ReviewReady {
		t.Fatal("a policy notice refused the analysis instead of continuing on the success edge")
	}

	stateAfter, _, _, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stateAfter.AnalysisUsed != stateBefore.AnalysisUsed {
		t.Fatalf("analysis_used moved from %d to %d: a policy notice touched the pass budget", stateBefore.AnalysisUsed, stateAfter.AnalysisUsed)
	}
	if stateAfter.CurrentAnalysis != 1 {
		t.Fatalf("current_analysis = %d, want 1", stateAfter.CurrentAnalysis)
	}

	facts, err := db.BugFacts(t.Context(), session.ID, BugFactStageAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("facts = %d, want exactly 1 recorded for the duplicate-path notice", len(facts))
	}
	if facts[0].Reference != "buganalysis.boundaries_duplicate:internal/widgets/handler.go" {
		t.Fatalf("fact reference = %q", facts[0].Reference)
	}
}

// TestApplyBugAnalysis_IntegrityFaultStillHardErrors pins story-002's other
// half: only policy-class findings were downgraded to facts. A genuine
// integrity fault — a remedy boundary path escaping the repository — remains
// a hard error, refusing the analysis rather than becoming a Notice.
func TestApplyBugAnalysis_IntegrityFaultStillHardErrors(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	session, _ := bugFixtureSession(t, db, 471)
	def := bugFixtureDefinition(t, db, "a")
	instance := bugFixtureInstance(t, db, session, def, "analyst", bugFixtureTime(0))
	run := bugFixtureRun(t, db, session, 1, bugFixtureTime(0))
	attempt := bugFixtureAttempt(t, db, instance, "analyst", run.ID, bugFixtureTime(0))

	invocation := "invocation-1"
	inputDigest := submissionDigest([]byte("analysis-input-1"))
	if _, err := db.ReserveBugAnalysisPass(t.Context(), BugAnalysisPass{SessionID: session.ID, Invocation: invocation, InputDigest: inputDigest, GraphAttemptID: attempt.ID}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(t.Context(), session.ID, invocation, inputDigest, run.ID); err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	binding, err := db.BindResultAttempt(t.Context(), ResultAttempt{Workflow: "bug", Task: session.TaskRef, WorkContext: "bug-analysis", Attempt: invocation, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	analysis := bugFixtureAnalysis()
	// An escaping path is an integrity fault (could name something outside the
	// project), never a policy notice.
	analysis.Boundaries = &buganalysis.BugRepairBoundaries{AllowedPaths: []string{"../outside/the/repo.go"}}
	var snapshot bugFixtureSnapshot
	snapshot.Result.Outcome = "completed"
	snapshot.Result.Payload.Bug = bugFixtureSnapshotBug{Response: "done", Invocation: invocation, BaseRevision: 0, Analysis: &analysis}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := db.RecordResult(t.Context(), ResultRecord{Binding: binding, RequestID: "req-1", Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: raw, Retired: []byte(`null`), Accepted: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	exit := int64(0)
	if _, err := db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exit, FinishedAt: bugFixtureTime(0)}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, invocation, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	outputPayload, err := json.Marshal(struct {
		ResultID       int64 `json:"result_id"`
		ResultRevision int64 `json:"result_revision"`
	}{binding.ResultID, receipt.AppliedRevision})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "success", FinishedAt: bugFixtureTime(0), Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: outputPayload}}}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ApplyBugAnalysis(t.Context(), BugAnalysisApplication{
		SessionID: session.ID, ExpectedRevision: bugCurrentRevision(t, db, session.ID), Invocation: invocation, Operation: "apply-1",
		InputDigest: submissionDigest([]byte("apply-1")), ResultID: binding.ResultID, ResultRevision: receipt.AppliedRevision,
	}); err == nil {
		t.Fatal("an escaping remedy boundary path was accepted instead of refused")
	}

	facts, err := db.BugFacts(t.Context(), session.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 0 {
		t.Fatalf("facts = %d, want 0: an integrity fault must not be recorded as a downgraded notice", len(facts))
	}
}
