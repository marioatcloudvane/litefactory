package codex

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestJudgeRefusesUnverifiedNativeToolCeiling(t *testing.T) {
	dir := t.TempDir()
	launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "judge-attempt", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-attempt", Model: "gpt-6-astra", Prompt: "saved evidence", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"}
	_, err = (&Runner{}).RunJudge(t.Context(), request)
	if !errors.Is(err, agent.ErrJudgeIsolationUnverified) || errors.Is(err, agent.ErrJudgeUnavailable) {
		t.Fatalf("wrong refusal: %v", err)
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
