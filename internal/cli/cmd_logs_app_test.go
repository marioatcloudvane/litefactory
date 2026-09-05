package cli

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

// runBareLogs dispatches args against the real command surface with LF_HOME
// pointed at home, exactly as TestLogsWithNoDatabaseIsAPrecondition does for
// the issue path. No store.Open, no migration: proving AC-1.2 (no prior
// `litefactory init` required) means never touching the database here.
func runBareLogs(t *testing.T, home string, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	var out, errOut bytes.Buffer
	env := &Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
	code = NewRouter("litefactory", env, Commands()).Run(t.Context(), args)
	return code, out.String(), errOut.String()
}

// TestLogsBareShowsTheAppLog is AC-1.1 and AC-1.2: `litefactory logs` with no
// issue prints litefactory's own log and exits 0, without ever requiring a
// database or a prior `litefactory init` — home here has neither.
func TestLogsBareShowsTheAppLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const line = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(line), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	// --raw sidesteps slogTextSink's rendering so this test asserts the app
	// log's content reached stdout, not the exact re-rendered text.
	code, stdout, stderr := runBareLogs(t, home, "logs", "--raw")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stdout != line {
		t.Errorf("stdout = %q, want %q", stdout, line)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
}

// TestLogsBareWithNoAppLogYetPrintsAndExitsZero is AC-1.5: a daemon that has
// never run yet is a state to report, not a failure.
func TestLogsBareWithNoAppLogYetPrintsAndExitsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, stdout, stderr := runBareLogs(t, home, "logs")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	wantPath := filepath.Join(home, "logs", "daemon.log")
	if !strings.Contains(stdout, wantPath) {
		t.Errorf("stdout = %q, want it to name %q", stdout, wantPath)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
}

// TestLogsDaemonArgIsRejectedWithAHelpfulMessage is AC-1.3: "logs daemon"
// fails to parse as an issue number, and the resulting usage error names both
// the problem and the bare-invocation alternative the reader probably meant.
func TestLogsDaemonArgIsRejectedWithAHelpfulMessage(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, _, stderr := runBareLogs(t, home, "logs", "daemon")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, `"daemon"`) {
		t.Errorf("stderr = %q, want it to quote the offending argument", stderr)
	}
	if !strings.Contains(stderr, "litefactory logs with no issue shows litefactory's own log") {
		t.Errorf("stderr = %q, want it to mention the bare-invocation alternative", stderr)
	}
}

// TestLogsIssueOnlyFlagsAreRejectedWithNoIssue is AC-1.4: a flag that only
// makes sense against a specific issue's run log is rejected, by name, when
// no issue is given.
func TestLogsIssueOnlyFlagsAreRejectedWithNoIssue(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	tests := []struct {
		args     []string
		wantFlag string
	}{
		{args: []string{"logs", "--run", "2"}, wantFlag: "--run"},
		{args: []string{"logs", "--runs"}, wantFlag: "--runs"},
		{args: []string{"logs", "--only", "tool"}, wantFlag: "--only"},
		{args: []string{"logs", "--full"}, wantFlag: "--full"},
		{args: []string{"logs", "--repo", "acme/api"}, wantFlag: "--repo"},
	}

	for _, tc := range tests {
		t.Run(tc.wantFlag, func(t *testing.T) {
			code, _, stderr := runBareLogs(t, home, tc.args...)
			if code != ExitUsage {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
			}
			if !strings.Contains(stderr, tc.wantFlag) {
				t.Errorf("stderr = %q, does not name the offending flag %q", stderr, tc.wantFlag)
			}
			if !strings.Contains(stderr, "only applies to an issue's log") {
				t.Errorf("stderr = %q, want it to explain why the flag was rejected", stderr)
			}
		})
	}
}

// TestLogsGrepAndInvertApplyWithNoIssue is US-7: --grep, -i and --invert were
// deliberately removed from issueOnlyFlags because they filter the app log's
// output exactly as they filter an issue's run log. This confirms they are
// both accepted and functional on the bare (no-issue) path, not merely
// unrejected.
func TestLogsGrepAndInvertApplyWithNoIssue(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const (
		startedLine = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
		errorLine   = `{"time":"2026-09-05T09:00:01Z","level":"ERROR","msg":"tick failed"}` + "\n"
		stoppedLine = `{"time":"2026-09-05T09:00:02Z","level":"INFO","msg":"daemon stopped"}` + "\n"
	)
	all := startedLine + errorLine + stoppedLine
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(all), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "grep matches",
			args: []string{"logs", "--raw", "--grep", "tick failed"},
			want: errorLine,
		},
		{
			name: "grep is case-insensitive with -i",
			args: []string{"logs", "--raw", "--grep", "TICK FAILED", "-i"},
			want: errorLine,
		},
		{
			name: "invert keeps everything but the match",
			args: []string{"logs", "--raw", "--grep", "tick failed", "--invert"},
			want: startedLine + stoppedLine,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runBareLogs(t, home, tc.args...)
			if code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
			}
			if stdout != tc.want {
				t.Errorf("stdout = %q, want %q", stdout, tc.want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want it empty", stderr)
			}
		})
	}
}

// TestLogsRawAndFollowStillApplyWithNoIssue confirms --raw and -f are not
// mistakenly treated as issue-only: they apply to the app log view too
// (T-1.4's doc comment on issueOnlyFlags), so passing --raw alone must not be
// rejected.
func TestLogsRawAndFollowStillApplyWithNoIssue(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, _, stderr := runBareLogs(t, home, "logs", "--raw")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
}
