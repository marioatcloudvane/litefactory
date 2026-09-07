package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// writeMixedLevelDaemonLog writes a daemon.log fixture with one record at
// each of debug/info/warn/error, plus one record whose "level" field is
// missing and one whose "level" field is a value logging.ParseLevel does not
// recognize. It returns the four well-formed lines individually so tests can
// assert on exactly which lines survive a --level filter, and the two
// edge-case lines together as "unrecognized" per AC-5.3.
func writeMixedLevelDaemonLog(t *testing.T, home string) (debugLine, infoLine, warnLine, errorLine, unrecognized string) {
	t.Helper()

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}

	debugLine = `{"time":"2026-09-05T09:00:00Z","level":"DEBUG","msg":"tick"}` + "\n"
	infoLine = `{"time":"2026-09-05T09:00:01Z","level":"INFO","msg":"daemon started"}` + "\n"
	warnLine = `{"time":"2026-09-05T09:00:02Z","level":"WARN","msg":"slow poll"}` + "\n"
	errorLine = `{"time":"2026-09-05T09:00:03Z","level":"ERROR","msg":"tick failed"}` + "\n"
	missingLevel := `{"time":"2026-09-05T09:00:04Z","msg":"no level field"}` + "\n"
	badLevel := `{"time":"2026-09-05T09:00:05Z","level":"LOUD","msg":"unrecognized level"}` + "\n"
	unrecognized = missingLevel + badLevel

	all := debugLine + infoLine + warnLine + errorLine + missingLevel + badLevel
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(all), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}
	return debugLine, infoLine, warnLine, errorLine, unrecognized
}

// TestLogsLevelFiltersTheAppLog is AC-5.1 and AC-5.3: --level narrows the app
// log to records at or above the given threshold, while a record whose level
// is absent or unrecognized is always shown regardless of the threshold.
func TestLogsLevelFiltersTheAppLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	debugLine, infoLine, warnLine, errorLine, unrecognized := writeMixedLevelDaemonLog(t, home)

	tests := []struct {
		name  string
		level string
		want  string
	}{
		{
			name:  "warn/shows warn and error only, plus unrecognized",
			level: "warn",
			want:  warnLine + errorLine + unrecognized,
		},
		{
			name:  "error/shows error only, plus unrecognized",
			level: "error",
			want:  errorLine + unrecognized,
		},
		{
			name:  "debug/shows everything",
			level: "debug",
			want:  debugLine + infoLine + warnLine + errorLine + unrecognized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runBareLogs(t, home, "logs", "--raw", "--level", tc.level)
			if code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
			}
			if stdout != tc.want {
				t.Errorf("stdout = %q, want %q", stdout, tc.want)
			}
		})
	}
}

// TestLogsLevelIsCaseInsensitive is AC-5.2's accepted half: --level DEBUG
// (any case) is accepted exactly like --level debug.
func TestLogsLevelIsCaseInsensitive(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	debugLine, infoLine, warnLine, errorLine, unrecognized := writeMixedLevelDaemonLog(t, home)
	want := debugLine + infoLine + warnLine + errorLine + unrecognized

	code, stdout, stderr := runBareLogs(t, home, "logs", "--raw", "--level", "DEBUG")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestLogsLevelRejectsAnUnrecognizedValue is AC-5.2's rejected half: a value
// outside debug/info/warn/error is a usage error that names the vocabulary.
func TestLogsLevelRejectsAnUnrecognizedValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, _, stderr := runBareLogs(t, home, "logs", "--level", "loud")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "debug, info, warn, error") {
		t.Errorf("stderr = %q, want it to name the accepted vocabulary", stderr)
	}
}

// TestLogsLevelWithIssueIsRejected is AC-5.4: --level only applies to the app
// log view, so passing it alongside an issue positional is a usage error
// naming --level, mirroring how -n and --all are rejected on the issue path.
func TestLogsLevelWithIssueIsRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, _, stderr := runBareLogs(t, home, "logs", "123", "--level", "warn")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "--level") {
		t.Errorf("stderr = %q, want it to name --level", stderr)
	}
	if !strings.Contains(stderr, "only applies to the app log view") {
		t.Errorf("stderr = %q, want it to explain why --level was rejected", stderr)
	}
}

// TestLogsGrepOnAppLogRejectsABadPattern is AC-7.2: an invalid --grep pattern
// on the app-log (no-issue) path is a usage error naming --grep, exactly as
// it is on the issue-log path (TestLogsGrepRejectsABadPattern).
func TestLogsGrepOnAppLogRejectsABadPattern(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	code, _, stderr := runBareLogs(t, home, "logs", "--grep", "(unclosed")
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "--grep") {
		t.Errorf("stderr = %q, want it to name --grep", stderr)
	}
}

// TestLogsLevelAndGrepCompose is AC-7.1's composition-ordering nuance: level
// filtering is applied first (outer), grep second (inner) — a low-level
// record that would otherwise match --grep is still hidden by --level, while
// a record that survives --level but does not match --grep is hidden by
// --grep, and a record surviving both is shown.
func TestLogsLevelAndGrepCompose(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const (
		debugTick = `{"time":"2026-09-05T09:00:00Z","level":"DEBUG","msg":"tick failed"}` + "\n"
		warnOther = `{"time":"2026-09-05T09:00:01Z","level":"WARN","msg":"slow poll"}` + "\n"
		errorTick = `{"time":"2026-09-05T09:00:02Z","level":"ERROR","msg":"tick failed"}` + "\n"
	)
	all := debugTick + warnOther + errorTick
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(all), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	// debugTick matches --grep but is below --level warn, so it must not
	// appear; warnOther survives --level but does not match --grep, so it
	// must not appear either; only errorTick survives both.
	code, stdout, stderr := runBareLogs(t, home, "logs", "--raw", "--level", "warn", "--grep", "tick failed")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stdout != errorTick {
		t.Errorf("stdout = %q, want %q", stdout, errorTick)
	}
}

// TestLogsLevelAdvisoryFiresWhenNothingCaptured is AC-8.1: when --level asks
// for records that were never captured, exactly one advisory line naming the
// configured level and `daemon start --level` is written to stderr, stdout
// carries only the (empty) log output, and the exit code is unaffected.
func TestLogsLevelAdvisoryFiresWhenNothingCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const infoLine = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(infoLine), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	// The fixture holds only INFO records; asking for --level error (a
	// higher threshold) filters all of them out, so no error-or-above record
	// was ever captured and the advisory must fire.
	code, stdout, stderr := runBareLogs(t, home, "logs", "--raw", "--level", "error")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want it empty since no error (or higher) records exist", stdout)
	}
	if !strings.Contains(stderr, "error") {
		t.Errorf("stderr = %q, want it to name the configured level %q", stderr, "error")
	}
	if !strings.Contains(stderr, "daemon start --level") {
		t.Errorf("stderr = %q, want it to mention `daemon start --level`", stderr)
	}
	if got := strings.Count(stderr, "no error (or higher) records were captured"); got != 1 {
		t.Errorf("stderr contains the advisory %d times, want exactly 1:\n%s", got, stderr)
	}
}

// TestLogsLevelAdvisoryDoesNotFireWhenCaptured is AC-8.1's negative case: when
// at least one record at or above the threshold is present, no advisory is
// printed at all.
func TestLogsLevelAdvisoryDoesNotFireWhenCaptured(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const debugLine = `{"time":"2026-09-05T09:00:00Z","level":"DEBUG","msg":"tick"}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(debugLine), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	code, stdout, stderr := runBareLogs(t, home, "logs", "--raw", "--level", "debug")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if stdout != debugLine {
		t.Errorf("stdout = %q, want %q", stdout, debugLine)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty since a debug record was captured", stderr)
	}
}

// TestLogsLevelAdvisoryDoesNotRepeatUnderFollow is AC-8.2: the advisory fires
// at most once, even when -f keeps the process running across multiple poll
// ticks — it is checked once, after the initial tail pass and strictly before
// the follow loop begins, never inside the loop itself.
func TestLogsLevelAdvisoryDoesNotRepeatUnderFollow(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	logsDir := filepath.Join(home, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}
	const infoLine = `{"time":"2026-09-05T09:00:00Z","level":"INFO","msg":"daemon started"}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "daemon.log"), []byte(infoLine), 0o600); err != nil {
		t.Fatalf("write daemon.log: %v", err)
	}

	// followInterval is 250ms; a deadline of several ticks gives the follow
	// loop multiple chances to (wrongly) repeat the advisory before ctx is
	// cancelled and the loop returns.
	ctx, cancel := context.WithTimeout(t.Context(), 1200*time.Millisecond)
	defer cancel()

	var out, errOut bytes.Buffer
	env := &Env{Stdout: &out, Stderr: &errOut, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
	code := NewRouter("litefactory", env, Commands()).Run(ctx, []string{"logs", "--raw", "--level", "error", "-f"})

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, errOut.String())
	}
	if got := strings.Count(errOut.String(), "no error (or higher) records were captured"); got != 1 {
		t.Errorf("stderr contains the advisory %d times across the follow session, want exactly 1:\n%s", got, errOut.String())
	}
}
