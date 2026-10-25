package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestSubmissionWorkerTimingFollowsTypedPlanPromptBinding(t *testing.T) {
	for _, mode := range []string{"nil options", "nil session", "typed plan"} {
		t.Run(mode, func(t *testing.T) {
			db, _, _ := agentResultFixture(t, "plan")
			input := harness.PromptInput{Ref: naming.Ref{Forge: naming.ForgeGitHub, Owner: "example", Repo: "results", Issue: 263}, FeatureSlug: "263-worker-timing", Branch: "lf/263-worker-timing", Mode: "plan", RunOrdinal: 1, Instruction: "Continue the approved work"}
			prompt, err := harness.BuildPrompt(input)
			mustProgress(t, err)
			timing := "\nWorker timing: effective limit 61 seconds; deadline 2026-10-25T12:00:00Z.\n"
			seen := ""
			prep := &preparation{promptInput: input, workerTiming: timing, runner: newStubRunner(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				seen = req.Prompt
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil
			})}
			if mode != "nil options" {
				prep.submissionOptions = &SubmissionSessionOptions{DB: db}
			}
			if mode == "typed plan" {
				prep.planSubmission = func(context.Context) (*PlanSubmissionCallbacks, error) { return &PlanSubmissionCallbacks{}, nil }
			}
			result, err := runSubmissionBound(t.Context(), prep, agent.Request{Mode: "plan", Prompt: prompt})
			if err != nil || result == nil || !result.Succeeded() {
				t.Fatalf("launch failed after timing was supplied: %+v %v", result, err)
			}
			expected := input
			if mode == "typed plan" {
				expected.SubmissionVersion = 2
			}
			rebound, err := harness.BuildPrompt(expected)
			mustProgress(t, err)
			if seen != rebound+timing || strings.Count(seen, timing) != 1 || prep.prompt != seen {
				t.Fatal("runner did not receive exactly one timing fact after the bound prompt")
			}
		})
	}
}

func TestNonGraphFinalizationBoundsBlockedTransportJoin(t *testing.T) {
	f := newRetainedFixture(t, "research")
	f.submit(t, f.result)
	entered := make(chan struct{})
	release := make(chan struct{})
	broker, err := submission.Start(t.Context(), submission.Options{Directory: launchFixtureDirectory(t), AllowResult: true, MaxWorkers: 1, RequestTimeout: time.Second, Handler: func(context.Context, submission.Request) (json.RawMessage, error) {
		close(entered)
		<-release
		return json.RawMessage(`{"state":"recorded"}`), nil
	}})
	mustProgress(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = submission.Call(t.Context(), broker.DescriptorPath(), "preflight", []byte(`{}`))
	}()
	defer func() { close(release); mustProgress(t, broker.Close()); <-done }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("transport handler did not start")
	}
	worker, cancel := agent.WithWorkerDeadline(t.Context(), time.Now().Add(50*time.Millisecond))
	defer cancel()
	p := &preparedMCP{service: f.service, resultBroker: broker}
	result, err := p.finalize(worker, &preparation{}, nil, &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "provider failed"}, nil)
	if err != nil || result == nil || !strings.Contains(result.Diagnostic, "factory helper cleanup unavailable") {
		t.Fatalf("blocked transport did not surface bounded cleanup: %+v %v", result, err)
	}
	receipt, err := f.service.Receipt(t.Context(), f.opts.Attempt.RunID, "req-1")
	mustProgress(t, err)
	if receipt.Receipt == nil || receipt.Receipt.Latest.Input != "accepted" || receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" || receipt.Receipt.Latest.Publication != "pending" {
		t.Fatalf("transport timeout changed durable input or invented workflow acceptance: %+v", receipt.Receipt)
	}
}
