package daemon_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func bugDaemonDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// Establish prior factory observations through public APIs; the exercised path
// is the real daemon Tick against its local HTTP forge fixture, not an analyst.
func bugDaemonReview(t *testing.T, f *fixture, published bool) {
	t.Helper()
	session := f.session()
	db := f.db
	ctx := t.Context()
	if _, err := db.AdmitWorkflow(ctx, store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureBugRecord(ctx, store.BugIdentity{SessionID: session.ID, Repository: testOwnerName + "/" + testRepoName, IssueID: 10123, IssueNumber: testIssue, OwnerID: ownerID}); err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "bug-analysis", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: f.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	for n := range 8 {
		instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "analyst", StartedAt: f.clock.Now()})
		if err != nil {
			t.Fatal(err)
		}
		run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: int64(n + 1), LogPath: func(ordinal int) string { return fmt.Sprintf("/fixture/bug-%d", ordinal) }})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: run.ID, StartedAt: f.clock.Now()})
		if err != nil {
			t.Fatal(err)
		}
		state, _, _, err := db.CurrentBugAnalysis(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		invocation := fmt.Sprintf("analysis-%d", n+1)
		if _, err := db.ReserveBugAnalysisPass(ctx, store.BugAnalysisPass{SessionID: session.ID, Invocation: invocation, InputDigest: strings.Repeat("b", 64), GraphAttemptID: attempt.ID}, state.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ClaimBugAnalysisDispatch(ctx, session.ID, invocation, strings.Repeat("b", 64), run.ID); err != nil {
			t.Fatal(err)
		}
		if n < 7 {
			exit := int64(1)
			if _, err := db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, ExitCode: &exit, Error: "prior investigation unsuccessful"}); err != nil {
				t.Fatal(err)
			}
			if err := db.FinishBugAnalysisPass(ctx, session.ID, invocation, "failed", "prior investigation unsuccessful"); err != nil {
				t.Fatal(err)
			}
			if err := db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: f.clock.Now(), InstanceStatus: store.GraphFailed}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		scope := ar.Context{Workflow: agent.WorkflowBug, SchemaVersion: 1, WorkflowVersion: 2, QuestionsAllowed: true, Bug: &ar.BugBinding{Role: "bug-analyst", Invocation: invocation}}
		service, err := turn.BindAgentResultService(ctx, turn.AgentResultServiceOptions{DB: db, Attempt: store.ResultAttempt{Task: session.TaskRef, Workflow: "bug", WorkContext: invocation, Attempt: invocation, RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(context.Context) (ar.Context, error) { return scope, nil }, ReadTargets: ar.EditTargets("bug"), EditTargets: ar.EditTargets("bug")})
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte(`{"result":{"outcome":"completed","comment":"Review exact repair analysis","payload":{"bug":{"response":"done","invocation":"analysis-8","base_revision":0,"analysis":{"expected":"Saved value returned","actual":"Value missing","reproduction":{"steps":["Save and read"],"environment":"controlled fixture","observation":"owner_reported"},"cause":"Key normalization differs","evidence":["owner observation"],"remedy":"Normalize lookup key","exclusions":["No schema change"],"criteria":[{"id":"AC-1","requirement":"Read saved value","measurement":"Regression test returns value"}]}}}}}`)
		submitted, err := service.Submit(ctx, "analysis-result", raw)
		if err != nil || !submitted.Validation.Valid {
			t.Fatalf("seed result: %+v %v", submitted, err)
		}
		exit := int64(0)
		if _, err := db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exit}); err != nil {
			t.Fatal(err)
		}
		if err := db.FinishBugAnalysisPass(ctx, session.ID, invocation, "succeeded", ""); err != nil {
			t.Fatal(err)
		}
		if err := db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.clock.Now(), Outputs: []store.GraphOutput{{Kind: "json", Payload: []byte(fmt.Sprintf(`{"result_id":%d,"result_revision":1}`, service.Binding().ResultID))}}}); err != nil {
			t.Fatal(err)
		}
		state, _, _, err = db.CurrentBugAnalysis(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		revision, err := db.ApplyBugAnalysis(ctx, store.BugAnalysisApplication{SessionID: session.ID, ExpectedRevision: state.Revision, Invocation: invocation, Operation: "record-analysis", InputDigest: bugDaemonDigest(raw), ResultID: service.Binding().ResultID, ResultRevision: 1})
		if err != nil {
			t.Fatal(err)
		}
		key := "bug-review"
		body := "Review " + revision.Digest
		if err := db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: key, InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "review", Kind: "review", Candidate: json.RawMessage(`{"review":"exact analysis"}`)}); err != nil {
			t.Fatal(err)
		}
		state, _, _, err = db.CurrentBugAnalysis(ctx, session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.RetainBugAnalysisReview(ctx, store.BugAnalysisReview{SessionID: session.ID, Review: "review-1", AnalysisRevision: 1, AnalysisDigest: revision.Digest, DeliveryKey: key, ContentDigest: bugDaemonDigest([]byte(body))}, state.Revision); err != nil {
			t.Fatal(err)
		}
		if !published {
			return
		}
		if _, err := db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: key, SessionID: session.ID, RunID: run.ID, TriggerID: run.TriggerCommentID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "graph", Purpose: "review", BotUserID: botID, Body: body, Marker: "fixture"}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ConfirmReportDelivery(ctx, key, 400); err != nil {
			t.Fatal(err)
		}
		if _, err := db.CompleteReportDelivery(ctx, key); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ConfirmBugAnalysisReview(ctx, session.ID, "review-1", botID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBugAnalysisBoundaryTickMechanicalEighthPassApproval(t *testing.T) {
	for _, variant := range []string{"exact", "wrong owner", "stale", "unpublished", "mixed"} {
		t.Run(variant, func(t *testing.T) {
			f := newFixture(t)
			f.plantCursor()
			f.issueWithLabels("Bug approval", "bug")
			f.settle()
			bugDaemonReview(t, f, variant != "unpublished")
			session := f.session()
			before, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			author := owner()
			body := "/approve-bug analysis 1"
			switch variant {
			case "wrong owner":
				author = stranger()
			case "stale":
				body = "/approve-bug analysis 2"
			case "mixed":
				body += " but change remedy"
			}
			f.forge.addComment(testIssue, 501, author, body)
			f.settle()
			f.settle()
			after, err := f.db.ListRuns(t.Context(), session.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("approval dispatched an analyst: %v", err)
			}
			if calls := f.turns.triggers(); len(calls) != 0 {
				t.Fatalf("approval called feature executor: %v", calls)
			}
			state, _, approval, err := f.db.CurrentBugAnalysis(t.Context(), session.ID)
			if err != nil || state.AnalysisUsed != 8 {
				t.Fatalf("approval changed analysis budget: %+v %v", state, err)
			}
			if (approval != nil) != (variant == "exact") {
				t.Fatalf("variant %s approval=%+v logs=%s", variant, approval, f.logs)
			}
			if approval != nil && approval.ReplyID != 501 {
				t.Fatalf("wrong reply consumed: %+v", approval)
			}
		})
	}
}
