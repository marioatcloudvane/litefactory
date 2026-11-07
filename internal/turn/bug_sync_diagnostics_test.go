package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugSyncStopRetainsReadablePartialEffects(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			f, profile, instance := approvedGraphSyncDiagnosticsFixture(t)
			gate, ok := graphexec.BugFixerSyncGate(profile)
			if !ok {
				t.Fatal("canonical Bug lacks sync prerequisite")
			}
			evidence := syncBaseOutput{Working: "lf/retained", Base: "main", Result: "conflict", Reason: "base overlaps retained repair", Conflicts: []string{"repair.go"}, Commit: strings.Repeat("a", 40), Publication: "uncertain"}
			raw, err := json.Marshal(evidence)
			mustProgress(t, err)
			handler := &graphTestHandler{results: map[string][]ActivityResult{gate.Name: {{Outcome: "conflict", Kind: store.GraphOutputJSON, Output: raw}}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				handler.errs = map[string]error{gate.Name: context.Canceled}
				handler.hook = func(context.Context, Options, ActivityRequest) { cancel() }
			}
			var activities ActivityRegistry
			mustProgress(t, activities.Register(graphexec.SyncBaseAction, handler))
			step, err := ExecuteNodeAction(ctx, f.opts, GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()})
			if canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation=%v", err)
				}
			} else {
				mustProgress(t, err)
			}
			wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
			mustProgress(t, err)
			if step.Status != "waiting_owner" || wait.OriginNode != gate.Name {
				t.Fatalf("sync did not park: %+v %+v", step, wait)
			}
			outputs, err := f.db.ListGraphOutputs(t.Context(), step.AttemptID)
			mustProgress(t, err)
			retained := false
			diagnostic := ""
			for _, output := range outputs {
				if output.Kind == store.GraphOutputJSON && string(output.Payload) == string(raw) {
					retained = true
				}
				if output.Kind == store.GraphOutputDiagnostic {
					diagnostic = string(output.Payload)
				}
			}
			for _, part := range []string{evidence.Reason, "repair.go", "uncertain", evidence.Commit, "verified: false"} {
				if !strings.Contains(diagnostic, part) {
					t.Fatalf("missing %q in diagnostic %q", part, diagnostic)
				}
			}
			if !retained {
				t.Fatal("partial effect JSON was lost")
			}
			// A canceled first publication remains recoverable from the immutable stop.
			_, err = f.opts.ResearchReporter.RecoverGraphStopQuestion(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
			mustProgress(t, err)
			for _, attempt := range attempts {
				if attempt.Node == "fixer" {
					t.Fatal("blocked synchronization launched fixer")
				}
			}
		})
	}
}

// The step budget belongs to the instance: once it is spent, the instance parks
// for the owner with one plain comment instead of failing, and the owner's reply
// starts a fresh budget at the same step.
func TestStepBudgetParksForTheOwnerAndAReplyResetsIt(t *testing.T) {
	f, profile, instance := approvedGraphSyncDiagnosticsFixture(t)
	gate, ok := graphexec.BugFixerSyncGate(profile)
	if !ok {
		t.Fatal("canonical Bug lacks sync prerequisite")
	}
	before, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	mustProgress(t, err)
	// The budget counts from the continuation the latest owner reply allocated.
	spent := len(before)
	if prior, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID); err == nil && prior.State == "allocated" {
		spent = 0
		for _, a := range before {
			if a.ID >= prior.ContinuationAttemptID {
				spent++
			}
		}
	}
	const retries = 3
	budget := spent + retries
	profile.Attrs = maps.Clone(profile.Attrs)
	profile.Attrs[graphexec.StepBudgetAttribute] = strconv.Itoa(budget)
	handler := &graphTestHandler{results: map[string][]ActivityResult{}}
	for i := 0; i < 2*retries; i++ {
		raw, err := json.Marshal(syncBaseOutput{Working: "lf/retained", Base: "main", Result: "retry", Reason: fmt.Sprintf("retained unpublished commit %d", i+1)})
		mustProgress(t, err)
		handler.results[gate.Name] = append(handler.results[gate.Name], ActivityResult{Outcome: "retry", Kind: store.GraphOutputJSON, Output: raw})
	}
	var activities ActivityRegistry
	mustProgress(t, activities.Register(graphexec.SyncBaseAction, handler))
	request := GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()}
	for i := 0; i < retries; i++ {
		step, err := ExecuteNodeAction(t.Context(), f.opts, request)
		mustProgress(t, err)
		if step.NextNode != gate.Name || step.Status != store.GraphRunning {
			t.Fatalf("retry escaped gate: %+v", step)
		}
	}
	step, err := ExecuteNodeAction(t.Context(), f.opts, request)
	mustProgress(t, err)
	if !step.Exhausted || step.Status != "waiting_owner" || len(handler.calls) != retries {
		t.Fatalf("exhaustion=%+v calls=%d", step, len(handler.calls))
	}
	current, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	mustProgress(t, err)
	if current.Status != store.GraphRunning || current.CurrentNode != gate.Name {
		t.Fatalf("exhausted budget did not park at its step: %+v", current)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || wait.OriginNode != gate.Name {
		t.Fatalf("no owner wait: %+v", wait)
	}
	intent, err := f.db.GetGraphComment(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	var question GraphStopQuestion
	mustProgress(t, json.Unmarshal(intent.Candidate, &question))
	if question.Cause != graphBudgetCause || !strings.Contains(question.Reason, fmt.Sprintf("Stopped after %d steps without an owner reply; reply to continue.", budget)) {
		t.Fatalf("stop comment: %+v", question)
	}
	// A repeat while parked posts nothing more and dispatches nothing.
	again, err := ExecuteNodeAction(t.Context(), f.opts, request)
	mustProgress(t, err)
	if again.Status != "waiting_owner" || len(handler.calls) != retries {
		t.Fatalf("parked instance moved: %+v calls=%d", again, len(handler.calls))
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7,
		CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID,
		TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug",
		DeliveryKey: wait.DeliveryKey, Body: "Keep going", At: request.At.Add(time.Minute)}
	claimed, err := AdmitGraphBugOwnerReply(t.Context(), f.db, reply)
	mustProgress(t, err)
	if claimed.State != "claimed" {
		t.Fatalf("reply was not claimed: %+v", claimed)
	}
	request.At = reply.At
	for i := 0; i < retries; i++ {
		step, err = ExecuteNodeAction(t.Context(), f.opts, request)
		mustProgress(t, err)
		if step.Exhausted || step.Status != store.GraphRunning {
			t.Fatalf("owner reply did not renew the budget: %+v", step)
		}
	}
	if len(handler.calls) != 2*retries {
		t.Fatalf("dispatches after the reply: %d", len(handler.calls))
	}
}

func approvedGraphSyncDiagnosticsFixture(t *testing.T) (*fixture, *graphexec.Profile, store.GraphInstance) {
	t.Helper()
	f, _, profile, instance := conversationSourceFixture(t, true)
	var activities ActivityRegistry
	mustProgress(t, activities.Register(graphexec.ResearchLoopAction, GraphBugConversation{Profile: profile, Reporter: f.opts.ResearchReporter}))
	request := GraphNodeRequest{Profile: profile, Activities: activities, InstanceID: instance.ID, At: f.opts.MCP.Now()}
	_, err := ExecuteNodeAction(t.Context(), f.opts, request)
	mustProgress(t, err)
	completeConversationAnalyst(t, f, instance, "done")
	_, err = ExecuteNodeAction(t.Context(), f.opts, request)
	mustProgress(t, err)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: delivery.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: "/approve-bug analysis 1", At: request.At.Add(time.Minute)}
	_, err = AdmitGraphBugOwnerReply(t.Context(), f.db, reply)
	mustProgress(t, err)
	request.At = reply.At
	_, err = ExecuteNodeAction(t.Context(), f.opts, request)
	mustProgress(t, err)
	return f, profile, instance
}
