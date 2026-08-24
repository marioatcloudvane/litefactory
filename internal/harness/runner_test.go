package harness

import (
	"context"
	"encoding/json"
	"errors"
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

	// The session id is naming's deterministic UUID v5, so the segments row and
	// the value claude was given cannot drift.
	want, err := naming.ClaudeSessionID(testRef, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID != want {
		t.Errorf("session id = %q, want %q", result.SessionID, want)
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
	sessionID, err := naming.ClaudeSessionID(testRef, 1)
	if err != nil {
		t.Fatal(err)
	}

	for _, pair := range [][2]string{
		{"-p", ""},
		{"--output-format", "stream-json"},
		{"--verbose", ""},
		{"--session-id", sessionID},
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
	f := newFixture(t, emit(t, initLine)+"/bin/sleep 120\n")

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	started := time.Now()
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
	if elapsed := time.Since(started); elapsed > 20*time.Second {
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
