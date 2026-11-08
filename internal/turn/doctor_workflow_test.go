package turn

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

const doctorDoneJSON = `{"version":1,"response":"done","result":{"outcome":"completed","comment":"The task stopped because its last check timed out.","payload":{"doctor":{"request":"Why is this task stuck?","findings":["The check timed out"]}}}}`

const doctorAskJSON = `{"version":1,"response":"ask","result":{"outcome":"incomplete","reason":"Resetting the branch discards work","comment":"Need your go: reset the branch to the base.","questions":[{"id":"q1","audience":"technical","text":"May I reset the branch?","options":[{"id":"a","text":"Yes"},{"id":"b","text":"No"}],"recommended_option":"b"}],"payload":{"doctor":{"request":"Why is this task stuck?"}}}}`

const doctorSecret = "doctor-fixture-secret-value"

// doctorFixture is the Spike fixture under the Doctor's command, with a
// redactor that knows one secret and a finished earlier run whose log holds it.
func doctorFixture(t *testing.T, runner Runner) (*fixture, *fakeForge, Request) {
	t.Helper()
	f, forge, req := assembledSpikeFixture(t, runner)
	f.opts.MCP.Redactor = redact.New(redact.Options{Environ: []string{"API_SECRET=" + doctorSecret}})
	body := "/doctor why is this task stuck?"
	req.Mode, req.Trigger.Body, req.Command, req.ToolProfile = agent.ModeDoctor, body, ParseCommand(body), "doctor"
	return f, forge, req
}

// earlierRun records a run of the task's own workflow whose log carries the
// fixture secret, in the given status.
func earlierRun(t *testing.T, f *fixture, status store.RunStatus) store.Run {
	t.Helper()
	ctx := t.Context()
	segment, err := f.db.GetOrCreateSegment(ctx, f.session.ID, 1, "earlier", store.AgentClaude)
	mustProgress(t, err)
	log := filepath.Join(t.TempDir(), "earlier.jsonl")
	mustProgress(t, os.WriteFile(log, []byte(`{"type":"tool","text":"token `+doctorSecret+`"}`+"\n"), 0o600))
	run, err := f.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, TriggerCommentID: 5, LogPath: func(int) string { return log }})
	mustProgress(t, err)
	if status != store.RunRunning {
		zero := int64(0)
		_, err = f.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: status, ExitCode: &zero, SemanticOutcome: "failed", FinishedAt: time.Now()})
		mustProgress(t, err)
	}
	return run
}

func doctorInstance(t *testing.T, f *fixture) store.GraphInstance {
	t.Helper()
	instance, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "doctor")
	mustProgress(t, err)
	return instance
}

var evidencePath = regexp.MustCompile(`Factory evidence for this task[^:]*: (\S+)`)

// The owner's /doctor on a stopped task runs the Doctor once with write access
// and a redacted evidence bundle, posts its report and completes.
func TestDoctorOnStoppedTaskReportsAndCompletes(t *testing.T) {
	var seen agent.Request
	var bundle string
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		seen = req
		if m := evidencePath.FindStringSubmatch(req.Prompt); m != nil {
			raw, err := os.ReadFile(filepath.Join(m[1], "logs", "run-1.jsonl"))
			if err == nil {
				bundle = string(raw)
			}
		}
		submitLiveResult(t, req, doctorDoneJSON)
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "done", FinishedAt: time.Now()}, nil
	})
	f, forge, req := doctorFixture(t, runner)
	earlierRun(t, f, store.RunFailed)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || !out.ResearchGraph || calls != 1 || forge.attemptCount() != 1 {
		t.Fatalf("doctor run: %+v calls=%d posts=%d err=%v", out, calls, forge.attemptCount(), err)
	}
	if !strings.Contains(spikeLastBody(forge), "its last check timed out") {
		t.Fatalf("the Doctor's report was not posted: %q", spikeLastBody(forge))
	}
	if seen.Graph == nil || !evidencePath.MatchString(seen.Prompt) {
		t.Fatal("the Doctor was not pointed at its evidence")
	}
	if bundle == "" || strings.Contains(bundle, doctorSecret) || !strings.Contains(bundle, "token") {
		t.Fatalf("the run log reached the Doctor unredacted or not at all: %q", bundle)
	}
	if instance := doctorInstance(t, f); instance.Status != store.GraphSucceeded || instance.CurrentNode != "complete" {
		t.Fatalf("instance %+v", instance)
	}
}

// A task with a running run is not stopped: the Doctor does not start and the
// owner reads why.
func TestDoctorRefusedWhileTheTaskRuns(t *testing.T) {
	calls := 0
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
	})
	f, _, req := doctorFixture(t, runner)
	earlierRun(t, f, store.RunRunning)
	out, err := Execute(t.Context(), f.opts, req)
	if err != nil || calls != 0 || !strings.Contains(out.Reason, "still has work in flight") {
		t.Fatalf("busy task: %+v calls=%d err=%v", out, calls, err)
	}
	if _, err := f.db.LatestWorkflowInstance(t.Context(), f.session.ID, "doctor"); err == nil {
		t.Fatal("a Doctor instance was admitted on a running task")
	}
}

// A Doctor question waits; the owner's reply resumes the same agent, which then
// finishes.
func TestDoctorAskThenOwnerReplyResumes(t *testing.T) {
	calls := 0
	var requests []agent.Request
	runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		requests = append(requests, req)
		if calls == 1 {
			submitLiveResult(t, req, doctorAskJSON)
		} else {
			client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
			update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"outcome"},"value":"completed"},{"op":"set","target":{"field":"comment"},"value":"Left the branch as it is."},{"op":"remove","target":{"field":"reason"}},{"op":"remove","target":{"field":"questions"}}]}`)
			if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
				t.Fatalf("finished report refused: %+v", got.Validation)
			}
		}
		return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "turn", FinishedAt: time.Now()}, nil
	})
	f, forge, req := doctorFixture(t, runner)
	_, err := Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	instance := doctorInstance(t, f)
	wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
	mustProgress(t, err)
	if wait.State != "waiting" || !strings.Contains(spikeLastBody(forge), "May I reset the branch?") {
		t.Fatalf("question not posted or not waiting: %+v %q", wait, spikeLastBody(forge))
	}
	delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
	mustProgress(t, err)
	reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "doctor", DeliveryKey: wait.DeliveryKey, Body: "No, keep it", At: time.Now()}
	_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
	mustProgress(t, err)
	req.Trigger.CommentID, req.Trigger.Body, req.Command = reply.CommentID, reply.Body, ParseCommand(reply.Body)
	_, err = Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if calls != 2 || requests[1].Graph == nil || requests[1].Graph.Node != requests[0].Graph.Node || !strings.Contains(requests[1].Prompt, "No, keep it") {
		t.Fatalf("the reply did not resume the same agent: calls=%d", calls)
	}
	if !strings.Contains(spikeLastBody(forge), "Left the branch as it is.") {
		t.Fatalf("final report: %q", spikeLastBody(forge))
	}
	if instance := doctorInstance(t, f); instance.Status != store.GraphSucceeded {
		t.Fatalf("instance %+v", instance)
	}
}
