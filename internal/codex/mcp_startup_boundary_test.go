package codex

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// This is a real subprocess boundary with synthetic provider stderr, not proof
// that an installed Codex host can initialize MCP.
func TestRunnerMCPStartupBoundary(t *testing.T) {
	t.Parallel()
	const failure = "Error: thread/start: thread/start failed: error creating thread: Fatal error: Failed to initialize session: required MCP servers failed to initialize: litefactory: Mcp error: -32602: Invalid tool parameters"
	for _, noisy := range []bool{false, true} {
		name := "short stderr"
		stderr := failure + "\n"
		if noisy {
			name = "stderr exceeds retention limit"
			stderr = "discarded startup prefix\n" + strings.Repeat("startup trace line\n", 10000) + stderr
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner, _ := fakeRunner(t, "cat <<'STDERR' >&2\n"+stderr+"STDERR\nexit 7\n")
			got, err := runner.Run(t.Context(), request(t, agent.PathCold))
			if err != nil {
				t.Fatal(err)
			}
			if got.Outcome != agent.OutcomeFailed || got.ExitCode != 7 || got.Reason != "coding agent exited with status 7" {
				t.Fatalf("lost process cause: %+v", got)
			}
			if got.HasResult || got.ResultText != "" || len(mustRead(t, got.LogPath)) != 0 {
				t.Fatal("startup failure unexpectedly produced stdout events or a result")
			}
			if !strings.Contains(got.Diagnostic, "required MCP servers") || !strings.HasSuffix(got.Stderr, failure+"\n") {
				t.Fatalf("startup diagnosis or terminal stderr missing: %+v", got)
			}
			if noisy {
				if strings.Contains(got.Stderr, "discarded startup prefix") || !strings.HasPrefix(got.Stderr, "[earlier stderr dropped]\n") || len(got.Stderr) > agent.StderrTailBytes+len("[earlier stderr dropped]\n") {
					t.Fatalf("stderr tail was not bounded and marked: %d bytes", len(got.Stderr))
				}
			} else if got.Stderr != stderr {
				t.Fatalf("short stderr changed: %q", got.Stderr)
			}
		})
	}
}
