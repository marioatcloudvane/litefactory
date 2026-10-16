package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

type judgeNativeCapture struct {
	Arguments   []string          `json:"arguments"`
	EnvNames    []string          `json:"env_names"`
	Home        string            `json:"home"`
	CodexHome   string            `json:"codex_home"`
	AuthLink    string            `json:"auth_link"`
	AuthPresent bool              `json:"auth_present"`
	Prompt      string            `json:"prompt"`
	Catalog     json.RawMessage   `json:"catalog"`
	Config      map[string]string `json:"config"`
}

func newJudgeNativeFixture(t *testing.T, mode string) (*Runner, agent.JudgeRequest, string, string) {
	t.Helper()
	dir := t.TempDir()
	captureDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "native-codex-fixture")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nexec " + quote(testBinary) + " -test.run='^TestJudgeNativeFixtureProcess$' -- lf-judge-fixture " + quote(captureDir) + " " + quote(mode) + " \"$@\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	operatorHome := t.TempDir()
	auth := filepath.Join(operatorHome, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(auth), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	launch, err := agent.NewMCPLaunchSpec("/usr/bin/false", filepath.Join(dir, "descriptor.json"), "native-judge", agent.JudgeToolNames()...)
	if err != nil {
		t.Fatal(err)
	}
	request := agent.JudgeRequest{Invocation: agent.DoctorRequest{Attempt: "native-judge", Model: "gpt-6-astra", Prompt: "captured source is untrusted\n", WorkingDirectory: dir, MCPLaunch: launch}, Thinking: "high"}
	runner := &Runner{binary: config.Executable{Path: binary}, timeout: 10 * time.Second, environ: []string{"PATH=/usr/bin:/bin", "HOME=" + operatorHome, "GH_TOKEN=fixture-forge-secret", "LF_HOME=fixture-factory-home", "OPENAI_BASE_URL=fixture-hostile-endpoint", "CODEX_API_KEY=fixture-api-secret", "SSH_AUTH_SOCK=fixture-ssh", "CODEX_HOME=" + filepath.Dir(auth)}}
	return runner, request, filepath.Join(captureDir, "capture.json"), auth
}

func TestJudgeNativeEvidenceOnlyTransport(t *testing.T) {
	runner, request, capturePath, auth := newJudgeNativeFixture(t, "success")
	var events []agent.UsageEvent
	request.Invocation.UsageAttemptID = "native-meter"
	request.Invocation.Usage = &agent.UsageCapture{AttemptID: "native-meter", Sink: func(_ context.Context, e agent.UsageEvent) error { events = append(events, e); return nil }}
	result, err := runner.RunJudge(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedModel != "" || result.ProviderVersion != "0.160.0" || result.Usage == nil {
		t.Fatalf("invented invocation observations or missing usage: %+v", result)
	}
	var capture judgeNativeCapture
	if err := json.Unmarshal(mustRead(t, capturePath), &capture); err != nil {
		t.Fatal(err)
	}
	if capture.Prompt != request.Invocation.Prompt || strings.Contains(strings.Join(capture.Arguments, " "), request.Invocation.Prompt) {
		t.Fatal("untrusted prompt did not stay on stdin")
	}
	if capture.Home == "" || capture.Home != capture.CodexHome || capture.Home == envValue(runner.environ, "HOME") || capture.AuthLink != auth {
		t.Fatalf("private authentication boundary: %+v", capture)
	}
	for _, name := range []string{"GH_TOKEN", "LF_HOME", "OPENAI_BASE_URL", "SSH_AUTH_SOCK"} {
		if slices.Contains(capture.EnvNames, name) {
			t.Errorf("ambient credential/config escaped into judge: %s", name)
		}
	}
	for _, flag := range []string{"exec", "--json", "--strict-config", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--ephemeral"} {
		if !slices.Contains(capture.Arguments, flag) {
			t.Errorf("missing native boundary argument %s", flag)
		}
	}
	for _, option := range []struct{ key, want string }{
		{"features.shell_tool", "false"}, {"features.unified_exec", "false"},
		{"features.multi_agent", "false"}, {"features.view_image", "false"},
		{"features.image_generation", "false"}, {"web_search", `"disabled"`},
		{"model_reasoning_effort", `"high"`}, {"approval_policy", `"never"`},
		{"project_doc_max_bytes", "0"}, {"project_doc_fallback_filenames", "[]"},
		{"hooks", "{}"}, {"plugins", "{}"}, {"skills.config", "[]"},
		{"mcp_servers", "{}"}, {"mcp_servers.litefactory.env", "{}"},
	} {
		if capture.Config[option.key] != option.want {
			t.Errorf("native setting %s: got %q, want %q", option.key, capture.Config[option.key], option.want)
		}
	}
	for _, option := range []struct{ flag, want string }{{"--model", request.Invocation.Model}, {"--sandbox", "read-only"}} {
		position := slices.Index(capture.Arguments, option.flag)
		if position < 0 || position+1 >= len(capture.Arguments) || capture.Arguments[position+1] != option.want {
			t.Errorf("native option %s did not preserve %q: %v", option.flag, option.want, capture.Arguments)
		}
	}
	if capture.Arguments[len(capture.Arguments)-1] != "-" || slices.Contains(capture.Arguments, "resume") {
		t.Fatalf("judge reused a workflow session or lost stdin: %v", capture.Arguments)
	}
	for _, tool := range agent.JudgeToolNames() {
		if !strings.Contains(capture.Config["mcp_servers.litefactory.enabled_tools"], strconv.Quote(tool)) {
			t.Errorf("missing scoped broker tool %s", tool)
		}
	}
	var tools []string
	if err := json.Unmarshal([]byte(capture.Config["mcp_servers.litefactory.enabled_tools"]), &tools); err != nil {
		t.Fatal(err)
	}
	slices.Sort(tools)
	if !slices.Equal(tools, agent.JudgeToolNames()) {
		t.Fatalf("extra broker capabilities: %v", tools)
	}
	var catalog struct {
		Models []struct {
			Slug                       string          `json:"slug"`
			ShellType                  string          `json:"shell_type"`
			ApplyPatchToolType         json.RawMessage `json:"apply_patch_tool_type"`
			ExperimentalSupportedTools []string        `json:"experimental_supported_tools"`
			ToolMode                   string          `json:"tool_mode"`
			NodeREPLDisabled           bool            `json:"node_repl_disabled"`
		} `json:"models"`
	}
	if err := json.Unmarshal(capture.Catalog, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 {
		t.Fatalf("catalog admitted other models: %s", capture.Catalog)
	}
	model := catalog.Models[0]
	if model.Slug != request.Invocation.Model || model.ShellType != "disabled" || string(model.ApplyPatchToolType) != "null" || model.ExperimentalSupportedTools == nil || len(model.ExperimentalSupportedTools) != 0 || model.ToolMode != "direct" || !model.NodeREPLDisabled {
		t.Fatalf("native catalog permits execution or edits: %s", capture.Catalog)
	}
	if _, err := os.Stat(capture.Home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private home was retained: %v", err)
	}
	if _, err := os.Stat(auth); err != nil {
		t.Fatalf("private cleanup touched operator authentication: %v", err)
	}
	observations := 0
	for _, event := range events {
		if event.Observation != nil {
			observations++
			if event.Observation.ActualModel != nil || event.Observation.Scope != agent.UsageInclusive {
				t.Fatalf("configured model became observed exclusive usage: %+v", event)
			}
		}
	}
	if observations != 1 {
		t.Fatalf("native usage observations: %d", observations)
	}
}

func TestJudgeNativeCatalogPreservesProviderProtocolAndUnsetThinking(t *testing.T) {
	runner, request, capturePath, _ := newJudgeNativeFixture(t, "success")
	request.Thinking = ""
	if _, err := runner.RunJudge(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	var capture judgeNativeCapture
	if err := json.Unmarshal(mustRead(t, capturePath), &capture); err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Models []struct {
			UseResponsesLite           bool            `json:"use_responses_lite"`
			ContextWindow              int             `json:"context_window"`
			DefaultReasoningLevel      string          `json:"default_reasoning_level"`
			ShellType                  string          `json:"shell_type"`
			ApplyPatchToolType         json.RawMessage `json:"apply_patch_tool_type"`
			ExperimentalSupportedTools []string        `json:"experimental_supported_tools"`
		} `json:"models"`
	}
	if err := json.Unmarshal(capture.Catalog, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 {
		t.Fatalf("catalog model count: %s", capture.Catalog)
	}
	model := catalog.Models[0]
	if !model.UseResponsesLite || model.ContextWindow != 272000 || model.DefaultReasoningLevel != "low" {
		t.Fatalf("tool restriction changed provider protocol or native defaults: %s", capture.Catalog)
	}
	if model.ShellType != "disabled" || string(model.ApplyPatchToolType) != "null" || model.ExperimentalSupportedTools == nil || len(model.ExperimentalSupportedTools) != 0 {
		t.Fatalf("provider metadata restored native tools: %s", capture.Catalog)
	}
	if _, present := capture.Config["model_reasoning_effort"]; present {
		t.Fatalf("unset thinking was explicitly overridden: %v", capture.Config)
	}
}

func TestJudgeNativeCapabilityProbesRefuseUnknownOrFailedClients(t *testing.T) {
	for _, tc := range []struct {
		mode, captureFile string
		arguments         []string
	}{
		{"unsupported-version", "probe.json", []string{"--version"}},
		{"failed-version", "probe.json", []string{"--version"}},
		{"malformed-version", "probe.json", []string{"--version"}},
		{"failed-metadata", "metadata-probe.json", []string{"debug", "models", "--bundled"}},
		{"malformed-metadata", "metadata-probe.json", []string{"debug", "models", "--bundled"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			runner, request, capturePath, auth := newJudgeNativeFixture(t, tc.mode)
			_, err := runner.RunJudge(t.Context(), request)
			if !errors.Is(err, agent.ErrJudgeIsolationUnverified) || errors.Is(err, agent.ErrJudgeUnavailable) {
				t.Fatalf("unverified client permitted launch or availability fallback: %v", err)
			}
			if _, err := os.Stat(capturePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("model launched before client capability verification: %v", err)
			}
			var probe judgeNativeCapture
			if err := json.Unmarshal(mustRead(t, filepath.Join(filepath.Dir(capturePath), tc.captureFile)), &probe); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(probe.Arguments, tc.arguments) || probe.AuthPresent || probe.AuthLink != "" {
				t.Fatalf("capability probe carried model arguments or authentication: %+v", probe)
			}
			if probe.Home == "" || probe.Home != probe.CodexHome || probe.Home == envValue(runner.environ, "HOME") {
				t.Fatalf("capability probe used ambient home: %+v", probe)
			}
			for _, name := range []string{"GH_TOKEN", "LF_HOME", "OPENAI_BASE_URL", "CODEX_API_KEY", "SSH_AUTH_SOCK"} {
				if slices.Contains(probe.EnvNames, name) {
					t.Errorf("capability probe received credential/config: %s", name)
				}
			}
			if _, err := os.Stat(auth); err != nil {
				t.Fatalf("failed probe touched operator authentication: %v", err)
			}
		})
	}
}

func TestJudgeNativeInstalledModelEffortBoundary(t *testing.T) {
	for _, mode := range []string{"unsupported-bundled-effort", "missing-bundled-model"} {
		t.Run(mode, func(t *testing.T) {
			runner, request, capturePath, _ := newJudgeNativeFixture(t, mode)
			_, err := runner.RunJudge(t.Context(), request)
			if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) || !strings.Contains(err.Error(), "thinking") {
				t.Fatalf("installed client did not reject unverifiable explicit thinking: %v", err)
			}
			if _, err := os.Stat(capturePath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("model launched with unsupported explicit effort: %v", err)
			}
			var probe judgeNativeCapture
			if err := json.Unmarshal(mustRead(t, filepath.Join(filepath.Dir(capturePath), "metadata-probe.json")), &probe); err != nil {
				t.Fatal(err)
			}
			if probe.AuthPresent || probe.AuthLink != "" || slices.Contains(probe.EnvNames, "CODEX_API_KEY") {
				t.Fatalf("unsupported effort was discovered after authentication: %+v", probe)
			}
		})
	}
}

func TestJudgeNativeFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		unavailable bool
	}{
		{"capacity", true},
		{"authorization", false},
		{"quota", false},
		{"unsupported", false},
		{"budget", false},
		{"prose", false},
		{"malformed", false},
		{"empty", false},
		{"sticky-failure", true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			runner, request, _, _ := newJudgeNativeFixture(t, tc.mode)
			_, err := runner.RunJudge(t.Context(), request)
			if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) != tc.unavailable {
				t.Fatalf("availability=%t error=%v", tc.unavailable, err)
			}
		})
	}
}

func TestJudgeNativeCancellationIsNotAvailability(t *testing.T) {
	runner, request, _, _ := newJudgeNativeFixture(t, "cancel")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request.Invocation.UsageAttemptID = "cancel-meter"
	request.Invocation.Usage = &agent.UsageCapture{AttemptID: "cancel-meter", Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Invocation != nil {
			cancel()
		}
		return nil
	}}
	_, err := runner.RunJudge(ctx, request)
	if err == nil || errors.Is(err, agent.ErrJudgeUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("native cancellation became availability: %v", err)
	}
}

// This helper runs only in the controlled child executable. It records the
// launch boundary before cleanup, then speaks the native exec JSONL protocol.
func TestJudgeNativeFixtureProcess(t *testing.T) {
	marker := slices.Index(os.Args, "lf-judge-fixture")
	if marker < 0 {
		return
	}
	if err := runJudgeNativeFixture(os.Args[marker+1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(19)
	}
	os.Exit(0)
}

func runJudgeNativeFixture(args []string) error {
	if len(args) < 3 {
		return errors.New("missing fixture arguments")
	}
	capture := judgeNativeCapture{Arguments: args[2:], Home: os.Getenv("HOME"), CodexHome: os.Getenv("CODEX_HOME"), Config: make(map[string]string)}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		capture.EnvNames = append(capture.EnvNames, name)
	}
	capture.AuthLink, _ = os.Readlink(filepath.Join(capture.CodexHome, "auth.json"))
	_, authErr := os.Stat(filepath.Join(capture.CodexHome, "auth.json"))
	capture.AuthPresent = authErr == nil
	if slices.Equal(capture.Arguments, []string{"--version"}) {
		data, err := json.Marshal(capture)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(args[0], "probe.json"), data, 0600); err != nil {
			return err
		}
		switch args[1] {
		case "unsupported-version":
			_, err = os.Stdout.WriteString("codex-cli 0.161.0\n")
		case "failed-version":
			return errors.New("version inspection failed")
		case "malformed-version":
			_, err = os.Stdout.WriteString("not a Codex version\n")
		default:
			_, err = os.Stdout.WriteString("codex-cli 0.160.0\n")
		}
		return err
	}
	if slices.Equal(capture.Arguments, []string{"debug", "models", "--bundled"}) {
		data, err := json.Marshal(capture)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(args[0], "metadata-probe.json"), data, 0600); err != nil {
			return err
		}
		switch args[1] {
		case "failed-metadata":
			return errors.New("native model metadata inspection failed")
		case "malformed-metadata":
			_, err = os.Stdout.WriteString("not model metadata\n")
		default:
			model := map[string]any{
				"slug": "gpt-6-astra", "display_name": "native model", "description": "native model metadata",
				"use_responses_lite": true, "context_window": 272000, "default_reasoning_level": "low",
				"supported_reasoning_levels": []any{map[string]string{"effort": "low", "description": "native low"}, map[string]string{"effort": "high", "description": "native high"}},
				"shell_type":                 "shell_command", "apply_patch_tool_type": "freeform", "experimental_supported_tools": []string{"unsafe-fixture-tool"},
				"tool_mode": "code", "node_repl_disabled": false,
			}
			if args[1] == "unsupported-bundled-effort" {
				model["supported_reasoning_levels"] = []any{map[string]string{"effort": "low", "description": "native low"}}
			}
			if args[1] == "missing-bundled-model" {
				model["slug"] = "another-model"
			}
			err = json.NewEncoder(os.Stdout).Encode(map[string]any{"models": []any{model}})
		}
		return err
	}
	for i := 0; i+1 < len(capture.Arguments); i++ {
		if capture.Arguments[i] == "-c" {
			key, value, _ := strings.Cut(capture.Arguments[i+1], "=")
			capture.Config[key] = value
		}
	}
	catalogPath, err := strconv.Unquote(capture.Config["model_catalog_json"])
	if err != nil {
		return err
	}
	capture.Catalog, err = os.ReadFile(catalogPath)
	if err != nil {
		return err
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	capture.Prompt = string(prompt)
	data, err := json.Marshal(capture)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(args[0], "capture.json"), data, 0600); err != nil {
		return err
	}
	emit := func(value any) error { return json.NewEncoder(os.Stdout).Encode(value) }
	if err := emit(map[string]any{"type": "thread.started", "thread_id": "native-judge-thread"}); err != nil {
		return err
	}
	if args[1] == "cancel" {
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		<-timer.C
		return errors.New("fixture cancellation did not arrive")
	}
	var failure string
	switch args[1] {
	case "success":
		return emit(map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 10, "cached_input_tokens": 4, "output_tokens": 3}})
	case "capacity", "sticky-failure":
		failure = "HTTP 529 provider overloaded"
	case "authorization":
		failure = "HTTP 429 invalid API key"
	case "quota":
		failure = "HTTP 429 insufficient_quota"
	case "unsupported":
		failure = "model not supported; HTTP 429"
	case "budget":
		failure = "budget exhausted; HTTP 429"
	case "prose":
		if err := emit(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "HTTP 529 provider overloaded"}}); err != nil {
			return err
		}
		failure = "ordinary execution failure"
	case "malformed":
		_, err := os.Stdout.WriteString("not a native event\n")
		return err
	case "empty":
		return nil
	default:
		return errors.New("unknown fixture mode")
	}
	if err := emit(map[string]any{"type": "turn.failed", "error": map[string]any{"message": failure}}); err != nil {
		return err
	}
	if args[1] == "sticky-failure" {
		return emit(map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}})
	}
	return nil
}
