package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

// Shared fixtures for the six-table Bug store (migrations 0079/0080). Every
// helper here drives the real public (or, where the schema's own internal
// wiring has no public seam, package-private) store API exactly the way a
// turn activity would, so the journeys under test exercise genuine behavior
// rather than hand-inserted rows standing in for it.

// bugFixtureTime returns a fixed instant offset for deterministic ordering.
func bugFixtureTime(offset time.Duration) time.Time {
	return time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC).Add(offset)
}

// bugFixtureSession creates a repo/session pair admitted into the standalone
// Bug workflow, with its canonical bug_tasks identity already recorded.
func bugFixtureSession(t *testing.T, db *DB, issue int) (Session, BugIdentity) {
	t.Helper()
	session := testSession(t, db, issue)
	if _, err := db.AdmitWorkflow(t.Context(), TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatalf("admit bug workflow: %v", err)
	}
	identity := BugIdentity{SessionID: session.ID, Repository: "acme/api", IssueID: int64(issue) + 5000, IssueNumber: int64(issue), OwnerID: 501}
	if err := db.EnsureBugRecord(t.Context(), identity); err != nil {
		t.Fatalf("ensure bug record: %v", err)
	}
	return session, identity
}

// bugFixtureRun allocates a Bug-workflow run (mode "", workflow "bug") on a
// single shared conversation segment for the session.
func bugFixtureRun(t *testing.T, db *DB, session Session, trigger int64, at time.Time) Run {
	t.Helper()
	segment, err := db.GetOrCreateSegment(t.Context(), session.ID, FirstSegmentOrdinal, "bug-segment", AgentClaude)
	if err != nil {
		t.Fatalf("bug segment: %v", err)
	}
	run, err := db.AllocateRun(t.Context(), AllocateRunParams{
		Workflow: "bug", SessionID: session.ID, SegmentID: segment.ID, Agent: AgentClaude,
		TriggerCommentID: trigger, StartedAt: at,
		LogPath: func(n int) string { return fmt.Sprintf("/logs/bug-%d.jsonl", n) },
	})
	if err != nil {
		t.Fatalf("allocate bug run: %v", err)
	}
	return run
}

// bugFixtureDefinition creates one Bug-workflow graph definition, distinct per
// digestChar so a test can hold both a parent and a child definition.
func bugFixtureDefinition(t *testing.T, db *DB, digestChar string) GraphDefinition {
	t.Helper()
	d, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "core", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat(digestChar, 64), CreatedAt: bugFixtureTime(0)})
	if err != nil {
		t.Fatalf("bug graph definition: %v", err)
	}
	return d
}

func bugFixtureInstance(t *testing.T, db *DB, session Session, def GraphDefinition, node string, at time.Time) GraphInstance {
	t.Helper()
	i, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: node, StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatalf("bug graph instance: %v", err)
	}
	return i
}

func bugFixtureAttempt(t *testing.T, db *DB, instance GraphInstance, node string, runID int64, at time.Time) GraphNodeAttempt {
	t.Helper()
	a, err := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: instance.ID, Node: node, RunID: runID, StartedAt: at})
	if err != nil {
		t.Fatalf("bug graph attempt %s: %v", node, err)
	}
	return a
}

// bugFixtureLifecycle pins the parent envelope EnsureBugLifecycle expects: a
// running root instance plus a distinct child definition, both Bug-workflow.
func bugFixtureLifecycle(t *testing.T, db *DB, session Session, instance GraphInstance, child GraphDefinition) BugLifecycle {
	t.Helper()
	l, err := db.EnsureBugLifecycle(t.Context(), session.ID, instance.ID, child.ID)
	if err != nil {
		t.Fatalf("ensure bug lifecycle: %v", err)
	}
	return l
}

// bugCurrentRevision reads the live optimistic revision, so callers never have
// to hand-track every write's side-effect bump.
func bugCurrentRevision(t *testing.T, db *DB, session int64) int64 {
	t.Helper()
	state, _, _, err := db.CurrentBugAnalysis(t.Context(), session)
	if err != nil {
		t.Fatalf("current bug analysis: %v", err)
	}
	return state.Revision
}

// bugFixtureAnalysis is a minimal review-ready analysis proposal.
func bugFixtureAnalysis() buganalysis.BugAnalysis {
	return buganalysis.BugAnalysis{
		Expected:     "the widgets endpoint returns 200",
		Actual:       "the widgets endpoint returns 500",
		Reproduction: buganalysis.BugReproduction{Steps: []string{"call GET /widgets"}, Environment: "staging", Observation: "observed"},
		Cause:        "nil pointer in the widget handler",
		Evidence:     []string{"panic trace at widgets.go:42"},
		Remedy:       "guard against a nil widget store",
		Exclusions:   []string{"not a database migration issue"},
		Criteria:     []buganalysis.BugCriterion{{ID: "AC-001", Requirement: "endpoint returns 200", Measurement: "integration test asserts status code"}},
	}
}

// bugFixtureSnapshotBug and bugFixtureSnapshot mirror the unexported
// bugRetainedAnalysis/proposal JSON shape ApplyBugAnalysis decodes, by field
// name rather than by Go type identity (an anonymous struct field cannot be
// named from outside its declaration).
type bugFixtureSnapshotBug struct {
	Evidence     []string                 `json:"evidence"`
	Response     string                   `json:"response"`
	Invocation   string                   `json:"invocation"`
	BaseRevision int64                    `json:"base_revision"`
	Analysis     *buganalysis.BugAnalysis `json:"analysis,omitempty"`
}
type bugFixtureSnapshot struct {
	Result struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
		Payload struct {
			Bug bugFixtureSnapshotBug `json:"bug"`
		} `json:"payload"`
	} `json:"result"`
}

// bugFixtureApprovedAnalysis drives one full analysis pass — reservation,
// dispatch, shared-result submission, application and owner review/approval —
// leaving bug_tasks.current_analysis at revision 1 with an active, approved
// review any repair reservation can cite. It returns that revision and
// approval, and the analyst's own graph attempt (already terminal).
func bugFixtureApprovedAnalysis(t *testing.T, db *DB, session Session, identity BugIdentity, instance GraphInstance, analystAttempt GraphNodeAttempt, run Run, at time.Time) (BugAnalysisRevision, BugAnalysisApproval) {
	t.Helper()
	ctx := t.Context()
	invocation := "invocation-1"
	inputDigest := submissionDigest([]byte("analysis-input-1"))

	if _, err := db.ReserveBugAnalysisPass(ctx, BugAnalysisPass{SessionID: session.ID, Invocation: invocation, InputDigest: inputDigest, GraphAttemptID: analystAttempt.ID}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("reserve analysis pass: %v", err)
	}
	if claimed, err := db.ClaimBugAnalysisDispatch(ctx, session.ID, invocation, inputDigest, run.ID); err != nil || !claimed {
		t.Fatalf("claim analysis dispatch: claimed=%v err=%v", claimed, err)
	}

	binding, err := db.BindResultAttempt(ctx, ResultAttempt{Workflow: "bug", Task: session.TaskRef, WorkContext: "bug-analysis", Attempt: invocation, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatalf("bind result attempt: %v", err)
	}
	analysis := bugFixtureAnalysis()
	var snapshot bugFixtureSnapshot
	snapshot.Result.Outcome = "completed"
	snapshot.Result.Payload.Bug = bugFixtureSnapshotBug{Response: "done", Invocation: invocation, BaseRevision: 0, Analysis: &analysis}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	receipt, err := db.RecordResult(ctx, ResultRecord{Binding: binding, RequestID: "req-1", Payload: []byte(`{}`), Diagnostics: []byte(`{}`), Snapshot: raw, Retired: []byte(`null`), Accepted: true, ExpectedRevision: 0})
	if err != nil {
		t.Fatalf("record result: %v", err)
	}

	exit := int64(0)
	if _, err := db.FinishRun(ctx, run.ID, RunOutcome{Status: RunSucceeded, ExitCode: &exit, FinishedAt: at}); err != nil {
		t.Fatalf("finish analyst run: %v", err)
	}
	if err := db.FinishBugAnalysisPass(ctx, session.ID, invocation, "succeeded", ""); err != nil {
		t.Fatalf("finish analysis pass: %v", err)
	}

	outputPayload, err := json.Marshal(struct {
		ResultID       int64 `json:"result_id"`
		ResultRevision int64 `json:"result_revision"`
	}{binding.ResultID, receipt.AppliedRevision})
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	transition := GraphTransition{InstanceID: instance.ID, FromNode: "analyst", ToNode: "attempt", OnOutcome: "success", At: at}
	if err := db.CompleteGraphNodeAttempt(ctx, analystAttempt.ID, GraphCompletion{
		Status: GraphSucceeded, Outcome: "success", FinishedAt: at,
		Outputs:    []GraphOutput{{Kind: GraphOutputJSON, Payload: outputPayload}},
		Transition: &transition,
	}); err != nil {
		t.Fatalf("complete analyst attempt: %v", err)
	}

	revision, err := db.ApplyBugAnalysis(ctx, BugAnalysisApplication{
		SessionID: session.ID, ExpectedRevision: bugCurrentRevision(t, db, session.ID), Invocation: invocation, Operation: "apply-1",
		InputDigest: submissionDigest([]byte("apply-1")), ResultID: binding.ResultID, ResultRevision: receipt.AppliedRevision,
	})
	if err != nil {
		t.Fatalf("apply bug analysis: %v", err)
	}
	if !revision.ReviewReady {
		t.Fatalf("fixture analysis is not review-ready: %+v", revision)
	}

	reviewKey := "review-1"
	reviewBody := "Analysis review: nil pointer fix, please approve."
	if err := db.RetainGraphComment(ctx, GraphCommentIntent{Key: reviewKey, InstanceID: instance.ID, SourceAttemptID: analystAttempt.ID, Node: "analysis_review", Kind: "review", Candidate: []byte(`{"outcome":"review"}`)}); err != nil {
		t.Fatalf("retain review comment: %v", err)
	}
	if _, err := db.EnsureReportDelivery(ctx, ReportDelivery{Key: reviewKey, Version: 1, SessionID: session.ID, RunID: run.ID, TriggerID: run.TriggerCommentID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "bug", Purpose: "analysis-review", BotUserID: 900, Body: reviewBody, Marker: reviewKey}); err != nil {
		t.Fatalf("ensure review delivery: %v", err)
	}
	if _, err := db.MarkReportDeliverySent(ctx, reviewKey); err != nil {
		t.Fatalf("mark review sent: %v", err)
	}
	const reviewCommentID = 5001
	if _, err := db.ConfirmReportDelivery(ctx, reviewKey, reviewCommentID); err != nil {
		t.Fatalf("confirm review delivery: %v", err)
	}
	if _, err := db.CompleteReportDelivery(ctx, reviewKey); err != nil {
		t.Fatalf("complete review delivery: %v", err)
	}

	if err := db.RetainBugAnalysisReview(ctx, BugAnalysisReview{
		SessionID: session.ID, Review: reviewKey, AnalysisRevision: revision.Revision, AnalysisDigest: revision.Digest,
		DeliveryKey: reviewKey, ContentDigest: submissionDigest([]byte(reviewBody)),
	}, bugCurrentRevision(t, db, session.ID)); err != nil {
		t.Fatalf("retain analysis review: %v", err)
	}
	if _, err := db.ConfirmBugAnalysisReview(ctx, session.ID, reviewKey, 900); err != nil {
		t.Fatalf("confirm analysis review: %v", err)
	}
	approval, err := db.ApproveBugAnalysis(ctx, session.ID, reviewKey, GraphOwnerReply{
		InstanceID: instance.ID, OwnerID: identity.OwnerID, AuthorID: identity.OwnerID,
		CommentID: reviewCommentID + 1, QuestionCommentID: reviewCommentID,
		TaskRef: session.TaskRef, SourceRef: session.TaskRef, DeliveryKey: reviewKey,
		Body: fmt.Sprintf("/approve-bug analysis %d", revision.Revision), At: at,
	}, bugCurrentRevision(t, db, session.ID))
	if err != nil {
		t.Fatalf("approve bug analysis: %v", err)
	}
	return revision, approval
}

// bugFixtureRepairRequest builds a BugRepairRequest citing the approved
// analysis, ready to reserve against a running "attempt" graph attempt.
func bugFixtureRepairRequest(session Session, revision BugAnalysisRevision, approval BugAnalysisApproval, request, cause string, attemptID int64, grantReference string) BugRepairRequest {
	return BugRepairRequest{
		SessionID: session.ID, Request: request, AnalysisRevision: revision.Revision, AnalysisDigest: revision.Digest,
		ApprovalReview: approval.Review, Cause: cause, Evidence: "evidence for " + request,
		SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		GraphAttemptID: attemptID, GrantReference: grantReference,
	}
}
