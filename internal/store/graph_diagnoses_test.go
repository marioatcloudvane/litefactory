package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/bugrepair"
)

func graphDiagnosisFixture(t *testing.T) (*DB, string, GraphInstance, Session, GraphDiagnosis, GraphNodeAttempt) {
	t.Helper()
	db, path, _, session := persistentBugStopFixture(t)
	at := graphTestTime()
	definition, e := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 2, Path: "bug-v2.dot", Digest: strings.Repeat("c", 64), CreatedAt: at})
	if e != nil {
		t.Fatal(e)
	}
	inst, e := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "repair", StartedAt: at, NextDueAt: at})
	if e != nil {
		t.Fatal(e)
	}
	segment, e := db.GetOrCreateSegment(t.Context(), session.ID, 1, "diagnosis", AgentClaude)
	if e != nil {
		t.Fatal(e)
	}
	run, e := db.AllocateRun(t.Context(), AllocateRunParams{Workflow: "bug", SessionID: session.ID, SegmentID: segment.ID, Agent: AgentClaude, TriggerCommentID: 10, StartedAt: at, LogPath: func(int) string { return "/logs/diagnosis.jsonl" }})
	if e != nil {
		t.Fatal(e)
	}
	source, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "repair", RunID: run.ID, Limit: 8, StartedAt: at})
	if e != nil {
		t.Fatal(e)
	}
	analysis := buganalysis.BugAnalysis{Expected: "Empty input returns no matches", Actual: "Empty input panics", Reproduction: buganalysis.BugReproduction{Environment: "Go fixture", Observation: "not_observed"}, Cause: "Missing length guard", Evidence: []string{"search.go:12 unchecked input[0] confirms the panic path"}, Remedy: "Add a length guard", Exclusions: []string{"Nonempty matching unchanged"}, Criteria: []buganalysis.BugCriterion{{ID: "C1", Requirement: "Empty input is safe", Measurement: "Call Search with empty input and assert no panic"}}}
	result := agentresult.Result{Outcome: "completed", Comment: "Ready for review", Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "done", Invocation: fmt.Sprintf("analysis:%d:%d", inst.ID, source.ID), Analysis: &analysis}}}
	binding, e := db.BindResultAttempt(t.Context(), ResultAttempt{Workflow: "bug", Task: session.TaskRef, RunID: run.ID, WorkContext: "diagnosis", Attempt: "analyst-1", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := json.Marshal(agentresult.Snapshot{Result: result})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.RecordResult(t.Context(), ResultRecord{Binding: binding, RequestID: "candidate-1", Payload: snapshot, Diagnostics: []byte(`{"valid":true,"complete":true}`), Snapshot: snapshot, Retired: []byte(`[]`), Accepted: true})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded})
	if e != nil {
		t.Fatal(e)
	}
	e = db.CompleteGraphNodeAttempt(t.Context(), source.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "done", FinishedAt: at, InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: inst.ID, FromNode: "repair", ToNode: "conversation", OnOutcome: "done", At: at}})
	if e != nil {
		t.Fatal(e)
	}
	conversation, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "conversation", Limit: 64, StartedAt: at})
	if e != nil {
		t.Fatal(e)
	}
	candidate, e := db.RetainGraphDiagnosis(t.Context(), GraphDiagnosisInput{InstanceID: inst.ID, ConversationAttemptID: conversation.ID, SourceAttemptID: source.ID, ResultID: binding.ResultID, ResultRevision: 1, At: at})
	if e != nil {
		t.Fatal(e)
	}
	return db, path, inst, session, candidate, conversation
}

func parkGraphDiagnosisReview(t *testing.T, db *DB, inst GraphInstance, session Session, candidate GraphDiagnosis, conversation GraphNodeAttempt, published bool) GraphOwnerReply {
	t.Helper()
	at := graphTestTime()
	raw, e := json.Marshal(struct {
		Diagnosis GraphDiagnosis `json:"diagnosis"`
	}{candidate})
	if e != nil {
		t.Fatal(e)
	}
	e = db.ParkGraphNode(t.Context(), GraphStop{Key: "review-1", InstanceID: inst.ID, SourceAttemptID: conversation.ID, Origin: "conversation", Cause: "diagnosis_review", Reason: "Review the current remedy", Candidate: raw, At: at, AllowRunless: true, Completion: &GraphCompletion{Status: GraphSucceeded, Outcome: "waiting", FinishedAt: at, InstanceStatus: GraphRunning}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.EnsureReportDelivery(t.Context(), ReportDelivery{Key: "review-1", SessionID: session.ID, Task: session.TaskRef, Destination: "acme/repo#457", TriggerNamespace: "issue-comment", Purpose: "review", BotUserID: 99, Body: "Review", Marker: "review-1"})
	if e != nil {
		t.Fatal(e)
	}
	if published {
		if _, e = db.MarkReportDeliverySent(t.Context(), "review-1"); e != nil {
			t.Fatal(e)
		}
		if _, e = db.ConfirmReportDelivery(t.Context(), "review-1", 101); e != nil {
			t.Fatal(e)
		}
		if _, e = db.CompleteReportDelivery(t.Context(), "review-1"); e != nil {
			t.Fatal(e)
		}
	}
	return GraphOwnerReply{InstanceID: inst.ID, OwnerID: 7, AuthorID: 7, CommentID: 102, QuestionCommentID: 101, TaskRef: session.TaskRef, SourceRef: session.TaskRef, Workflow: "bug", DeliveryKey: "review-1", Body: "/approve-bug analysis 1", At: at.Add(time.Minute)}
}

func TestGraphDiagnosisPersistentApprovalAndAtomicConsumption(t *testing.T) {
	db, path, inst, session, candidate, conversation := graphDiagnosisFixture(t)
	reply := parkGraphDiagnosisReview(t, db, inst, session, candidate, conversation, true)
	db = reopenBugStopDB(t, db, path)
	before, approval, e := db.CurrentGraphDiagnosis(t.Context(), inst.ID)
	if e != nil || approval != nil || !reflect.DeepEqual(before, candidate) {
		t.Fatalf("candidate history changed: %+v %v %v", before, approval, e)
	}
	a, e := db.ApproveGraphDiagnosis(t.Context(), reply, 1)
	if e != nil {
		t.Fatal(e)
	}
	if a.Digest != candidate.Digest || a.Review != "review-1" || a.OwnerID != 7 {
		t.Fatalf("approval mismatch: %+v", a)
	}
	wait, e := db.GetGraphOwnerWait(t.Context(), inst.ID)
	if e != nil || wait.State != "claimed" || wait.ReplyID != 102 {
		t.Fatalf("reply not consumed atomically: %+v %v", wait, e)
	}
	repeated, e := db.ApproveGraphDiagnosis(t.Context(), reply, 1)
	if e != nil || !reflect.DeepEqual(repeated, a) {
		t.Fatalf("approval replay changed: %+v %v", repeated, e)
	}
	continued, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "conversation", Limit: 64, StartedAt: reply.At})
	if e != nil {
		t.Fatal(e)
	}
	db = reopenBugStopDB(t, db, path)
	_, retained, e := db.CurrentGraphDiagnosis(t.Context(), inst.ID)
	if e != nil || retained == nil || !reflect.DeepEqual(*retained, a) {
		t.Fatalf("approval restart changed: %+v %v", retained, e)
	}
	wait, e = db.GetGraphOwnerWait(t.Context(), inst.ID)
	if e != nil || wait.State != "allocated" || wait.ContinuationAttemptID != continued.ID {
		t.Fatalf("allocation lost: %+v %v", wait, e)
	}
	if _, e = db.ClaimGraphOwnerReply(t.Context(), reply); e == nil {
		t.Fatal("duplicate reply claimed")
	}
}

func TestGraphDiagnosisApprovalNegativeInputsAndRollback(t *testing.T) {
	for name, mutate := range map[string]func(*GraphOwnerReply){
		"unauthorized":   func(r *GraphOwnerReply) { r.AuthorID = 8 },
		"foreign":        func(r *GraphOwnerReply) { r.SourceRef = "github:acme/repo#999" },
		"wrong task":     func(r *GraphOwnerReply) { r.TaskRef = "github:acme/repo#999"; r.SourceRef = r.TaskRef },
		"stale question": func(r *GraphOwnerReply) { r.QuestionCommentID = 100 },
		"old reply":      func(r *GraphOwnerReply) { r.CommentID = 100 },
		"free text":      func(r *GraphOwnerReply) { r.Body = "looks good" },
		"mixed changes":  func(r *GraphOwnerReply) { r.Body = "/approve-bug analysis 1\nchange the remedy" },
		"stale revision": func(r *GraphOwnerReply) { r.Body = "/approve-bug analysis 2" },
	} {
		t.Run(name, func(t *testing.T) {
			db, _, inst, session, c, conversation := graphDiagnosisFixture(t)
			r := parkGraphDiagnosisReview(t, db, inst, session, c, conversation, true)
			mutate(&r)
			if _, e := db.ApproveGraphDiagnosis(t.Context(), r, 1); e == nil {
				t.Fatal("unauthorized approval accepted")
			}
			_, a, e := db.CurrentGraphDiagnosis(t.Context(), inst.ID)
			if e != nil || a != nil {
				t.Fatalf("failure manufactured approval: %v %v", a, e)
			}
			w, e := db.GetGraphOwnerWait(t.Context(), inst.ID)
			if e != nil || w.State != "waiting" {
				t.Fatalf("failure consumed reply: %+v %v", w, e)
			}
		})
	}
	t.Run("unpublished", func(t *testing.T) {
		db, _, i, s, c, n := graphDiagnosisFixture(t)
		r := parkGraphDiagnosisReview(t, db, i, s, c, n, false)
		if _, e := db.ApproveGraphDiagnosis(t.Context(), r, 1); e == nil {
			t.Fatal("unpublished review approved")
		}
	})
	t.Run("rollback", func(t *testing.T) {
		db, _, i, s, c, n := graphDiagnosisFixture(t)
		r := parkGraphDiagnosisReview(t, db, i, s, c, n, true)
		if _, e := db.sql.ExecContext(t.Context(), `CREATE TRIGGER fail_claim BEFORE UPDATE ON graph_owner_waits BEGIN SELECT RAISE(ABORT,'injected claim failure'); END`); e != nil {
			t.Fatal(e)
		}
		if _, e := db.ApproveGraphDiagnosis(t.Context(), r, 1); e == nil {
			t.Fatal("injected failure ignored")
		}
		_, a, e := db.CurrentGraphDiagnosis(t.Context(), i.ID)
		if e != nil || a != nil {
			t.Fatalf("partial approval retained: %+v %v", a, e)
		}
		w, e := db.GetGraphOwnerWait(t.Context(), i.ID)
		if e != nil || w.State != "waiting" {
			t.Fatalf("partial claim: %+v %v", w, e)
		}
	})
	t.Run("guidance", func(t *testing.T) {
		db, _, i, s, c, n := graphDiagnosisFixture(t)
		r := parkGraphDiagnosisReview(t, db, i, s, c, n, true)
		r.Body = "Use the reported reproduction evidence"
		if _, e := db.ClaimGraphOwnerReply(t.Context(), r); e != nil {
			t.Fatal(e)
		}
		_, a, e := db.CurrentGraphDiagnosis(t.Context(), i.ID)
		if e != nil || a != nil {
			t.Fatalf("guidance created approval: %+v %v", a, e)
		}
		replies, e := db.GraphDiagnosisGuidance(t.Context(), i.ID)
		if e != nil || len(replies) != 1 || replies[0].ReplyBody != r.Body {
			t.Fatalf("guidance lost: %+v %v", replies, e)
		}
	})
}

func TestGraphDiagnosisReshapeSuspendsApprovalAndPreservesHistory(t *testing.T) {
	db, path, inst, session, c, conversation := graphDiagnosisFixture(t)
	reply := parkGraphDiagnosisReview(t, db, inst, session, c, conversation, true)
	_, e := db.ApproveGraphDiagnosis(t.Context(), reply, 1)
	if e != nil {
		t.Fatal(e)
	}
	continued, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "conversation", Limit: 64, StartedAt: reply.At})
	if e != nil {
		t.Fatal(e)
	}
	e = db.CompleteGraphNodeAttempt(t.Context(), continued.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "approved", FinishedAt: reply.At, InstanceStatus: GraphRunning, Transition: &GraphTransition{InstanceID: inst.ID, FromNode: "conversation", ToNode: "fixer", OnOutcome: "approved", At: reply.At}})
	if e != nil {
		t.Fatal(e)
	}
	segment, e := db.GetOrCreateSegment(t.Context(), session.ID, 1, "diagnosis", AgentClaude)
	if e != nil {
		t.Fatal(e)
	}
	run, e := db.AllocateRun(t.Context(), AllocateRunParams{Workflow: "bug", SessionID: session.ID, SegmentID: segment.ID, Agent: AgentClaude, TriggerCommentID: 20, StartedAt: reply.At, LogPath: func(int) string { return "/logs/reshape.jsonl" }})
	if e != nil {
		t.Fatal(e)
	}
	source, e := db.StartGraphNodeAttempt(t.Context(), GraphAttemptStart{InstanceID: inst.ID, Node: "fixer", RunID: run.ID, Limit: 3, StartedAt: reply.At})
	if e != nil {
		t.Fatal(e)
	}
	reason := "The proposed guard is insufficient; the caller also needs a remedy change"
	result := agentresult.Result{Outcome: "incomplete", Comment: "Partial work retained", Reason: reason, Payload: agentresult.Payload{Bug: &agentresult.Bug{Response: "reshape", Repair: &bugrepair.Evidence{AnalysisDigest: c.Digest}}}}
	binding, e := db.BindResultAttempt(t.Context(), ResultAttempt{Workflow: "bug", Task: session.TaskRef, RunID: run.ID, WorkContext: "reshape", Attempt: "fixer-1", SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := json.Marshal(agentresult.Snapshot{Result: result})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.RecordResult(t.Context(), ResultRecord{Binding: binding, RequestID: "reshape-1", Payload: snapshot, Diagnostics: []byte(`{"valid":true,"complete":true}`), Snapshot: snapshot, Retired: []byte(`[]`), Accepted: true})
	if e != nil {
		t.Fatal(e)
	}
	retained, e := json.Marshal(struct {
		ResultID       int64                    `json:"result_id"`
		ResultRevision int64                    `json:"result_revision"`
		Node           agentresult.ResearchNode `json:"node"`
	}{binding.ResultID, 1, agentresult.ResearchNode{Version: 1, Response: "reshape", Result: result}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.FinishRun(t.Context(), run.ID, RunOutcome{Status: RunSucceeded})
	if e != nil {
		t.Fatal(e)
	}
	e = db.CompleteGraphNodeAttempt(t.Context(), source.ID, GraphCompletion{Status: GraphSucceeded, Outcome: "reshape", FinishedAt: reply.At, InstanceStatus: GraphRunning, Outputs: []GraphOutput{{Kind: GraphOutputJSON, Payload: retained}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.SuspendGraphDiagnosis(t.Context(), inst.ID, source.ID, c.Revision, c.Digest, reason, reply.At); e != nil {
		t.Fatal(e)
	}
	db = reopenBugStopDB(t, db, path)
	_, approval, e := db.CurrentGraphDiagnosis(t.Context(), inst.ID)
	if e != nil || approval != nil {
		t.Fatalf("reshape retained authority: %+v %v", approval, e)
	}
	if e = db.SuspendGraphDiagnosis(t.Context(), inst.ID, source.ID, c.Revision, c.Digest, reason, reply.At); e != nil {
		t.Fatal("lost-ack reshape replay failed:", e)
	}
	history, e := db.GraphDiagnosisReshapes(t.Context(), inst.ID)
	if e != nil || len(history) != 1 || !strings.Contains(string(history[0]), reason) {
		t.Fatalf("reshape history missing or duplicated: %s %v", history, e)
	}
}
