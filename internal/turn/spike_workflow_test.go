package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

const spikeDoneJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"The staging path is correct.","payload":{"spike":{"request":"Why does staging fail?","findings":["Staging is correct"]}}}}`

const spikeAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"The answer depends on the environment","comment":"Blocked on one decision.","questions":[{"id":"q1","audience":"technical","text":"Which environment is the target?","options":[{"id":"a","text":"CI"},{"id":"b","text":"Local"}],"recommended_option":"a"}],"payload":{"spike":{"request":"Why does staging fail?"}}}}`

func assembledSpikeFixture(t *testing.T, runner Runner) (*fixture, *fakeForge, Request) {
	t.Helper()
	f := newFixture(t, runner)
	authority, err := NewAttemptAuthority(t.Context(), f.db)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, authority.Close(context.WithoutCancel(t.Context()))) })
	f.opts.MCP = &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}
	f.opts.Tiers.ModelByTier = claudeEveryTier("test-model")
	forge := newFakeForge(t)
	f.opts.MCP.Comments = forge.client
	f.opts.MCP.Redactor = redact.New(redact.Options{Environ: []string{}})
	f.opts.MCP.BotID = 9999
	f.opts.ResearchReporter = newReporter(t, f, forge, nil)
	req := f.request()
	body := "/spike why does staging fail?"
	req.Mode, req.Trigger.Body, req.Command, req.ToolProfile = agent.ModeSpike, body, ParseCommand(body), "spike"
	return f, forge, req
}

func spikeInstance(t *testing.T, f *fixture) store.GraphInstance {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike")
	mustProgress(t, err)
	return instance
}

func spikeLastBody(forge *fakeForge) string {
	posts := forge.all()
	if len(posts) == 0 {
		return ""
	}
	return posts[len(posts)-1].body
}

// A finished result is posted as the agent wrote it and the Spike completes,
// with one agent run and one comment. The agent runs with write access.
func TestSpikeDonePostsTheReportAndCompletes(t *testing.T) {
	var seen agent.Request
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		seen = req
		submitLiveResult(t, req, spikeDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || !out.ResearchGraph || calls != 1 || forge.attemptCount() != 1 {
		t.Fatalf("done path: %+v calls=%d posts=%d err=%v", out, calls, forge.attemptCount(), err)
	}
	if !strings.Contains(spikeLastBody(forge), "The staging path is correct.") {
		t.Fatalf("the agent's comment was not posted: %q", spikeLastBody(forge))
	}
	surface, ok := seen.Permissions.Payload.(harness.ToolSurface)
	if !ok || !slices.Contains(surface.Tools, "Edit") || surface.ReadOnlyShell {
		t.Fatalf("Spike does not run with write access: %+v", seen.Permissions)
	}
	if instance := spikeInstance(t, f); instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
		t.Fatalf("instance %+v", instance)
	}
}

// An ask is posted and the instance waits. Each owner reply resumes the same
// agent node with the reply; there is no cap on question rounds, and the
// conversation ends when the agent reports it is finished.
func TestSpikeAskWaitsAndEachReplyResumesTheSameAgent(t *testing.T) {
	calls := 0
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		requests = append(requests, req)
		client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
		switch calls {
		case 1:
			submitLiveResult(t, req, spikeAskJSON)
		case 2:
			// Asks again: no factory counter refuses a second round.
			update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"One more choice."}]}`)
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("second question refused: %+v", got.Validation)
			}
		default:
			update := []byte(`{"expected_revision":2,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Changed it for CI."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}}]}`)
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("finished report refused: %+v", got.Validation)
			}
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "turn", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	for round, body := range []string{"CI", "yes, do it"} {
		instance := spikeInstance(t, f)
		wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
		mustProgress(t, err)
		if wait.State != "waiting" || instance.Status != store.GraphRunning || forge.attemptCount() != round+1 {
			t.Fatalf("round %d did not post and wait: %+v %+v posts=%d", round, instance, wait, forge.attemptCount())
		}
		if !strings.Contains(spikeLastBody(forge), "Which environment is the target?") {
			t.Fatalf("question was not posted: %q", spikeLastBody(forge))
		}
		delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
		mustProgress(t, err)
		reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + int64(round) + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "spike", DeliveryKey: wait.DeliveryKey, Body: body, At: time.Now()}
		_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
		mustProgress(t, err)
		req.Trigger.CommentID = reply.CommentID
		req.Trigger.Body = reply.Body
		req.Command = ParseCommand(reply.Body)
		_, err = Execute(t.Context(), f.opts, req)
		mustProgress(t, err)
	}
	if calls != 3 || forge.attemptCount() != 3 || !strings.Contains(spikeLastBody(forge), "Changed it for CI.") {
		t.Fatalf("conversation did not finish: calls=%d posts=%d last=%q", calls, forge.attemptCount(), spikeLastBody(forge))
	}
	for i, r := range requests[1:] {
		if r.Graph == nil || r.Graph.Node != "spike" || !strings.Contains(r.Prompt, []string{"CI", "yes, do it"}[i]) {
			t.Fatalf("reply %d did not reach the same agent: %+v", i, r.Graph)
		}
	}
	if instance := spikeInstance(t, f); instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
		t.Fatalf("instance %+v", instance)
	}
}

// An owner comment after a finished Spike starts the next Spike, which receives
// the finished one's result and the comment as context.
func TestSpikeReplyAfterFinishStartsTheNextSpikeWithContext(t *testing.T) {
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		requests = append(requests, req)
		submitLiveResult(t, req, spikeDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	first := spikeInstance(t, f)
	req.Trigger.CommentID++
	req.Trigger.Body = "Thanks. Now rename the flag."
	req.Command = ParseCommand(req.Trigger.Body)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.Succeeded() || len(requests) != 2 || forge.attemptCount() != 2 {
		t.Fatalf("next Spike did not run: %+v runs=%d posts=%d err=%v", out, len(requests), forge.attemptCount(), err)
	}
	if next := spikeInstance(t, f); next.ID == first.ID {
		t.Fatal("the finished Spike was reused instead of followed")
	}
	if prompt := requests[1].Prompt; !strings.Contains(prompt, "Staging is correct") || !strings.Contains(prompt, "rename the flag") {
		t.Fatal("the next Spike lost the earlier result or the owner's comment")
	}
}

// A failed attempt posts the failure and stops.
func TestSpikeAgentFailurePostsAndStops(t *testing.T) {
	runner := lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		return &agent.RunResult{Outcome: agent.OutcomeTimedOut, Reason: "timeout", FinishedAt: time.Now()}, nil
	})
	f, forge, req := assembledSpikeFixture(t, runner)
	out, err := Execute(t.Context(), f.opts, req)
	if err == nil || out.Run.Status != store.RunFailed || forge.attemptCount() != 1 || spikeInstance(t, f).Status != store.GraphFailed {
		t.Fatalf("agent failure went unreported: %+v posts=%d err=%v", out.Run, forge.attemptCount(), err)
	}
	intents, e := f.db.ListGraphComments(t.Context(), spikeInstance(t, f).ID, "", 100)
	mustProgress(t, e)
	if len(intents) != 1 || intents[0].Kind != "failure" {
		t.Fatalf("want exactly the failure comment, got %+v", intents)
	}
}

func TestSpikeAdmissionLeavesOtherModesUnhandled(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeImplement, agent.ModeValidate} {
		if _, handled, err := admitSpikeExecution(t.Context(), Options{}, Request{Mode: mode}, Result{}); err != nil || handled {
			t.Fatalf("Spike admission handled %s: handled=%v err=%v", mode, handled, err)
		}
	}
}

func TestSpikeAdmissionKnownLegacyHistoryIsRefusedBeforeDispatch(t *testing.T) {
	f, forge, req := assembledSpikeFixture(t, nil)
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, store.FirstSegmentOrdinal, "legacy-history", store.AgentClaude)
	mustProgress(t, err)
	_, err = f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "spike", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "old.log" }})
	mustProgress(t, err)
	out, handled, err := admitSpikeExecution(t.Context(), f.opts, req, Result{Session: f.session, SessionID: f.session.ID})
	if err != nil || !handled || forge.attemptCount() != 0 || !strings.HasPrefix(out.Reason, spikeRetiredReason) || out.Disposition != DispositionPrepareFailed {
		t.Fatalf("a legacy-history task was not refused: handled=%v err=%v out=%+v", handled, err, out)
	}
	if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "spike"); err == nil {
		t.Fatal("a legacy-history task created a graph instance")
	}
	route, err := f.db.ResearchExecutionPath(t.Context(), f.session.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refusal persisted a route: %q %v", route, fmt.Sprint(err))
	}
}
