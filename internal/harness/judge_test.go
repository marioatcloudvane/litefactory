package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestJudgeScopedTransport(t *testing.T) {
	dir := t.TempDir()
	contextDir := t.TempDir()
	observed := filepath.Join(t.TempDir(), "config-dir")
	mcpObserved := filepath.Join(t.TempDir(), "mcp-config")
	binary := filepath.Join(t.TempDir(), "fixture")
	script := `#!/bin/sh
if [ "$1" = "--help" ]; then printf '%s\n' '--effort <level> (choices: low, medium, high, xhigh, max)'; exit 0; fi
printf '%s' "$CLAUDE_CONFIG_DIR" > ` + observed + `
while [ $# -gt 0 ]; do if [ "$1" = "--mcp-config" ]; then printf '%s' "$2" > ` + mcpObserved + `; fi; shift; done
printf '%s\n' '{"type":"system","subtype":"init","session_id":"judge-session","claude_code_version":"fixture"}' '{"type":"assistant","session_id":"judge-session","message":{"id":"m","model":"opus","usage":{"input_tokens":4,"output_tokens":2}}}' '{"type":"result","is_error":false}'
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-attempt", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	osHome := t.TempDir()
	credentials := filepath.Join(osHome, ".claude", ".credentials.json")
	if err = os.MkdirAll(filepath.Dir(credentials), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(credentials, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	persistent := filepath.Join(t.TempDir(), "claude-home")
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-attempt", Model: "opus", Prompt: "challenge saved work", WorkingDirectory: dir, ReadableDirectories: []string{contextDir}, MCPLaunch: launch}, Thinking: "high"}
	runner := &Runner{claudeConfigDir: persistent, osHome: osHome, binary: config.Executable{Path: binary}, timeout: time.Second * 10, environ: []string{"PATH=/usr/bin:/bin"}}
	result, err := runner.RunJudge(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedModel != "opus" {
		t.Fatalf("model: %+v", result)
	}
	// The judge runs under the factory's persistent claude home (#1057).
	home, err := os.ReadFile(observed)
	if err != nil || string(home) != persistent {
		t.Fatalf("judge config dir %q, want the persistent home %q (%v)", home, persistent, err)
	}
	if target, err := os.Readlink(filepath.Join(persistent, ".credentials.json")); err != nil || target != credentials {
		t.Fatalf("persistent home did not survive the run: %q %v", target, err)
	}
	mcpPath, err := os.ReadFile(mcpObserved)
	if err != nil || len(mcpPath) == 0 {
		t.Fatalf("mcp config not observed: %v", err)
	}
	if strings.HasPrefix(string(mcpPath), persistent+string(filepath.Separator)) {
		t.Fatalf("per-run mcp.json written into the persistent home: %s", mcpPath)
	}
	if _, err := os.Stat(filepath.Dir(string(mcpPath))); !os.IsNotExist(err) {
		t.Fatalf("per-run scratch dir %s survived: %v", filepath.Dir(string(mcpPath)), err)
	}
	// The persistent home holds the credentials link; it must not sit in a root the judge reads.
	for _, root := range []string{dir, contextDir} {
		if pathsOverlap(persistent, root) {
			t.Fatalf("persistent home %s is readable by the judge under %s", persistent, root)
		}
	}
	request.Invocation.Thinking = request.Thinking
	args, err := scopedDoctorArguments(request.Invocation, "/private/mcp.json", persistent, "judge")
	if err != nil {
		t.Fatal(err)
	}
	value := func(flag string) string {
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
		}
		t.Fatalf("missing %s: %v", flag, args)
		return ""
	}
	if tools := value("--tools"); tools != "Read,Grep,Glob" {
		t.Fatalf("judge native tools are not read-only: %q", tools)
	}
	if value("--disallowedTools") != "Read(/"+persistent+"/**)" {
		t.Fatalf("judge may read the persistent claude home: %v", args)
	}
	if value("--permission-mode") != "dontAsk" || value("--add-dir") != contextDir {
		t.Fatalf("judge cannot read context or may ask: %v", args)
	}
	allowed := strings.Split(value("--allowedTools"), ",")
	want := []string{"Read(/" + dir + "/**)", "Read(/" + contextDir + "/**)"}
	for _, tool := range agent.JudgeToolNames() {
		want = append(want, "mcp__litefactory__"+tool)
	}
	slices.Sort(allowed)
	slices.Sort(want)
	if !slices.Equal(allowed, want) {
		t.Fatalf("judge allowlist %v, want %v", allowed, want)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--effort high") || !strings.Contains(joined, "--no-session-persistence") || strings.Contains(joined, "--settings") {
		t.Fatalf("missing effort/isolation: %s", joined)
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
			runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, timeout: time.Second * 10, environ: []string{"PATH=/usr/bin:/bin"}}
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
	runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, environ: []string{"PATH=/usr/bin:/bin"}}
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
	runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin"}}
	result, err := runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-alias", Model: "sonnet", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"})
	if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) || !strings.Contains(err.Error(), "observed judge model") {
		t.Fatalf("unsupported observed model did not refuse: %+v %v", result, err)
	}
	if result.ObservedModel != "claude-3-5-sonnet" {
		t.Fatalf("lost observed invocation provenance: %+v", result)
	}
}

func TestJudgeRefusesReadableRootContainingClaudeHome(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "launched")
	binary := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-overlap", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	contextDir := t.TempDir()
	runner := &Runner{claudeConfigDir: filepath.Join(contextDir, "claude-home"), binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin"}}
	_, err = runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-overlap", Model: "opus", Prompt: "challenge", WorkingDirectory: dir, ReadableDirectories: []string{contextDir}, MCPLaunch: launch}})
	if err == nil {
		t.Fatal("judge launched with the claude home inside a readable root")
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("judge provider launched")
	}
}

func TestJudgeProviderFailureLogsExitCodeAndRedactedStderr(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fixture")
	secret := "sk-ant-oat01-" + strings.Repeat("Q", 40)
	script := "#!/bin/sh\nprintf '%s\\n' 'OAuth token refresh failed: invalid_grant' 'token " + secret + "' >&2\nexit 7\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "judge-failed", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	runner := &Runner{claudeConfigDir: t.TempDir(), logger: slog.New(slog.NewJSONHandler(&logs, nil)), binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin"}}
	_, err = runner.RunJudge(t.Context(), agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "judge-failed", Model: "opus", Prompt: "challenge", WorkingDirectory: dir, MCPLaunch: launch}})
	if err == nil {
		t.Fatal("failed provider reported success")
	}
	if strings.Contains(err.Error(), "invalid_grant") || strings.Contains(err.Error(), secret) {
		t.Fatalf("stderr crossed into the returned error: %v", err)
	}
	var line struct {
		Attempt  string `json:"attempt"`
		ExitCode int    `json:"exit_code"`
		Stderr   string `json:"stderr"`
	}
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("no failure log line: %q %v", logs.String(), err)
	}
	if line.Attempt != "judge-failed" || line.ExitCode != 7 || !strings.Contains(line.Stderr, "invalid_grant") {
		t.Fatalf("failure log: %s", logs.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("stderr logged unredacted: %s", logs.String())
	}
}
