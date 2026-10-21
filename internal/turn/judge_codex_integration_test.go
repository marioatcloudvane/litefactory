package turn

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestJudgeNativeCodexSharedService(t *testing.T) {
	for _, tc := range []struct {
		name, selection, behavior string
		task                      config.Kind
		fallback, refused         bool
	}{
		{"explicit", "codex", "success", config.KindClaude, false, false},
		{"task default", "", "success", config.KindCodex, false, false},
		{"opposite Claude source", "other", "success", config.KindClaude, false, false},
		{"one availability fallback", "other", "unavailable", config.KindClaude, true, false},
		{"unsupported client refuses without fallback", "other", "unsupported-client", config.KindClaude, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, request, writer := judgeRecoveryFixture(t)
			request.TaskProvider = tc.task
			request.Options.JudgeOptions = config.JudgeOptions{Agent: tc.selection, Thinking: "high"}
			binary, nativeExecution := judgeCodexFixtureBinary(t, tc.behavior)
			runner, err := codex.New(codex.Options{Paths: service.Paths, Codex: config.Codex{Binary: binary, TimeoutSeconds: 20}, Model: "gpt-6-astra", Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err != nil {
				t.Fatal(err)
			}
			fallback := &recoveryJudgeRunner{}
			service.Providers[agent.Kind("codex")] = runner
			service.Providers[agent.Kind("claude")] = fallback
			body, err := service.Execute(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			execution := "completed"
			if tc.refused {
				execution = "failed"
			}
			saved, err := service.DB.FindJudgeRequest(t.Context(), request.TicketKey, request.RemoteCommentID)
			if err != nil {
				t.Fatal(err)
			}
			report := decodeJudgeSavedReport(t, saved.Report)
			if body == "" || report.Execution != execution || report.Presentation.SourceReference == "" {
				t.Fatalf("native request did not produce an accepted, source-identified advisory: %s", saved.Report)
			}
			assertJudgeTextWithoutProvenance(t, body)
			attempts, err := service.DB.ListJudgeAttempts(t.Context(), saved.ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := 1
			if tc.fallback {
				expected = 2
			}
			if len(attempts) != expected || attempts[0].Provider != "codex" {
				t.Fatalf("attempts: %+v", attempts)
			}
			switch {
			case tc.refused:
				if len(fallback.calls) != 0 || attempts[0].State != "failed" || report.Presentation.FallbackReason != "" || !strings.Contains(body, "unable to assess") {
					t.Fatalf("unsupported client granted fallback or clean verdict: %+v %s", attempts, saved.Report)
				}
			case tc.fallback:
				if len(fallback.calls) != 1 || report.Presentation.FallbackReason == "" || report.Presentation.Provider != "claude" || attempts[1].Provider != "claude" {
					t.Fatalf("fallback not bounded: %+v %s", attempts, saved.Report)
				}
			case len(fallback.calls) != 0 || report.Presentation.Provider != "codex" || !strings.Contains(body, "Native Codex captured source inspected"):
				t.Fatalf("native Codex assessment replaced: %s %s", body, saved.Report)
			}
			if !tc.refused && attempts[0].ProviderVersion != "0.160.0" {
				t.Fatalf("native version provenance missing: %+v", attempts[0])
			}
			if !tc.refused && !tc.fallback && report.Presentation.ProviderVersion != "0.160.0" {
				t.Fatalf("native version omitted from saved advisory: %s", saved.Report)
			}
			executed, executionErr := os.ReadFile(nativeExecution)
			if tc.refused {
				if !errors.Is(executionErr, os.ErrNotExist) {
					t.Fatalf("unsupported client reached model execution: %q %v", executed, executionErr)
				}
			} else if executionErr != nil || string(executed) != "1" {
				t.Fatalf("native execution count: %q %v", executed, executionErr)
			}
			if err = service.DB.CheckResultWriter(t.Context(), writer); err != nil {
				t.Fatalf("source writer changed: %v", err)
			}
			again, err := service.Execute(t.Context(), request)
			if err != nil || again != body {
				t.Fatalf("saved native result replay: %v", err)
			}
			replayAttempts, err := service.DB.ListJudgeAttempts(t.Context(), saved.ID)
			if err != nil || len(replayAttempts) != expected {
				t.Fatalf("saved native result relaunched: %+v %v", replayAttempts, err)
			}
			replayed, replayErr := os.ReadFile(nativeExecution)
			if string(replayed) != string(executed) || tc.refused && !errors.Is(replayErr, os.ErrNotExist) || !tc.refused && replayErr != nil {
				t.Fatalf("saved report replay executed native model: %q %v", replayed, replayErr)
			}
		})
	}
}

func judgeCodexFixtureBinary(t *testing.T, behavior string) (string, string) {
	t.Helper()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot := t.TempDir()
	binary := filepath.Join(fixtureRoot, "codex-fixture")
	nativeExecution := filepath.Join(fixtureRoot, "native-executed")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nexec " + quote(testBinary) + " -test.run=^TestJudgeCodexNativeFixture$ -- lf-judge-native-fixture " + quote(behavior) + " " + quote(nativeExecution) + " \"$@\"\n"
	if err = os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return binary, nativeExecution
}

// This subprocess implements only the controlled native provider's protocol. It
// never invokes a paid client. Its broker calls still use the real durable path.
func TestJudgeCodexNativeFixture(t *testing.T) {
	marker := slices.Index(os.Args, "lf-judge-native-fixture")
	if marker < 0 {
		return
	}
	args := os.Args[marker+3:]
	if slices.Contains(args, "--version") {
		if os.Args[marker+1] == "unsupported-client" {
			fmt.Println("codex-cli 0.161.0")
		} else {
			fmt.Println("codex-cli 0.160.0")
		}
		os.Exit(0)
	}
	if slices.Equal(args, []string{"debug", "models", "--bundled"}) {
		fmt.Println(`{"models":[{"slug":"gpt-6-astra","display_name":"Controlled native model","use_responses_lite":true,"shell_type":"shell_command","apply_patch_tool_type":"freeform","experimental_supported_tools":["native-dangerous-tool"],"supported_reasoning_levels":[{"effort":"high","description":"High"}],"default_reasoning_level":"high"}]}`)
		os.Exit(0)
	}
	if slices.Contains(args, "--help") {
		fmt.Println("--json --strict-config --ignore-user-config --ignore-rules --ephemeral --sandbox --model --config model_catalog_json")
		os.Exit(0)
	}
	count, err := os.OpenFile(os.Args[marker+2], os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = count.WriteString("1"); err != nil {
		t.Fatal(err)
	}
	if err = count.Close(); err != nil {
		t.Fatal(err)
	}
	if os.Args[marker+1] == "unsupported-client" {
		t.Fatal("unsupported client reached model execution")
	}
	if os.Args[marker+1] == "unavailable" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"HTTP 503 service unavailable"}}`)
		os.Exit(1)
	}
	descriptor, catalogPath := "", ""
	for _, arg := range args {
		if strings.HasPrefix(arg, "model_catalog_json=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(arg, "model_catalog_json=")), &catalogPath); err != nil {
				t.Fatal(err)
			}
		}
		if !strings.HasPrefix(arg, "mcp_servers.litefactory.args=") {
			continue
		}
		var mcpArgs []string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(arg, "mcp_servers.litefactory.args=")), &mcpArgs); err != nil {
			t.Fatal(err)
		}
		for i := 0; i+1 < len(mcpArgs); i++ {
			if mcpArgs[i] == "--descriptor" {
				descriptor = mcpArgs[i+1]
			}
		}
	}
	if descriptor == "" {
		t.Fatal("native invocation did not register its scoped broker")
	}
	encodedCatalog, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	var nativeCatalog struct {
		Models []struct {
			Slug          string   `json:"slug"`
			ResponsesLite bool     `json:"use_responses_lite"`
			Shell         string   `json:"shell_type"`
			Patch         any      `json:"apply_patch_tool_type"`
			Experimental  []string `json:"experimental_supported_tools"`
		} `json:"models"`
	}
	if err = json.Unmarshal(encodedCatalog, &nativeCatalog); err != nil || len(nativeCatalog.Models) != 1 {
		t.Fatalf("native catalog: %s %v", encodedCatalog, err)
	}
	model := nativeCatalog.Models[0]
	if model.Slug != "gpt-6-astra" || !model.ResponsesLite || model.Shell != "disabled" || model.Patch != nil || len(model.Experimental) != 0 {
		t.Fatalf("native wire metadata or tool ceiling changed: %s", encodedCatalog)
	}
	client := submission.Client{DescriptorPath: descriptor}
	catalog, err := client.List(t.Context())
	if err != nil || catalog.Error != "" {
		t.Fatalf("catalog: %s %v", catalog.Error, err)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err = json.Unmarshal(catalog.Result, &list); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, agent.JudgeToolNames()) {
		t.Fatalf("native broker tools: %v", names)
	}
	call := func(name string, input any) submission.Response {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Call(t.Context(), name, raw)
		if err != nil || response.Error != "" {
			t.Fatalf("%s: %s %v", name, response.Error, err)
		}
		return response
	}
	call("evidence_list", map[string]any{})
	source := call("evidence_read", map[string]any{"id": "source", "start": 1, "count": 1})
	expectedSource := "Saved conclusion"
	if os.Args[marker+1] == "plan-success" {
		expectedSource = `\"name\":\"Saved plan\"`
	}
	if !strings.Contains(string(source.Result), expectedSource) {
		t.Fatalf("pinned source missing: %s", source.Result)
	}
	input := agentresult.JudgeInput{Report: agentresult.JudgeReport{Version: 1, Verdict: "unable_to_assess", Summary: "Native Codex captured source inspected", Coverage: []string{"source"}, Uncertainty: []string{"Repository snapshot is unavailable."}, Findings: []agentresult.JudgeFinding{}}}
	preflight := call("communication_preflight", input)
	var diagnostics agentresult.Diagnostics
	if err = json.Unmarshal(preflight.Result, &diagnostics); err != nil || !diagnostics.Valid {
		t.Fatalf("preflight: %s %v", preflight.Result, err)
	}
	submitted := call("communication_submit", input)
	call("communication_receipt", map[string]string{"request_id": submitted.RequestID})
	fmt.Println(`{"type":"thread.started","thread_id":"native-judge-fixture"}`)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":12,"cached_input_tokens":0,"output_tokens":6}}`)
	os.Exit(0)
}
