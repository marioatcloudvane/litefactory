package harness

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestDoctorFinalExclusiveTranscriptUsage(t *testing.T) {
	for _, finalOutput := range []int{3, 19} {
		t.Run(fmt.Sprint(finalOutput), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			binary := filepath.Join(t.TempDir(), "fixture")
			script := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do [ "$arg" != "--no-session-persistence" ] || exit 9; done
printf '%%s\n' '{"type":"system","subtype":"init","session_id":"s","claude_code_version":"fixture"}' '{"type":"assistant","session_id":"s","message":{"id":"m","model":"observed","usage":{"input_tokens":10,"cache_read_input_tokens":2,"cache_creation_input_tokens":0,"output_tokens":3}}}'
mkdir -p "$CLAUDE_CONFIG_DIR/projects/%s"
printf '%%s\n' '{"type":"assistant","sessionId":"s","message":{"id":"m","model":"observed","usage":{"input_tokens":10,"cache_read_input_tokens":2,"cache_creation_input_tokens":0,"output_tokens":%d}}}' > "$CLAUDE_CONFIG_DIR/projects/%s/s.jsonl"
printf '%%s\n' '{"type":"result","session_id":"s","modelUsage":{"observed":{"inputTokens":10,"cacheReadInputTokens":2,"cacheCreationInputTokens":0,"outputTokens":19}}}'
`, transcriptProjectKey(dir), finalOutput, transcriptProjectKey(dir))
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			spec, err := agent.NewMCPLaunchSpec(binary, filepath.Join(dir, "descriptor.json"), "attempt", agent.DoctorToolNames()...)
			if err != nil {
				t.Fatal(err)
			}
			var input, output int64
			request := agent.DoctorRequest{Attempt: "attempt", Model: "configured", Prompt: "fixture", WorkingDirectory: dir, MCPLaunch: spec, Sandbox: &agent.DoctorSandbox{Version: agent.DoctorSandboxVersion, ScratchDirectory: dir, ReadableRoots: []string{"/"}, DeniedReadPaths: []string{"/credential-fixture"}}, UsageAttemptID: "usage", Usage: &agent.UsageCapture{AttemptID: "usage", Sink: func(_ context.Context, e agent.UsageEvent) error {
				if o := e.Observation; o != nil && o.Scope == agent.UsageExclusive {
					input += *o.Tokens.InputUncached
					output += *o.Tokens.Output
				}
				return nil
			}}}
			runner := &Runner{claudeConfigDir: t.TempDir(), binary: config.Executable{Path: binary}, timeout: time.Minute, environ: []string{"PATH=/usr/bin:/bin"}}
			result, err := runner.RunDoctor(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if input != 10 || output != int64(finalOutput) {
				t.Fatalf("exclusive totals input=%d output=%d", input, output)
			}
			if result.Usage == nil || (result.Usage.Status == agent.CollectionComplete) != (finalOutput == 19) {
				t.Fatalf("coverage %+v", result.Usage)
			}
			if finalOutput == 3 && !slices.Contains(result.Usage.Diagnostics, "inclusive_summary_mismatch_output") {
				t.Fatalf("missing output scope: %+v", result.Usage)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("private cleanup: %v %v", entries, err)
			}
		})
	}
}
