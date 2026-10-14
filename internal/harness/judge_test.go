package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestJudgeScopedTransport(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then printf '%s\n' '--effort <level> (choices: low, medium, high, xhigh, max)'; exit 0; fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"judge-session","claude_code_version":"fixture"}' '{"type":"assistant","session_id":"judge-session","message":{"id":"m","model":"opus","usage":{"input_tokens":4,"output_tokens":2}}}' '{"type":"result","is_error":false}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-attempt", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-attempt", Model: "opus", Prompt: "challenge saved evidence", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: time.Second * 10, environ: []string{"PATH=/usr/bin:/bin"}}
	result, err := runner.RunJudge(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedModel != "opus" {
		t.Fatalf("model: %+v", result)
	}
	request.Invocation.Thinking = request.Thinking
	args, err := scopedDoctorArguments(request.Invocation, "/private/mcp.json", "judge")
	if err != nil {
		t.Fatal(err)
	}
	for i, arg := range args {
		if arg == "--tools" && args[i+1] != "" {
			t.Fatal("judge exposes native tools")
		}
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--effort high") || !strings.Contains(joined, "--no-session-persistence") {
		t.Fatalf("missing effort/isolation: %s", joined)
	}
	for _, tool := range agent.JudgeToolNames() {
		if !strings.Contains(joined, "mcp__litefactory__"+tool) {
			t.Fatal("missing scoped tool", tool)
		}
	}
	if _, err = runner.RunDoctorRepair(t.Context(), agent.RepairRequest{Invocation: request.Invocation, Operation: "judge"}); err == nil {
		t.Fatal("judge accepted as repair")
	}
}

func TestNativeRequestThinkingOption(t *testing.T) {
	runner := &Runner{model: "sonnet", settingsPath: "/private/settings.json"}
	args := runner.args(agent.Request{Model: "opus", Thinking: "max"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--model opus") || !strings.Contains(joined, "--effort max") {
		t.Fatalf("native overrides missing: %s", joined)
	}
}

func TestJudgeNativeAvailabilityUsesOnlyProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name, terminal string
		unavailable    bool
	}{
		{"capacity", `{"type":"result","is_error":true,"errors":["Selected model is at capacity"]}`, true},
		{"http", `{"type":"result","is_error":true,"errors":["HTTP 529"]}`, true},
		{"prose", `{"type":"result","is_error":true,"result":"Selected model is at capacity"}`, false},
		{"auth", `{"type":"result","is_error":true,"errors":["HTTP 429 invalid api key"]}`, false},
		{"quota", `{"type":"result","is_error":true,"errors":["HTTP 429 insufficient_quota"]}`, false},
		{"unsupported", `{"type":"result","is_error":true,"errors":["model is not supported; HTTP 429"]}`, false},
		{"budget", `{"type":"result","is_error":true,"errors":["budget exhausted; HTTP 429"]}`, false},
		{"success", `{"type":"result","is_error":false,"errors":["HTTP 529"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(t.TempDir(), "fixture")
			script := "#!/bin/sh\nprintf '%s\\n' '" + tc.terminal + "'\nexit 1\n"
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-capacity", agent.JudgeToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			runner := &Runner{binary: config.Executable{Path: binary}, timeout: time.Second * 10, environ: []string{"PATH=/usr/bin:/bin"}}
			_, err = runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-capacity", Model: "opus", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}})
			if errors.Is(err, agent.ErrJudgeUnavailable) != tc.unavailable {
				t.Fatalf("availability=%t error=%v", tc.unavailable, err)
			}
		})
	}
}

func TestJudgeEffortHelpCapabilityCeiling(t *testing.T) {
	for _, tc := range []struct {
		help     string
		thinking config.Thinking
		valid    bool
	}{
		{"--effort <level>", "max", true},
		{"--effort <level> (choices: low, medium, high)", "high", true},
		{"--effort <level> (choices: low, medium, high)", "max", false},
		{"--effort <level>\n    choices: low, medium, high, xhigh, max", "xhigh", true},
		{"--model <model>", "high", false},
	} {
		if err := validateJudgeEffortHelp(tc.help, tc.thinking); (err == nil) != tc.valid {
			t.Errorf("help=%q effort=%q valid=%t err=%v", tc.help, tc.thinking, tc.valid, err)
		}
	}
}

func TestJudgeMissingClientIsAvailability(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "missing-client")
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "missing-judge", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, environ: []string{"PATH=/usr/bin:/bin"}}
	_, err = runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "missing-judge", Model: "opus", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"})
	if !errors.Is(err, agent.ErrJudgeUnavailable) {
		t.Fatalf("missing executable was not availability: %v", err)
	}
}

func TestJudgeRefusesUnsupportedObservedAliasModel(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then printf '%s\n' '--effort <level> (choices: low, medium, high, xhigh, max)'; exit 0; fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"alias-session","claude_code_version":"fixture"}' '{"type":"assistant","session_id":"alias-session","message":{"id":"m","model":"claude-3-5-sonnet","usage":{"input_tokens":4,"output_tokens":2}}}' '{"type":"result","is_error":false}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-alias", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin"}}
	result, err := runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-alias", Model: "sonnet", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"})
	if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) || !strings.Contains(err.Error(), "observed judge model") {
		t.Fatalf("unsupported observed model did not refuse: %+v %v", result, err)
	}
	if result.ObservedModel != "claude-3-5-sonnet" {
		t.Fatalf("lost observed invocation provenance: %+v", result)
	}
}
