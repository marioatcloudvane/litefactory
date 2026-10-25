package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// newOversizedCodexFixture is a shell codex whose judge turn or bundled
// catalogue prints one line over the 1 MiB line budget.
func newOversizedCodexFixture(t *testing.T, catalogue string) (*Runner, agent.JudgeRequest, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
case "$1" in
--version) echo 'codex-cli 0.160.0'; exit 0;;
debug) ` + catalogue + `; exit 0;;
esac
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"t"}'
head -c 1100000 /dev/zero | tr '\0' x; printf '\n'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":3}}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "judge-oversized", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-oversized", Model: "gpt-6-astra", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, logger: slog.New(slog.NewJSONHandler(&logs, nil)), environ: []string{"PATH=/usr/bin:/bin"}}
	return runner, request, &logs
}

const smallCodexCatalogue = `printf '%s\n' '{"models":[{"slug":"gpt-6-astra","supported_reasoning_levels":[{"effort":"high","description":"h"}]}]}'`

// An oversized codex event line is skipped, not fatal, and logged once (#1181).
func TestJudgeOversizedOutputLineIsLoggedNotFatal(t *testing.T) {
	runner, request, logs := newOversizedCodexFixture(t, smallCodexCatalogue)
	if _, err := runner.RunJudge(t.Context(), request); err != nil {
		t.Fatalf("judge ended by oversized output: %v", err)
	}
	var oversized []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) == nil && record["msg"] == "judge provider output oversized" {
			oversized = append(oversized, record)
		}
	}
	if len(oversized) != 1 {
		t.Fatalf("oversized output log: %s", logs.String())
	}
	total, _ := oversized[0]["total_bytes"].(float64)
	if oversized[0]["provider"] != "codex" || oversized[0]["attempt"] != "judge-oversized" || oversized[0]["kind"] != "line_dropped" || oversized[0]["largest_dropped_line_bytes"] != float64(1100000) || total <= 1100000 {
		t.Fatalf("oversized output log: %s", logs.String())
	}
}
