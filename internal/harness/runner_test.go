package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
)

// The fake claude is a shell script on a test-controlled PATH, per the plan's
// testing rules: it exercises the real resolution path, the real pipes, the
// real process group and the real kill, with none of the cost or the
// non-determinism of the actual binary.

var testRef = naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

// testSessionID is what naming.ClaudeSessionID derives for testRef's first
// segment, written out rather than recomputed: this package no longer derives
// the id, so a test that called the derivation to build its expectation would
// be comparing the derivation against itself instead of against what the
// request carried.
const testSessionID = "d3ece914-438d-584f-a110-fa6b77f31645"

// storedSessionID is a canonical UUID that is *not* what testRef derives — the
// shape of a segments row written before some future change to the derivation,
// or by a different litefactory version. It is the id claude has seen, so it is
// the id that has to reach argv.
const storedSessionID = "6f2a1c3d-9e4b-5a7c-8d1e-2f3a4b5c6d7e"

const testPrompt = "# litefactory run 0001\n\nplease add a /health endpoint\n"

// fixture is one prepared runner: an $LF_HOME, a worktree, and a fake claude on
// PATH.
type fixture struct {
	home     string
	worktree string
	binDir   string
	runner   *Runner
}

func newFixture(t *testing.T, script string, adjust ...func(*config.Claude)) *fixture {
	t.Helper()

	root := t.TempDir()
	f := &fixture{
		home:     filepath.Join(root, "lfhome"),
		worktree: filepath.Join(root, "worktree"),
		binDir:   filepath.Join(root, "bin"),
	}
	for _, dir := range []string{f.home, f.worktree, f.binDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	writeScript(t, filepath.Join(f.binDir, "claude"), script)

	claude := config.Claude{
		Binary:         "claude",
		Tools:          []string{"Read", "Edit"},
		AllowedTools:   []string{"Read", "Edit(./**)"},
		SettingsPath:   filepath.Join(f.home, "claude-settings.json"),
		TimeoutSeconds: 30,
	}
	for _, fn := range adjust {
		fn(&claude)
	}

	runner, err := New(Options{
		Paths:   config.NewPaths(f.home),
		Claude:  claude,
		Environ: fixtureEnviron(f.binDir, f.home, root),
		Logger:  discardLogger(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.runner = runner
	return f
}

// fixtureEnviron is a daemon environment that carries every credential shape
// the boundary has to stop, so that every runner test asserts AC-3.13 whether
// it means to or not.
func fixtureEnviron(binDir, home, tmp string) []string {
	return []string{
		"PATH=" + binDir + ":/usr/bin:/bin",
		"HOME=" + home,
		"USER=lf-test-user",
		"SHELL=/bin/sh",
		"LANG=en_US.UTF-8",
		"TERM=dumb",
		"TMPDIR=" + tmp,
		"GITHUB_TOKEN=ghp_leakcanary000000000000",
		"GH_TOKEN=gho_leakcanary000000000000",
		"LF_HOME=" + home,
		"LF_BOT_TOKEN=ghs_leakcanary000000000000",
		"AWS_SECRET_ACCESS_KEY=leakcanary-aws",
		"DATABASE_PASSWORD=leakcanary-db",
		"SOME_CREDENTIAL=leakcanary-cred",
		"ANTHROPIC_API_KEY=leakcanary-anthropic",
		"CLAUDE_CONFIG_DIR=" + home + "/claude-config",
		"SSH_AUTH_SOCK=" + tmp + "/agent.sock",
	}
}

func (f *fixture) request() Request {
	return Request{
		Ref:            testRef,
		RunOrdinal:     1,
		SegmentOrdinal: 1,
		Session:        SessionRef{Ref: testSessionID},
		Mode:           "spike",
		WorktreePath:   f.worktree,
		Prompt:         testPrompt,
	}
}

func (f *fixture) run(t *testing.T) *RunResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()

	result, err := f.runner.Run(ctx, f.request())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

// dump reads a file the fake claude wrote into the worktree.
func (f *fixture) dump(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.worktree, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func (f *fixture) runLog(t *testing.T) string {
	t.Helper()
	path, err := naming.RunLogPath(f.home, testRef, 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	return string(data)
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runningMarker is the file a fake claude touches in the worktree to say it is
// running, so a test can synchronise on the child existing instead of guessing
// how long an exec takes.
const runningMarker = "running.marker"

// emit renders a shell line that prints one JSON event. Single quotes would
// break the quoting, and every event this package cares about is single-quote
// free, so a stray one is a broken fixture rather than a clever escape.
func emit(t *testing.T, line string) string {
	t.Helper()
	if strings.Contains(line, "'") {
		t.Fatalf("fixture event contains a single quote: %s", line)
	}
	return "printf '%s\\n' '" + line + "'\n"
}

// dumpPrologue makes the fake record everything the boundary tests need: its
// own environment, its own argv, its own ps line, and its stdin.
const dumpPrologue = `/usr/bin/env > env.dump
printf '%s\n' "$@" > argv.dump
/bin/ps -o args= -p $$ > ps.dump 2>/dev/null || true
/bin/cat > stdin.dump
`

func successScript(t *testing.T) string {
	t.Helper()
	return dumpPrologue + emit(t, initLine) + emit(t, resultLine) + "exit 0\n"
}

func TestRunSucceeds(t *testing.T) {
	f := newFixture(t, successScript(t))
	result := f.run(t)

	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d", result.ExitCode)
	}
	if result.Reason != "" {
		t.Errorf("a successful run carries a failure reason: %q", result.Reason)
	}
	if !result.Succeeded() {
		t.Error("Succeeded() disagrees with Outcome")
	}
	// AC-3.17: model and tool surface from init, result text and cost from the
	// terminal result event.
	if result.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", result.Model)
	}
	if !slices.Equal(result.Tools, []string{"Read", "Edit"}) {
		t.Errorf("tools = %q", result.Tools)
	}
	if result.ResultText != "DONE" {
		t.Errorf("result text = %q", result.ResultText)
	}
	if !result.HasResult || result.CostUSD != 0.04622315 {
		t.Errorf("cost = %v (has result: %v)", result.CostUSD, result.HasResult)
	}
	if result.Duration <= 0 || result.FinishedAt.Before(result.StartedAt) {
		t.Errorf("timings are wrong: %v .. %v (%v)", result.StartedAt, result.FinishedAt, result.Duration)
	}
	if result.StreamError != nil {
		t.Errorf("stream error: %v", result.StreamError)
	}

	// The result reports the id the request carried, which is the id on the
	// segments row, so the row and the conversation claude keeps cannot drift.
	if result.SessionID != testSessionID {
		t.Errorf("session id = %q, want %q", result.SessionID, testSessionID)
	}

	if log := f.runLog(t); strings.Count(log, "\n") != 2 {
		t.Errorf("run log should hold two events:\n%s", log)
	}
}

// SD-7's verified invocation, plus the two hardening flags. --verbose in
// particular: without it claude hard-errors and writes nothing to stdout, which
// is indistinguishable from a hang.
func TestRunUsesTheVerifiedInvocation(t *testing.T) {
	f := newFixture(t, successScript(t))
	result := f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")

	for _, pair := range [][2]string{
		{"-p", ""},
		{"--output-format", "stream-json"},
		{"--verbose", ""},
		{"--session-id", testSessionID},
		{"--tools", "Read,Edit"},
		{"--settings", filepath.Join(f.home, "claude-settings.json")},
		{"--allowedTools", "Read,Edit(./**)"},
		{"--strict-mcp-config", ""},
	} {
		i := slices.Index(argv, pair[0])
		if i < 0 {
			t.Errorf("argv is missing %s: %q", pair[0], argv)
			continue
		}
		if pair[1] == "" {
			continue
		}
		if i+1 >= len(argv) || argv[i+1] != pair[1] {
			t.Errorf("%s value = %q, want %q", pair[0], argv[i+1:], pair[1])
		}
	}

	// The grep-able invariant, asserted at runtime too. The needle is assembled
	// from fragments so that this file is not itself a match for the
	// repository-wide search that enforces the invariant.
	if slices.Contains(argv, "--dangerously-"+"skip-permissions") {
		t.Fatal("the permission-skipping flag was passed to claude")
	}

	if got := result.Command[0]; got != filepath.Join(f.binDir, "claude") {
		t.Errorf("binary = %q, want the absolute path resolved from config", got)
	}
	if strings.Contains(strings.Join(result.Command, " "), "health endpoint") {
		t.Error("the prompt is in the recorded command line")
	}
}

// AC-SR-1.2, the harness half of it. The id claude is given is the one the
// request carried and nothing else, so a run whose stored session id has parted
// company with the derivation still names the conversation claude actually has.
//
// storedSessionID is deliberately not what testRef derives: reinstating a
// derivation inside Run would put the derived id on argv and fail this.
func TestRunPutsTheRequestSessionIDOnArgv(t *testing.T) {
	f := newFixture(t, successScript(t))

	req := f.request()
	req.Session = SessionRef{Ref: storedSessionID}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	result, err := f.runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--session-id")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv is missing --session-id: %q", argv)
	}
	if argv[i+1] != storedSessionID {
		t.Errorf("--session-id = %q, want the request's %q", argv[i+1], storedSessionID)
	}
	if slices.Contains(argv, testSessionID) {
		t.Errorf("argv carries the derived id %q, so the runner derived it again", testSessionID)
	}
	if result.SessionID != storedSessionID {
		t.Errorf("result session id = %q, want %q", result.SessionID, storedSessionID)
	}
}

// AC-SR-1.3. The id arrives on the request instead of being derived here, so
// its shape is no longer guaranteed by construction and validate has to check
// it. Canonical means what naming.ClaudeSessionID emits: lowercase, hyphenated,
// version 5. claude rejects anything else itself, but only after the process
// has started, which reads as a run failure rather than a bad request.
func TestRunRejectsAMalformedSessionID(t *testing.T) {
	f := newFixture(t, successScript(t))

	tests := []struct {
		name      string
		sessionID string
		wantValid bool
	}{
		{"the derived id", testSessionID, true},
		{"a stored id from some other derivation", storedSessionID, true},
		{"empty", "", false},
		{"not a uuid at all", "seeded-session-id", false},
		{"one character short", testSessionID[:len(testSessionID)-1], false},
		{"uppercase hex", strings.ToUpper(testSessionID), false},
		{"unhyphenated", strings.ReplaceAll(testSessionID, "-", ""), false},
		{"version 4 rather than 5", "d3ece914-438d-484f-a110-fa6b77f31645", false},
		{"surrounded by whitespace", " " + testSessionID + "\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := f.request()
			req.Session = SessionRef{Ref: tc.sessionID}

			_, err := f.runner.Run(t.Context(), req)
			switch {
			case tc.wantValid && err != nil:
				t.Fatalf("Run rejected %s: %v", tc.name, err)
			case !tc.wantValid && err == nil:
				t.Fatalf("Run accepted %s", tc.name)
			case !tc.wantValid && !errors.Is(err, ErrInvalidRequest):
				t.Fatalf("error %v does not unwrap to ErrInvalidRequest", err)
			}
		})
	}
}

// AC-3.13. The fake dumps its own environment; nothing secret-shaped may be in
// it. This is the security boundary of the product (SD-4): redaction is
// hygiene, and an adversarial agent can base64 a secret past any matcher. The
// boundary is not giving the subprocess the secret.
func TestRunChildEnvironmentCarriesNoSecrets(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)

	dump := f.dump(t, "env.dump")
	for _, line := range strings.Split(dump, "\n") {
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if IsSecretEnvName(name) {
			t.Errorf("secret-shaped variable %q reached claude", name)
		}
		if strings.HasPrefix(name, "LF_") {
			t.Errorf("litefactory variable %q reached claude", name)
		}
		switch name {
		case "GITHUB_TOKEN", "GH_TOKEN", "SSH_AUTH_SOCK", "CLAUDE_CONFIG_DIR":
			t.Errorf("%s reached claude", name)
		}
	}
	if strings.Contains(dump, "leakcanary") {
		t.Error("a credential value appears in the child environment")
	}
	// USER is required: without it claude's Keychain auth fails "Not logged in".
	if !strings.Contains(dump, "USER=lf-test-user") {
		t.Errorf("USER is missing from the child environment:\n%s", dump)
	}
}

// AC-3.12. On stdin, so it is not subject to ARG_MAX and does not appear in ps.
func TestRunDeliversThePromptOnStdinAndNotInArgv(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)

	if got := f.dump(t, "stdin.dump"); got != testPrompt {
		t.Errorf("stdin = %q, want %q", got, testPrompt)
	}
	if got := f.dump(t, "argv.dump"); strings.Contains(got, "health endpoint") {
		t.Errorf("the prompt is in argv:\n%s", got)
	}
	if got := f.dump(t, "ps.dump"); strings.Contains(got, "health endpoint") {
		t.Errorf("the prompt is visible in ps output:\n%s", got)
	}
}

// An issue body large enough to blow the ~1 MB macOS ARG_MAX would fail exec
// with E2BIG if the prompt went in argv.
func TestRunAcceptsAnOversizedPrompt(t *testing.T) {
	f := newFixture(t, successScript(t))
	req := f.request()
	req.Prompt = strings.Repeat("x", 4<<20)

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	result, err := f.runner.Run(ctx, req)
	if err != nil {
		t.Fatalf("a 4 MB prompt failed to run: %v", err)
	}
	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if got := len(f.dump(t, "stdin.dump")); got != 4<<20 {
		t.Errorf("claude received %d bytes of prompt, want %d", got, 4<<20)
	}
}

// AC-3.16 / SD-6. A single event line over 64 KB must be written intact and
// must not cost the run its terminal result event. Real lines of 72,411 bytes
// were measured in the spike.
func TestRunWritesAnOversizedEventLineIntact(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "huge.jsonl")
	huge := `{"type":"user","content":"` + strings.Repeat("x", 200_000) + `"}`
	if err := os.WriteFile(payload, []byte(huge+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := emit(t, initLine) + "/bin/cat " + payload + "\n" + emit(t, resultLine) + "exit 0\n"
	f := newFixture(t, script)
	result := f.run(t)

	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if !result.HasResult {
		t.Fatal("the terminal result event was lost behind the over-long line")
	}
	if result.LongestLine <= 64<<10 {
		t.Fatalf("longest line = %d bytes; the fixture is not exercising the limit", result.LongestLine)
	}
	if result.Events != 3 {
		t.Errorf("events = %d, want 3", result.Events)
	}
	if log := f.runLog(t); !strings.Contains(log, huge) {
		t.Error("the over-long line was not written to NNNN.jsonl intact")
	}
}

// AC-3.16: written as it arrives, not buffered — a crash keeps what already
// happened. The fake blocks after the init event until the test has seen it on
// disk.
func TestRunStreamsEventsAsTheyArrive(t *testing.T) {
	root := t.TempDir()
	gate := filepath.Join(root, "gate")

	script := emit(t, initLine) +
		"i=0\nwhile [ ! -f " + gate + " ] && [ $i -lt 200 ]; do /bin/sleep 0.05; i=$((i+1)); done\n" +
		emit(t, resultLine) + "exit 0\n"
	f := newFixture(t, script)

	logPath, err := naming.RunLogPath(f.home, testRef, 1)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan *RunResult, 1)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	go func() {
		result, err := f.runner.Run(ctx, f.request())
		if err != nil {
			t.Errorf("Run: %v", err)
			done <- nil
			return
		}
		done <- result
	}()

	if !waitFor(5*time.Second, func() bool {
		data, err := os.ReadFile(logPath)
		return err == nil && strings.Contains(string(data), `"subtype":"init"`)
	}) {
		cancel()
		<-done
		t.Fatal("the init event was not on disk while the run was still in flight; the stream is buffered")
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	result := <-done
	if result == nil {
		t.FailNow()
	}
	if result.Outcome != OutcomeSucceeded {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
}

// AC-3.20 / S-9: a non-zero exit is captured and surfaced, never swallowed.
func TestRunNonZeroExit(t *testing.T) {
	script := emit(t, initLine) + "echo 'boom: something went wrong' >&2\nexit 3\n"
	f := newFixture(t, script)
	result := f.run(t)

	if result.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s", result.Outcome)
	}
	if result.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", result.ExitCode)
	}
	if !strings.Contains(result.Reason, "status 3") {
		t.Errorf("reason = %q", result.Reason)
	}
	if !strings.Contains(result.Stderr, "boom") {
		t.Errorf("stderr tail = %q; S-9 quotes it in the failure comment", result.Stderr)
	}
	// Partial JSONL is retained exactly as captured, never truncated on failure.
	if !strings.Contains(f.runLog(t), `"subtype":"init"`) {
		t.Error("the partial run log was not retained")
	}
}

// SD-3, third outcome: no terminal result event at all. A stale credential
// makes claude retry internally and emit nothing; here the fake simply exits
// zero without one. It must not be reported as a success.
func TestRunNoResultEvent(t *testing.T) {
	f := newFixture(t, emit(t, initLine)+"exit 0\n")
	result := f.run(t)

	if result.Outcome != OutcomeNoResult {
		t.Fatalf("outcome = %s, want no_result", result.Outcome)
	}
	if result.Succeeded() {
		t.Fatal("a run with no terminal event was reported as a success")
	}
	if result.HasResult {
		t.Error("HasResult is true without a result event")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want the real 0 rather than a synthesised one", result.ExitCode)
	}
	if !strings.Contains(result.Reason, "terminal result event") {
		t.Errorf("reason = %q", result.Reason)
	}
}

// SD-3, first half: .subtype reads "success" even on is_error runs. Branching
// on it reports a failed run as successful.
func TestRunFailsOnIsErrorDespiteSuccessSubtype(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in. Please run /login",` +
		`"total_cost_usd":0,"num_turns":0}`
	f := newFixture(t, emit(t, initLine)+emit(t, line)+"exit 0\n")
	result := f.run(t)

	if result.Outcome != OutcomeFailed {
		t.Fatalf("outcome = %s; .subtype was trusted over .is_error", result.Outcome)
	}
	if !result.IsError {
		t.Error("IsError was not recorded")
	}
	if !strings.Contains(result.Reason, "Not logged in") {
		t.Errorf("reason = %q; it should quote what claude said", result.Reason)
	}
}

// AC-3.19 / SD-1. Setpgid plus kill(-pgid) is not enough: claude's Bash tool
// setsids its shell into a new process group and the grandchild survives,
// reparented to PID 1, still holding the worktree open. The fixture reproduces
// that with `set -m`, which puts the background job in its own process group,
// and then asserts the sleeper is gone.
func TestRunTimeoutKillsTheWholeSubtree(t *testing.T) {
	root := t.TempDir()
	sleeperPID := filepath.Join(root, "sleeper.pid")
	sleeperPGID := filepath.Join(root, "sleeper.pgid")
	shellPGID := filepath.Join(root, "shell.pgid")
	child := filepath.Join(root, "child.sh")

	writeScript(t, child, "set -m\n"+
		"/bin/sleep 120 &\n"+
		"sleeper=$!\n"+
		"printf '%s' \"$sleeper\" > "+sleeperPID+"\n"+
		"/bin/ps -o pgid= -p \"$sleeper\" | /usr/bin/tr -d ' ' > "+sleeperPGID+"\n"+
		"/bin/ps -o pgid= -p $$ | /usr/bin/tr -d ' ' > "+shellPGID+"\n"+
		"wait\n")

	script := emit(t, initLine) + "/bin/sh " + child + " &\n/bin/sleep 120\n"
	f := newFixture(t, script, func(c *config.Claude) { c.TimeoutSeconds = 1 })

	started := time.Now()
	result := f.run(t)

	if result.Outcome != OutcomeTimedOut {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if result.ExitCode != SignalExitCode {
		t.Errorf("exit code = %d, want %d for a killed run (S-9)", result.ExitCode, SignalExitCode)
	}
	if !strings.Contains(result.Reason, "timed out") {
		t.Errorf("reason = %q", result.Reason)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("the run took %v; the kill or the drain is not bounded", elapsed)
	}

	// Fixture sanity: if the grandchild had not escaped into its own process
	// group, this test would be asserting the easy case.
	sleeperGroup := readTrimmed(t, sleeperPGID)
	shellGroup := readTrimmed(t, shellPGID)
	if sleeperGroup == "" || sleeperGroup == shellGroup {
		t.Fatalf("fixture did not reproduce SD-1: sleeper pgid %q, parent shell pgid %q", sleeperGroup, shellGroup)
	}

	pid, err := strconv.Atoi(readTrimmed(t, sleeperPID))
	if err != nil {
		t.Fatalf("sleeper pid: %v", err)
	}
	if !waitFor(10*time.Second, func() bool { return !processAlive(pid) }) {
		t.Fatalf("grandchild %d survived the timeout kill", pid)
	}
}

// D-B: SIGTERM cancels the run context and the daemon exits promptly. That is
// an abandonment, not a timeout — nothing is wrong with the task.
func TestRunAbandonedWhenTheContextIsCancelled(t *testing.T) {
	// The fake announces itself by creating runningMarker in the worktree before
	// it blocks, so the cancellation can wait for the child to exist rather than
	// assume it has been exec'd within a fixed sleep. A context cancelled before
	// the process starts is a start failure, not the abandonment under test.
	f := newFixture(t, emit(t, initLine)+"printf 'x' > "+runningMarker+"\n/bin/sleep 120\n")
	marker := filepath.Join(f.worktree, runningMarker)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	defer cancel()

	cancelledAt := make(chan time.Time, 1)
	go func() {
		// Cancel even if the marker never appears: the assertions below then
		// report the real failure instead of hanging until the run's timeout.
		waitFor(10*time.Second, func() bool {
			_, err := os.Stat(marker)
			return err == nil
		})
		cancelledAt <- time.Now()
		cancel()
	}()

	result, err := f.runner.Run(ctx, f.request())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Outcome != OutcomeAbandoned {
		t.Fatalf("outcome = %s (%s)", result.Outcome, result.Reason)
	}
	if result.ExitCode != SignalExitCode {
		t.Errorf("exit code = %d, want %d", result.ExitCode, SignalExitCode)
	}
	if elapsed := time.Since(<-cancelledAt); elapsed > 20*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
}

// A run that finished cleanly a moment before the daemon was signalled is a
// success, not an abandonment: the evidence is a zero exit and a terminal event.
func TestClassifySuccessWinsOverALateCancellation(t *testing.T) {
	outcome, reason, code := classify(classification{
		abandoned: true,
		exitCode:  0,
		result:    &resultEvent{Result: "DONE"},
	})
	if outcome != OutcomeSucceeded || reason != "" || code != 0 {
		t.Fatalf("classify = %s %q %d, want a success", outcome, reason, code)
	}
}

func TestClassifyDistinguishesEveryFailure(t *testing.T) {
	tests := []struct {
		name        string
		in          classification
		wantOutcome Outcome
		wantCode    int
		wantReason  string
	}{
		{
			name:        "abandoned",
			in:          classification{abandoned: true, exitCode: SignalExitCode},
			wantOutcome: OutcomeAbandoned, wantCode: SignalExitCode, wantReason: "abandoned",
		},
		{
			name:        "timed out",
			in:          classification{timedOut: true, timeout: 90 * time.Second, exitCode: SignalExitCode},
			wantOutcome: OutcomeTimedOut, wantCode: SignalExitCode, wantReason: "timed out after 1m30s",
		},
		{
			name:        "no result event",
			in:          classification{exitCode: 0},
			wantOutcome: OutcomeNoResult, wantCode: 0, wantReason: "terminal result event",
		},
		{
			name:        "is_error",
			in:          classification{exitCode: 0, result: &resultEvent{IsError: true, Result: "nope"}},
			wantOutcome: OutcomeFailed, wantCode: 0, wantReason: "is_error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outcome, reason, code := classify(tc.in)
			if outcome != tc.wantOutcome {
				t.Errorf("outcome = %s, want %s", outcome, tc.wantOutcome)
			}
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(reason, tc.wantReason) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.wantReason)
			}
		})
	}
}

// Every distinct outcome must render as a distinct, stable identifier: it is
// written into NNNN.meta.json and read back by a human debugging a failure.
func TestOutcomeStringsAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, o := range []Outcome{OutcomeSucceeded, OutcomeFailed, OutcomeTimedOut, OutcomeAbandoned, OutcomeNoResult} {
		s := o.String()
		if s == "" || s == "invalid" {
			t.Errorf("outcome %d renders as %q", int(o), s)
		}
		if seen[s] {
			t.Errorf("outcome string %q is used twice", s)
		}
		seen[s] = true
	}
}

// SD-2: without --settings the run inherits the operator's personal
// ~/.claude/settings.json, ambient allow-rules and all.
func TestRunEnsuresTheSettingsFile(t *testing.T) {
	f := newFixture(t, successScript(t))
	settings := filepath.Join(f.home, "claude-settings.json")

	f.run(t)

	info, err := os.Stat(settings)
	if err != nil {
		t.Fatalf("settings file was not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings mode = %o, want 600", perm)
	}
	var decoded map[string]any
	data, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}
}

func TestRunDoesNotOverwriteAnExistingSettingsFile(t *testing.T) {
	f := newFixture(t, successScript(t))
	settings := filepath.Join(f.home, "claude-settings.json")
	const custom = `{"permissions":{"deny":["Read(~/.ssh/**)"]}}`
	if err := os.WriteFile(settings, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	f.run(t)

	data, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf("an operator's settings file was rewritten by a run:\n%s", data)
	}
}

func TestRunCreatesTheRunDirectoryWithRestrictivePermissions(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)

	path, err := naming.RunLogPath(f.home, testRef, 1)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("run log mode = %o, want 600; it is unredacted (S-1)", perm)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("run directory mode = %o, want 700", perm)
	}
}

// AC-3.18: a missing or non-executable claude is caught before any turn — and
// therefore any worktree — exists, naming the binary and the PATH searched.
// exec.LookPath's error cannot say that, which is why config resolves it.
func TestNewRejectsAnUnresolvableBinary(t *testing.T) {
	home := t.TempDir()
	_, err := New(Options{
		Paths:   config.NewPaths(home),
		Claude:  config.Claude{Binary: "claude", Tools: []string{"Read"}, TimeoutSeconds: 30},
		Environ: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"},
		Logger:  discardLogger(),
	})
	if err == nil {
		t.Fatal("New accepted a claude that is not on PATH")
	}
	if !errors.Is(err, config.ErrExecutableNotFound) {
		t.Fatalf("error %v does not unwrap to ErrExecutableNotFound", err)
	}
	if !strings.Contains(err.Error(), "/usr/sbin") {
		t.Errorf("the error does not name the PATH it searched: %v", err)
	}
}

func TestNewRejectsUnusableOptions(t *testing.T) {
	home := t.TempDir()
	base := func() Options {
		return Options{
			Paths:   config.NewPaths(home),
			Claude:  config.Claude{Binary: "/bin/sh", Tools: []string{"Read"}, TimeoutSeconds: 30},
			Environ: []string{"PATH=/bin"},
			Logger:  discardLogger(),
		}
	}
	if _, err := New(base()); err != nil {
		t.Fatalf("the baseline options were rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Options)
	}{
		{"no logger", func(o *Options) { o.Logger = nil }},
		{"relative home", func(o *Options) { o.Paths = config.NewPaths("relative") }},
		{"no tool surface", func(o *Options) { o.Claude.Tools = nil }},
		{"zero timeout", func(o *Options) { o.Claude.TimeoutSeconds = 0 }},
		{"relative settings path", func(o *Options) { o.Claude.SettingsPath = "settings.json" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := base()
			tc.mutate(&opts)
			if _, err := New(opts); err == nil {
				t.Fatalf("New accepted %s", tc.name)
			} else if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("error %v does not unwrap to ErrInvalidOptions", err)
			}
		})
	}
}

func TestRunRejectsUnusableRequests(t *testing.T) {
	f := newFixture(t, successScript(t))

	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"invalid ref", func(r *Request) { r.Ref.Owner = "" }},
		{"zero run ordinal", func(r *Request) { r.RunOrdinal = 0 }},
		{"zero segment ordinal", func(r *Request) { r.SegmentOrdinal = 0 }},
		{"empty mode", func(r *Request) { r.Mode = "" }},
		{"relative worktree", func(r *Request) { r.WorktreePath = "worktree" }},
		{"missing worktree", func(r *Request) { r.WorktreePath = filepath.Join(f.home, "nope") }},
		{"empty prompt", func(r *Request) { r.Prompt = "  \n" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := f.request()
			tc.mutate(&req)
			if _, err := f.runner.Run(t.Context(), req); err == nil {
				t.Fatalf("Run accepted %s", tc.name)
			} else if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error %v does not unwrap to ErrInvalidRequest", err)
			}
		})
	}
}

func TestRunReportsAStartFailure(t *testing.T) {
	f := newFixture(t, successScript(t))
	// Remove the execute bit after resolution, so the failure happens at exec
	// time rather than at New time.
	if err := os.Chmod(filepath.Join(f.binDir, "claude"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := f.runner.Run(t.Context(), f.request())
	if err == nil {
		t.Fatalf("Run returned a result for a process that never started: %+v", result)
	}
	if !errors.Is(err, ErrStartFailed) {
		t.Fatalf("error %v does not unwrap to ErrStartFailed", err)
	}
}

func TestRunResultCloneIsDeep(t *testing.T) {
	original := &RunResult{Tools: []string{"Read"}, Command: []string{"claude", "-p"}}
	clone := original.Clone()
	clone.Tools[0] = "Bash"
	clone.Command[1] = "--tampered"

	if original.Tools[0] != "Read" || original.Command[1] != "-p" {
		t.Fatal("Clone shares its slices with the original")
	}
	if (*RunResult)(nil).Clone() != nil {
		t.Fatal("Clone of nil is not nil")
	}
}

// waitFor polls until cond is true or the budget runs out.
func waitFor(budget time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// processAlive reports whether pid still exists. Signal 0 performs the
// permission and existence checks without delivering anything.
func processAlive(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// runArgv runs one request on the given path and returns the argv the child
// actually received. The fake overwrites argv.dump on every run, so two calls
// on one fixture compare two invocations of the same runner.
func runArgv(t *testing.T, f *fixture, path RunPath) []string {
	t.Helper()

	req := f.request()
	req.Path = path

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	if _, err := f.runner.Run(ctx, req); err != nil {
		t.Fatalf("Run on the %s path: %v", path, err)
	}
	return strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
}

// withoutSessionFlag returns argv with the session flag and its value removed,
// which is everything the two paths are required to agree on.
func withoutSessionFlag(t *testing.T, argv []string) []string {
	t.Helper()

	for _, flag := range []string{"--session-id", "--resume"} {
		if i := slices.Index(argv, flag); i >= 0 {
			if i+1 >= len(argv) {
				t.Fatalf("%s has no value: %q", flag, argv)
			}
			return slices.Concat(argv[:i], argv[i+2:])
		}
	}
	t.Fatalf("argv carries neither --session-id nor --resume: %q", argv)
	return nil
}

// TestRunResumeSwapsOnlyTheSessionFlag is AC-SR-2.1 and AC-SR-2.9.
//
// The first run of a segment names the conversation it is about to start; every
// run after it resumes that conversation instead. The two flags cannot both be
// passed — claude rejects the combination unless --fork-session is also given —
// so this is a swap, not an addition.
//
// The rest of argv is asserted to be identical, and the tool boundary is the
// reason: a resumed session resolves --tools from the argv it is given and does
// not inherit a wider surface from the transcript it continues, so the two
// paths must present the same boundary or resuming would be a way to widen it.
func TestRunResumeSwapsOnlyTheSessionFlag(t *testing.T) {
	f := newFixture(t, successScript(t))

	cold := runArgv(t, f, PathCold)
	resumed := runArgv(t, f, PathResume)

	if i := slices.Index(cold, "--session-id"); i < 0 || cold[i+1] != testSessionID {
		t.Errorf("the cold path does not pass --session-id %s: %q", testSessionID, cold)
	}
	if slices.Contains(cold, "--resume") {
		t.Errorf("the cold path passes --resume: %q", cold)
	}
	if i := slices.Index(resumed, "--resume"); i < 0 || resumed[i+1] != testSessionID {
		t.Errorf("the resume path does not pass --resume %s: %q", testSessionID, resumed)
	}
	if slices.Contains(resumed, "--session-id") {
		t.Errorf("the resume path still passes --session-id, which claude rejects: %q", resumed)
	}
	if !slices.Equal(withoutSessionFlag(t, cold), withoutSessionFlag(t, resumed)) {
		t.Errorf("the two paths differ by more than the session flag:\ncold:   %q\nresume: %q", cold, resumed)
	}

	// The fallback runs cold in every respect. It is a different name for the
	// same invocation, kept only so the log line can say where it came from.
	if fallback := runArgv(t, f, PathColdAfterResumeRefused); !slices.Equal(fallback, cold) {
		t.Errorf("the cold rerun's argv differs from a cold run's:\nrerun: %q\ncold:  %q", fallback, cold)
	}
}

// refusalScript is P5 of the CLI contract: --resume on an id claude has no
// transcript for. It exits 1 having emitted no init event and exactly one
// terminal result event, which is why "no events" is not the test.
func refusalScript(t *testing.T) string {
	t.Helper()

	const refusal = `{"type":"result","subtype":"error_during_execution","is_error":true,` +
		`"duration_ms":0,"num_turns":0,"result":null,"total_cost_usd":0}`
	return dumpPrologue +
		"echo 'No conversation found with session ID: " + testSessionID + "' >&2\n" +
		emit(t, refusal) +
		"exit 1\n"
}

// TestRunReportsARefusedResume is AC-SR-2.7. A conversation the store believed
// in and claude cannot find is the one run failure that comes back as an error
// as well as a result: the caller's answer is to rerun cold, not to report it.
func TestRunReportsARefusedResume(t *testing.T) {
	f := newFixture(t, refusalScript(t))

	req := f.request()
	req.Path = PathResume

	result, err := f.runner.Run(t.Context(), req)
	if !errors.Is(err, ErrSessionNotResumable) {
		t.Fatalf("error = %v, want it to unwrap to ErrSessionNotResumable", err)
	}
	if result == nil {
		t.Fatal("no result came back with the refusal; the stderr is the evidence for it")
	}
	if result.Outcome != OutcomeFailed || result.ExitCode != 1 {
		t.Errorf("outcome = %s exit = %d, want failed and 1", result.Outcome, result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "No conversation found") {
		t.Errorf("stderr tail = %q, want claude's own explanation kept as evidence", result.Stderr)
	}
	if !result.HasResult || result.Events != 1 {
		t.Errorf("events = %d has_result = %t; the refusal emits one terminal event, so a test on "+
			"the event count instead of the init event would never fire", result.Events, result.HasResult)
	}
}

// The same failure on a cold run is an ordinary failure. A cold run is not
// asking to resume anything, so rerunning it would fix nothing — and the
// identical shape is reachable there for a different reason entirely: a
// --session-id that is already in use exits 1 with no events at all.
func TestRunDoesNotReportARefusalOnTheColdPath(t *testing.T) {
	f := newFixture(t, refusalScript(t))

	result, err := f.runner.Run(t.Context(), f.request())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Outcome != OutcomeFailed {
		t.Errorf("outcome = %s, want the ordinary failure", result.Outcome)
	}
}

// TestResumeRefusedClauses pins every clause of the classification. Each row is
// a failure that looks like a refusal in some respect and is not one.
func TestResumeRefusedClauses(t *testing.T) {
	tests := []struct {
		name     string
		resuming bool
		result   RunResult
		want     bool
	}{
		{
			name:     "a refused resume",
			resuming: true,
			result:   RunResult{Outcome: OutcomeFailed, ExitCode: 1},
			want:     true,
		},
		{
			name:     "the same shape on a cold run",
			resuming: false,
			result:   RunResult{Outcome: OutcomeFailed, ExitCode: 1},
		},
		{
			name:     "a resumed run that failed after its init event",
			resuming: true,
			result:   RunResult{Outcome: OutcomeFailed, ExitCode: 2, Model: "claude-fake-1"},
		},
		{
			name:     "a resumed run killed by the timeout",
			resuming: true,
			result:   RunResult{Outcome: OutcomeTimedOut, ExitCode: SignalExitCode},
		},
		{
			name:     "a resumed run abandoned by a shutting-down daemon",
			resuming: true,
			result:   RunResult{Outcome: OutcomeAbandoned, ExitCode: SignalExitCode},
		},
		{
			name:     "a resumed run killed by a signal",
			resuming: true,
			result:   RunResult{Outcome: OutcomeFailed, ExitCode: SignalExitCode},
		},
		{
			name:     "the SD-7 missing --verbose failure: no events, clean exit",
			resuming: true,
			result:   RunResult{Outcome: OutcomeNoResult, ExitCode: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.result
			if got := resumeRefused(tc.resuming, &result); got != tc.want {
				t.Errorf("resumeRefused = %t, want %t", got, tc.want)
			}
		})
	}
}

// AC-SR-2.10, the harness half: which path a run took is on every line it logs,
// so a fallback can be read out of daemon.log afterwards.
func TestRunLogsThePathItTook(t *testing.T) {
	for _, tc := range []struct {
		path RunPath
		want string
	}{
		{PathCold, "path=cold"},
		{"", "path=cold"},
		{PathResume, "path=resume"},
		{PathColdAfterResumeRefused, "path=cold_after_resume_refused"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			logs := &bytes.Buffer{}
			f := newFixture(t, successScript(t))
			f.runner.logger = slog.New(slog.NewTextHandler(logs, nil))

			req := f.request()
			req.Path = tc.path
			if _, err := f.runner.Run(t.Context(), req); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !strings.Contains(logs.String(), tc.want) {
				t.Errorf("the run log lines do not carry %s:\n%s", tc.want, logs)
			}
		})
	}
}

// An unrecognised path would run cold, and on a task with a live conversation
// running cold is the one outcome this feature exists to prevent.
// TestRunWithNoProfileArgvIsByteIdentical is AC-2.3, pinned as hard as
// possible: with Request.Tools empty — which is every run before this feature
// existed, and every run today that carries no lf:tools= label — argv is
// compared whole, not flag by flag, against the exact sequence the runner
// built before Tools and AllowedTools existed on the request at all.
//
// --model joined that sequence when claude.model did. It is unconditional and
// sits between --settings and --allowedTools; a change to its position or its
// value is a change to every run and should have to edit this list to happen.
func TestRunWithNoProfileArgvIsByteIdentical(t *testing.T) {
	f := newFixture(t, successScript(t))
	f.run(t)

	got := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	want := []string{
		"-p",
		"--output-format", "stream-json",
		"--verbose",
		"--session-id", testSessionID,
		"--tools", "Read,Edit",
		"--settings", filepath.Join(f.home, "claude-settings.json"),
		"--model", config.DefaultClaudeModel,
		"--autocompact", config.DefaultClaudeAutoCompact,
		"--allowedTools", "Read,Edit(./**)",
		"--strict-mcp-config",
	}
	if !slices.Equal(got, want) {
		t.Errorf("argv = %q\nwant  %q", got, want)
	}
}

func TestRunPassesConfiguredAutoCompactThreshold(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.AutoCompact = "250k"
	})
	f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--autocompact")
	if i < 0 || i+1 >= len(argv) || argv[i+1] != "250k" {
		t.Errorf("argv does not carry --autocompact 250k: %q", argv)
	}
}

// TestRunPassesTheConfiguredModel pins that claude.model reaches argv verbatim.
// The runner never derives or rewrites it: whatever config validation admitted
// is what the child is asked to run on, so the recorded command line is an
// honest record of which model did the work.
func TestRunPassesTheConfiguredModel(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.Model = "claude-opus-5"
	})
	f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--model")
	if i < 0 || i == len(argv)-1 {
		t.Fatalf("argv carries no --model value: %q", argv)
	}
	if got := argv[i+1]; got != "claude-opus-5" {
		t.Errorf("--model = %q, want %q", got, "claude-opus-5")
	}
}

// TestRunDefaultsTheModelWhenUnset covers the config.Claude that was assembled
// by hand rather than loaded — a test, or an embedder — where Model is the zero
// value. It must not reach argv empty: "--model" followed by "" is a flag with
// no value, which claude rejects, and the failure would land at run time on
// every run rather than at startup on one.
func TestRunDefaultsTheModelWhenUnset(t *testing.T) {
	f := newFixture(t, successScript(t)) // fixture leaves Model empty
	f.run(t)

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--model")
	if i < 0 || i == len(argv)-1 {
		t.Fatalf("argv carries no --model value: %q", argv)
	}
	if got := argv[i+1]; got != config.DefaultClaudeModel {
		t.Errorf("--model = %q, want the default %q", got, config.DefaultClaudeModel)
	}
}

// TestRunUsesTheProfileSurfaceExclusively is AC-2.5's first half: a request
// that carries a resolved profile's Tools and AllowedTools uses exactly those
// two lists for --tools and --allowedTools — never the runner's own
// configured surface, and never a union of the two. Granting nothing beyond
// what the profile says is the whole point of a ceiling that bounds profiles
// rather than merges into them.
func TestRunUsesTheProfileSurfaceExclusively(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.ToolCeiling = []string{"Read", "Edit", "Bash"}
	})

	req := f.request()
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: []string{"Bash"}, AllowedTools: []string{"Bash(ls:*)"}},
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	if _, err := f.runner.Run(ctx, req); err != nil {
		t.Fatalf("Run: %v", err)
	}

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	i := slices.Index(argv, "--tools")
	if i < 0 || argv[i+1] != "Bash" {
		t.Errorf("--tools = %v, want exactly the profile's [Bash], not the runner's own configured surface", argv)
	}
	if slices.Contains(argv, "Read,Edit") {
		t.Errorf("argv still carries the runner's configured --tools value: %q", argv)
	}
	j := slices.Index(argv, "--allowedTools")
	if j < 0 || argv[j+1] != "Bash(ls:*)" {
		t.Errorf("--allowedTools = %v, want exactly the profile's [Bash(ls:*)]", argv)
	}
}

// TestRunOmitsAllowedToolsWhenTheProfileHasNone is AC-2.5's second half: a
// profile with a granted --tools surface and no allowed_tools rules at all
// must omit --allowedTools rather than fill it in from the runner's own
// configured list — empty and absent are different flags to claude, and
// --tools stays unconditional regardless.
func TestRunOmitsAllowedToolsWhenTheProfileHasNone(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.ToolCeiling = []string{"Read", "Edit", "Bash"}
	})

	req := f.request()
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: []string{"Bash"}, AllowedTools: nil},
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	if _, err := f.runner.Run(ctx, req); err != nil {
		t.Fatalf("Run: %v", err)
	}

	argv := strings.Split(strings.TrimRight(f.dump(t, "argv.dump"), "\n"), "\n")
	if i := slices.Index(argv, "--tools"); i < 0 || argv[i+1] != "Bash" {
		t.Errorf("--tools = %v, want it present with value Bash", argv)
	}
	if slices.Contains(argv, "--allowedTools") {
		t.Errorf("argv carries --allowedTools for a profile with no rules: %q", argv)
	}
}

// TestRunRejectsAToolOutsideTheCeiling is AC-2.6: the ceiling is re-checked
// here, at the moment argv is about to be built, independently of whatever
// config validation proved about the operator's profile map at boot (D-2).
func TestRunRejectsAToolOutsideTheCeiling(t *testing.T) {
	f := newFixture(t, successScript(t), func(c *config.Claude) {
		c.ToolCeiling = []string{"Read", "Edit"}
	})

	req := f.request()
	req.Permissions = Permissions{
		Profile: "research",
		Payload: ToolSurface{Tools: []string{"Read", "Bash"}},
	}

	_, err := f.runner.Run(t.Context(), req)
	if err == nil {
		t.Fatal("Run accepted a tool outside the configured ceiling")
	}
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error %v does not unwrap to ErrInvalidRequest", err)
	}
	if !strings.Contains(err.Error(), "Bash") {
		t.Errorf("error does not name the offending tool: %v", err)
	}
}

// TestRunFallsBackToTheConfiguredCeilingWhenNoneWasResolved is the harness
// half of the New doc comment: a config.Claude assembled by hand rather than
// loaded through internal/config carries an empty ToolCeiling, and that must
// bound every profiled request by claude.tools rather than accept anything —
// an empty ceiling is not "no ceiling".
func TestRunFallsBackToTheConfiguredCeilingWhenNoneWasResolved(t *testing.T) {
	f := newFixture(t, successScript(t)) // ToolCeiling left unset by the fixture's baseline

	req := f.request()
	req.Permissions = Permissions{
		Profile: "research",
		// Outside claude.tools = [Read, Edit], and no ceiling was ever resolved.
		Payload: ToolSurface{Tools: []string{"Bash"}},
	}

	_, err := f.runner.Run(t.Context(), req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest: an empty ceiling must fall back to claude.tools, not allow-all", err)
	}
}

func TestRunRejectsAnUnknownPath(t *testing.T) {
	f := newFixture(t, successScript(t))

	req := f.request()
	req.Path = "continue"

	_, err := f.runner.Run(t.Context(), req)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

// logLineContaining returns the one line of a captured log buffer that
// contains substr, or fails the test. Text-handler output puts every
// attribute of one record on one line, so this is enough to isolate a single
// log line's attributes from the rest of a run's output.
func logLineContaining(t *testing.T, logs, substr string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	t.Fatalf("no log line contains %q:\n%s", substr, logs)
	return ""
}

// TestRunLogsAllowedToolsAndToolProfile is AC-4.5: the generic start line
// carries allowed_tools and tool_profile alongside tools, and — the part that
// actually matters — all three report the surface this run was invoked with,
// never the runner's own configured default, once a profile has been
// resolved for the request (runner.go:602-604).
func TestRunLogsAllowedToolsAndToolProfile(t *testing.T) {
	tests := []struct {
		name             string
		reqTools         []string
		reqAllowedTools  []string
		reqToolProfile   string
		wantTools        string
		wantAllowedTools string
		wantToolProfile  string
	}{
		{
			name:             "profile resolved/reports the request's surface, not the runner's configured default",
			reqTools:         []string{"Read", "Grep", "Bash"},
			reqAllowedTools:  []string{"Bash(git:*)", "Grep(*)"},
			reqToolProfile:   "research",
			wantTools:        "tools=Read,Grep,Bash",
			wantAllowedTools: "allowed_tools=Bash(git:*),Grep(*)",
			wantToolProfile:  "tool_profile=research",
		},
		{
			name:             "no profile/reports the runner's own configured surface",
			wantTools:        "tools=Read,Edit",
			wantAllowedTools: "allowed_tools=Read,Edit(./**)",
			wantToolProfile:  `tool_profile=""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			f := newFixture(t, successScript(t), func(c *config.Claude) {
				// Wide enough that a profile's tools sit inside the ceiling
				// but still outside the runner's own configured default
				// (Read, Edit), so the two surfaces are actually different.
				c.ToolCeiling = []string{"Read", "Edit", "Grep", "Bash"}
			})
			f.runner.logger = slog.New(slog.NewTextHandler(logs, nil))

			req := f.request()
			if len(tt.reqTools) > 0 || tt.reqToolProfile != "" {
				req.Permissions = Permissions{
					Profile: tt.reqToolProfile,
					Payload: ToolSurface{Tools: tt.reqTools, AllowedTools: tt.reqAllowedTools},
				}
			}

			if _, err := f.runner.Run(t.Context(), req); err != nil {
				t.Fatalf("Run: %v", err)
			}

			line := logLineContaining(t, logs.String(), `msg="coding agent started"`)
			for _, want := range []string{tt.wantTools, tt.wantAllowedTools, tt.wantToolProfile} {
				if !strings.Contains(line, want) {
					t.Errorf("coding agent start log line missing %q:\n%s", want, line)
				}
			}
		})
	}
}
