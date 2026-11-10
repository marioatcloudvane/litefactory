package turn

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

const bugFixedJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"Fixed: saving no longer crashes on an empty name.","payload":{"bug":{"request":"Saving crashes on an empty name.","work_done":["go test -run TestSaveEmpty failed before the fix","go test -run TestSaveEmpty passed after the fix"],"findings":["The save path indexed an empty name."]}}}}`

const bugAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"The fix would change the saved format","comment":"Need your answer: the fix changes the saved format.","questions":[{"id":"q1","audience":"technical","text":"May the saved format change?","options":[{"id":"a","text":"Yes"},{"id":"b","text":"No, keep it"}],"recommended_option":"b"}],"payload":{"bug":{"request":"Saving crashes on an empty name."}}}}`

// assembledBugFixture is Spike's fixture in Bug mode, on a bug-labelled task.
func assembledBugFixture(t *testing.T, runner Runner) (*fixture, *fakeForge, Request) {
	t.Helper()
	f, forge, req := assembledSpikeFixture(t, runner)
	_, err := f.db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: f.session.ID, Version: 1, Family: "standalone", Workflow: "bug"})
	mustProgress(t, err)
	body := "Saving crashes on an empty name."
	req.Mode, req.Workflow, req.Trigger.Body, req.Command, req.ToolProfile = agent.ModeBug, agent.WorkflowBug, body, ParseCommand(body), "bug"
	return f, forge, req
}

func bugInstance(t *testing.T, f *fixture) store.GraphInstance {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "bug")
	mustProgress(t, err)
	return instance
}

func claimBugReply(t *testing.T, f *fixture, instance store.GraphInstance, author, comment int64, body string) (store.GraphOwnerReply, error) {
	t.Helper()
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: author, CommentID: comment, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "bug", Workflow: "bug", DeliveryKey: wait.DeliveryKey, Body: body, At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	return reply, err
}

// A Bug reproduces and fixes the bug without a go, the factory publishes the
// shipping review, a non-owner's "ship" is refused, and only the owner's
// exact "ship" merges and closes.
func TestBugFixShipsOnlyOnTheOwnersShip(t *testing.T) {
	calls := 0
	checkout := ""
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		checkout = req.WorktreePath
		if req.Graph == nil || req.Mode != "bug" {
			t.Fatalf("not a Bug graph invocation: %+v", req.Graph)
		}
		mustProgress(t, os.WriteFile(filepath.Join(checkout, "save_test.go"), []byte("package save\n"), 0600))
		submitLiveResult(t, req, bugFixedJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	})
	f, _, req := assembledBugFixture(t, runner)
	remote := &graphDeliveryForge{issue: github.Issue{ID: 123, Number: 123, State: "open"}}
	code := checkoutDeliveryForge{graphDeliveryForge: remote, checkout: &checkout}
	f.opts.DeliveryCode, f.opts.DeliveryTickets = code, code
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance := bugInstance(t, f)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	intent, err := f.db.GetGraphComment(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	if calls != 1 || wait.State != "waiting" || intent.Kind != "review" || remote.merges != 0 || remote.closes != 0 {
		t.Fatalf("fix did not reach the shipping review: calls=%d wait=%+v kind=%s merges=%d", calls, wait, intent.Kind, remote.merges)
	}
	if _, err := claimBugReply(t, f, instance, 8, testComment+1, "ship"); err == nil {
		t.Fatal("a non-owner's ship was claimed")
	}
	reply, err := claimBugReply(t, f, instance, 7, testComment+2, "ship")
	mustProgress(t, err)
	req.Trigger.CommentID, req.Trigger.Body, req.Command = reply.CommentID, reply.Body, ParseCommand(reply.Body)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if remote.merges != 1 || remote.closes != 1 || calls != 1 || bugInstance(t, f).Status != store.GraphSucceeded {
		t.Fatalf("ship did not deliver: merges=%d closes=%d calls=%d", remote.merges, remote.closes, calls)
	}
}

// A Bug that asks is posted and waits; the owner's reply resumes the same
// agent, which then finishes with nothing to change and its report is posted.
func TestBugAskResumesTheSameAgentOnTheOwnersReply(t *testing.T) {
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		requests = append(requests, req)
		if len(requests) == 1 {
			submitLiveResult(t, req, bugAskJSON)
		} else {
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Nothing to fix: the format stays."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}}]}`)
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("finished report refused: %+v", got.Validation)
			}
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledBugFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance := bugInstance(t, f)
	if instance.Status != store.GraphRunning || forge.attemptCount() != 1 {
		t.Fatalf("question was not posted: %+v posts=%d", instance, forge.attemptCount())
	}
	reply, err := claimBugReply(t, f, instance, 7, testComment+1, "b, keep it")
	mustProgress(t, err)
	req.Trigger.CommentID, req.Trigger.Body, req.Command = reply.CommentID, reply.Body, ParseCommand(reply.Body)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if len(requests) != 2 || requests[1].Graph == nil || requests[1].Graph.Node != requests[0].Graph.Node {
		t.Fatalf("the reply did not resume the same agent: %d requests", len(requests))
	}
	if forge.attemptCount() != 2 || bugInstance(t, f).Status != store.GraphSucceeded {
		t.Fatalf("report not posted or Bug not finished: posts=%d %+v", forge.attemptCount(), bugInstance(t, f))
	}
}
