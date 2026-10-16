package codex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestJudgeRejectsInvalidRequestBeforeLaunch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*agent.JudgeRequest)
	}{
		{"unsupported thinking", func(request *agent.JudgeRequest) { request.Thinking = "unsupported" }},
		{"unsupported model effort", func(request *agent.JudgeRequest) { request.Invocation.Model = "unknown-model" }},
		{"missing prompt", func(request *agent.JudgeRequest) { request.Invocation.Prompt = "" }},
		{"missing catalog", func(request *agent.JudgeRequest) { request.Invocation.MCPLaunch = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "judge-attempt", agent.JudgeToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-attempt", Model: "gpt-6-astra", Prompt: "saved evidence", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"}
			tc.mutate(&request)
			_, err = (&Runner{}).RunJudge(t.Context(), request)
			if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) {
				t.Fatalf("invalid request did not fail before availability: %v", err)
			}
		})
	}
}

func TestJudgeCancelledBeforeLaunch(t *testing.T) {
	dir := t.TempDir()
	launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "cancelled-judge", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "cancelled-judge", Model: "gpt-6-astra", Prompt: "saved evidence", WorkingDirectory: dir, MCPLaunch: launch}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = (&Runner{}).RunJudge(ctx, request)
	if !errors.Is(err, context.Canceled) || errors.Is(err, agent.ErrJudgeUnavailable) {
		t.Fatalf("cancellation became provider availability: %v", err)
	}
}

func TestJudgeMissingClientIsAvailability(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "missing-client")
	launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "missing-judge", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "missing-judge", Model: "gpt-6-astra", Prompt: "saved evidence", WorkingDirectory: dir, MCPLaunch: launch}}
	runner := &Runner{binary: config.Executable{Path: binary}, environ: []string{"PATH=/usr/bin:/bin"}}
	_, err = runner.RunJudge(t.Context(), request)
	if !errors.Is(err, agent.ErrJudgeUnavailable) {
		t.Fatalf("missing executable was not availability: %v", err)
	}
}

func TestNativeRequestThinkingOption(t *testing.T) {
	runner := &Runner{model: "gpt-6-astra"}
	args, err := runner.args(agent.Request{Model: "gpt-6-astra", Thinking: "xhigh"}, "read-only")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model gpt-6-astra") || !strings.Contains(joined, `model_reasoning_effort="xhigh"`) {
		t.Fatalf("native overrides missing: %s", joined)
	}
}

func TestJudgeAvailabilityStatusBoundary(t *testing.T) {
	for _, tc := range []struct {
		detail string
		want   bool
	}{
		{"HTTP 503 service unavailable", true},
		{"Unexpected status code: 502", true},
		{"HTTP 5030 diagnostic", false},
		{"unsupported thinking; HTTP 503", false},
		{"model does not support requested effort; HTTP 429", false},
		{"context length exceeded; HTTP 429", false},
		{"budget exhausted; HTTP 503", false},
	} {
		if got := judgeCapacity(tc.detail); got != tc.want {
			t.Errorf("%q: availability=%t, want %t", tc.detail, got, tc.want)
		}
	}
}
