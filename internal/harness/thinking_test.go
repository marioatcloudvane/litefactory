package harness

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

const thinkingLine = `{"type":"assistant","message":{"model":"claude-sonnet-4-6","content":[{"type":"thinking","thinking":"I should read the handler first.","signature":"sig"},{"type":"text","text":"ok"}]}}`

// thinkingFixture is a fake claude that answers the --thinking-display probe
// the way a supporting (or an older, ignoring) binary does, counts probes in
// a file, and otherwise behaves like a successful run.
func thinkingFixture(t *testing.T, supported bool, level config.ThinkingLogs) (*fixture, string, *bytes.Buffer) {
	t.Helper()
	probes := filepath.Join(t.TempDir(), "probes")
	answer := "echo '2.1.200 (Claude Code)'; exit 0"
	if supported {
		answer = "echo \"error: option '--thinking-display <display>' argument '$2' is invalid. Allowed choices are summarized, omitted, highlights.\" >&2; exit 1"
	}
	script := `if [ "$1" = --thinking-display ] && [ "$3" = --version ]; then printf 'probe\n' >> '` + probes + `'; ` + answer + `; fi
` + dumpPrologue + emit(t, initLine) + emit(t, thinkingLine) + emit(t, resultLine) + "exit 0\n"
	f := newFixture(t, script)
	var logs bytes.Buffer
	f.runner.logger = slog.New(slog.NewTextHandler(&logs, nil))
	f.runner.thinkingLogs = level
	return f, probes, &logs
}

func probeCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "probe")
}

func thinkingFlagValue(argv []string) (string, bool) {
	i := slices.Index(argv, thinkingDisplayFlag)
	if i < 0 || i+1 >= len(argv) {
		return "", false
	}
	return argv[i+1], true
}

func TestThinkingLogsMapToClaudeThinkingDisplay(t *testing.T) {
	for _, tt := range []struct {
		level  config.ThinkingLogs
		want   string
		probes int
	}{
		{config.ThinkingLogsNone, "", 0},
		{"", "", 0},
		{config.ThinkingLogsSummarized, "summarized", 1},
		{config.ThinkingLogsFull, "summarized", 1},
	} {
		t.Run(string(tt.level), func(t *testing.T) {
			f, probes, _ := thinkingFixture(t, true, tt.level)
			for range 2 {
				result := f.run(t)
				if result.Outcome != OutcomeSucceeded {
					t.Fatalf("outcome = %v, reason %q", result.Outcome, result.Reason)
				}
				argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
				got, ok := thinkingFlagValue(argv)
				if tt.want == "" && (ok || slices.Contains(argv, thinkingDisplayFlag)) {
					t.Fatalf("argv carries %s for %q: %q", thinkingDisplayFlag, tt.level, argv)
				}
				if tt.want != "" && got != tt.want {
					t.Fatalf("argv %s = %q, want %q: %q", thinkingDisplayFlag, got, tt.want, argv)
				}
				if !slices.Equal(result.Tools, []string{"Read", "Edit"}) {
					t.Fatalf("thinking changed the observed tool surface: %q", result.Tools)
				}
			}
			if got := probeCount(t, probes); got != tt.probes {
				t.Fatalf("probes = %d, want %d (once per binary)", got, tt.probes)
			}
		})
	}
}

func TestThinkingLogsUnsupportedClaudeRunsWithoutTheFlagAndWarnsOnce(t *testing.T) {
	f, probes, logs := thinkingFixture(t, false, config.ThinkingLogsFull)
	for range 3 {
		result := f.run(t)
		if result.Outcome != OutcomeSucceeded {
			t.Fatalf("outcome = %v, reason %q", result.Outcome, result.Reason)
		}
		argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
		if slices.Contains(argv, thinkingDisplayFlag) {
			t.Fatalf("unsupported binary got %s: %q", thinkingDisplayFlag, argv)
		}
	}
	if got := probeCount(t, probes); got != 1 {
		t.Fatalf("probes = %d, want 1", got)
	}
	if got := strings.Count(logs.String(), "does not accept "+thinkingDisplayFlag); got != 1 {
		t.Fatalf("WARN lines = %d, want 1:\n%s", got, logs.String())
	}
}

func TestThinkingLogsLeaveTheConversationUnchanged(t *testing.T) {
	runner, request := recordedClaudeConversation(t, "normal")
	runner.thinkingLogs = config.ThinkingLogsFull
	runner.logger = discardLogger()
	if _, err := runner.RunConversation(t.Context(), request); err != nil {
		t.Fatalf("RunConversation: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(runner.claudeConfigDir, "launch.args"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), thinkingDisplayFlag) {
		t.Fatalf("conversation launch carries %s:\n%s", thinkingDisplayFlag, data)
	}
}
