package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// These tests are about the thing `logs` exists for: showing a person what a
// run did. The fixtures are therefore the real stream-json shapes claude
// writes — a message with a content array of typed blocks — and not the flat
// {"type":..,"text":..} shorthand the older tests in cmd_logs_test.go use. Both
// have to render, which is why the shorthand is kept there and exercised here
// only as the fallback it is.

// streamLine marshals one event to a JSONL line.
func streamLine(t *testing.T, event map[string]any) string {
	t.Helper()

	line, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(line) + "\n"
}

// initLine is the run's system init event.
func initLine(t *testing.T, model, sessionID string) string {
	t.Helper()

	return streamLine(t, map[string]any{
		"type": "system", "subtype": "init", "model": model,
		"session_id": sessionID, "cwd": "/work/api", "tools": []string{"Bash", "Read", "Edit"},
	})
}

// assistantLine is an assistant message carrying the given content blocks.
func assistantLine(t *testing.T, parentToolUseID string, blocks ...map[string]any) string {
	t.Helper()

	return streamLine(t, map[string]any{
		"type":               "assistant",
		"parent_tool_use_id": parentToolUseID,
		"message":            map[string]any{"role": "assistant", "content": blocks},
	})
}

// userLine is a user message carrying the given content blocks — how a tool
// result comes back.
func userLine(t *testing.T, blocks ...map[string]any) string {
	t.Helper()

	return streamLine(t, map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": blocks},
	})
}

func textBlock(text string) map[string]any {
	return map[string]any{"type": "text", "text": text}
}

func thinkingBlock(text string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": text, "signature": "sig"}
}

func toolUseBlock(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResultBlock(toolUseID, content string, isError bool) map[string]any {
	return map[string]any{
		"type": "tool_result", "tool_use_id": toolUseID, "content": content, "is_error": isError,
	}
}

// aRunWithALog registers a finished run for issue 123 whose log is content,
// and returns the fixture.
func aRunWithALog(t *testing.T, content string) *logsHome {
	t.Helper()

	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	run := f.allocateRun(session, 1001, time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC))
	f.finishRun(run, store.RunSucceeded, 0, 42*time.Second)
	f.writeLog(session, 1, content)
	return f
}

// richLog is one run's worth of the events a real run produces.
func richLog(t *testing.T) string {
	t.Helper()

	return initLine(t, "claude-opus-5", "sess-abcdef123456") +
		assistantLine(t, "", textBlock("I'll start by reading the router.")) +
		assistantLine(t, "", thinkingBlock("the parser is the likely culprit")) +
		assistantLine(t, "", toolUseBlock("toolu_01", "Read", map[string]any{"file_path": "/work/api/router.go"})) +
		userLine(t, toolResultBlock("toolu_01", "package api\n\nfunc Route() {}\n", false)) +
		assistantLine(t, "", toolUseBlock("toolu_02", "Bash", map[string]any{
			"command": "go test ./...", "description": "Run the test suite",
		})) +
		userLine(t, toolResultBlock("toolu_02", "FAIL\tapi\t0.2s\nexit status 1\n", true)) +
		streamLine(t, map[string]any{
			"type": "result", "subtype": "success", "is_error": false,
			"result": "the fix is in router.go", "total_cost_usd": 0.8312,
			"duration_ms": 271000, "num_turns": 22,
		})
}

// TestLogsRendersTheEventStream is the heart of it: `litefactory logs <issue>`
// prints the run's events, not a path to a file of them.
func TestLogsRendersTheEventStream(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	for _, want := range []string{
		"system  init  model=claude-opus-5",
		"text    I'll start by reading the router.",
		"tool    Read  /work/api/router.go",
		"output  Read  3 lines, 29 B  package api",
		"tool    Bash  go test ./...",
		"output  Bash  error  2 lines",
		"result  ok  4m31s  $0.8312  22 turns",
		"result  the fix is in router.go",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the rendered log is missing %q:\n%s", want, stdout)
		}
	}

	// The summary still comes first, because knowing a run failed after 4
	// minutes and $0.83 is the first thing you want.
	if !strings.Contains(stdout, "run 0001 of github:acme/api#123") {
		t.Errorf("the default view lost the run summary:\n%s", stdout)
	}

	// And what it must not do: hand back the JSON.
	if strings.Contains(stdout, `"tool_use"`) || strings.Contains(stdout, `{"type":`) {
		t.Errorf("the default view printed raw JSON:\n%s", stdout)
	}
	// Thinking is the one kind that is off unless asked for.
	if strings.Contains(stdout, "the parser is the likely culprit") {
		t.Errorf("thinking is shown by default; it is the bulkiest kind:\n%s", stdout)
	}
}

// TestLogsRendersTheNewestRunByDefault covers the run selection of the default
// view, which used to be the run listing.
func TestLogsRendersTheNewestRunByDefault(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	first := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(first, store.RunSucceeded, 0, time.Second)
	second := f.allocateRun(session, 1002, time.Now().UTC())
	f.finishRun(second, store.RunSucceeded, 0, time.Second)
	f.writeLog(session, 1, assistantLine(t, "", textBlock("the older run")))
	f.writeLog(session, 2, assistantLine(t, "", textBlock("the newer run")))

	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "the newer run") || strings.Contains(stdout, "the older run") {
		t.Errorf("the default view did not show the newest run:\n%s", stdout)
	}
	if !strings.Contains(stdout, "run 0002") {
		t.Errorf("the default view did not name the run it showed:\n%s", stdout)
	}
}

// TestLogsOnlySelectsKinds is the "show me what the agents are doing" case:
// the tool calls and the text, without the tool output they read.
func TestLogsOnlySelectsKinds(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	tests := []struct {
		only    string
		want    []string
		unwant  []string
		nonZero bool
	}{
		{
			only:   "agent",
			want:   []string{"text    I'll start", "think   the parser", "tool    Read", "tool    Bash"},
			unwant: []string{"output  ", "result  ", "system  "},
		},
		{
			only:   "tool",
			want:   []string{"tool    Read  /work/api/router.go", "tool    Bash  go test ./..."},
			unwant: []string{"text    ", "output  ", "result  "},
		},
		{
			only:   "text,result",
			want:   []string{"text    I'll start", "result  the fix is in router.go"},
			unwant: []string{"tool    ", "output  ", "think   "},
		},
		{
			only:   "all",
			want:   []string{"system  ", "text    ", "think   ", "tool    ", "output  ", "result  "},
			unwant: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.only, func(t *testing.T) {
			code, stdout, stderr := f.run("logs", "123", "--only", tc.only)
			if code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("--only %s is missing %q:\n%s", tc.only, want, stdout)
				}
			}
			for _, unwanted := range tc.unwant {
				if strings.Contains(stdout, unwanted) {
					t.Errorf("--only %s still shows %q:\n%s", tc.only, unwanted, stdout)
				}
			}
			// A filtered view is a view to read or pipe line by line; the
			// summary block on top of it would be noise.
			if strings.Contains(stdout, "claude session") {
				t.Errorf("--only %s printed the run summary:\n%s", tc.only, stdout)
			}
		})
	}
}

// TestLogsRejectsAnUnknownKind: a typo must not silently print nothing.
func TestLogsRejectsAnUnknownKind(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	code, stdout, stderr := f.run("logs", "123", "--only", "tools")
	if code != ExitUsage {
		t.Fatalf("--only tools exited %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "unknown kind") || !strings.Contains(stderr, "tool") {
		t.Errorf("stderr = %q, want it to name the mistake and the valid kinds", stderr)
	}
	if stdout != "" {
		t.Errorf("a rejected --only still printed %q", stdout)
	}
}

// TestLogsGrepFiltersLines is the grep half of the ask.
func TestLogsGrepFiltersLines(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	tests := []struct {
		name   string
		args   []string
		want   []string
		unwant []string
	}{
		{
			name:   "matching lines only",
			args:   []string{"--grep", "go test"},
			want:   []string{"tool    Bash  go test ./..."},
			unwant: []string{"Read", "system", "result"},
		},
		{
			name:   "case insensitive",
			args:   []string{"--grep", "ROUTER.GO", "-i"},
			want:   []string{"tool    Read  /work/api/router.go", "result  the fix is in router.go"},
			unwant: []string{"tool    Bash"},
		},
		{
			name:   "inverted",
			args:   []string{"--grep", "^(tool|output|system)", "--invert"},
			want:   []string{"text    I'll start by reading the router.", "result  ok"},
			unwant: []string{"tool    ", "output  ", "system  "},
		},
		{
			name:   "combined with --only",
			args:   []string{"--only", "tool", "--grep", "Bash"},
			want:   []string{"tool    Bash  go test ./..."},
			unwant: []string{"Read", "text"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := f.run(append([]string{"logs", "123"}, tc.args...)...)
			if code != ExitOK {
				t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("%v is missing %q:\n%s", tc.args, want, stdout)
				}
			}
			for _, unwanted := range tc.unwant {
				if strings.Contains(stdout, unwanted) {
					t.Errorf("%v still shows %q:\n%s", tc.args, unwanted, stdout)
				}
			}
		})
	}
}

// TestLogsGrepRejectsABadPattern keeps a regexp mistake a usage error rather
// than a panic or an empty view.
func TestLogsGrepRejectsABadPattern(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	code, _, stderr := f.run("logs", "123", "--grep", "(unclosed")
	if code != ExitUsage {
		t.Fatalf("a bad --grep exited %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "--grep") {
		t.Errorf("stderr = %q, want it to name the flag", stderr)
	}
}

// TestLogsGrepAppliesToRaw covers grepping the JSONL itself, for when you want
// the events rather than the rendering of them.
func TestLogsGrepAppliesToRaw(t *testing.T) {
	f := aRunWithALog(t, richLog(t))

	code, stdout, stderr := f.run("logs", "123", "--raw", "--grep", "tool_use")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("--raw --grep emitted something that is not a whole event: %q", line)
		}
		if !strings.Contains(line, "tool_use") {
			t.Errorf("--raw --grep kept a line that does not match: %q", line)
		}
	}
	if !strings.Contains(stdout, `"go test ./..."`) {
		t.Errorf("--raw --grep dropped a matching event:\n%s", stdout)
	}
}

// TestLogsMarksSubAgentActivity: with a Task sub-agent running, the only thing
// distinguishing its events from the main agent's is parent_tool_use_id.
func TestLogsMarksSubAgentActivity(t *testing.T) {
	log := assistantLine(t, "", toolUseBlock("toolu_parent99", "Task", map[string]any{
		"description": "Audit the parser", "subagent_type": "Explore",
		"prompt": "a very long briefing that should not be what is shown",
	})) +
		assistantLine(t, "toolu_parent99", textBlock("reading parse.go")) +
		assistantLine(t, "", textBlock("back in the main agent"))
	f := aRunWithALog(t, log)

	code, stdout, stderr := f.run("logs", "123", "--only", "text,tool")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "tool    Task  Audit the parser") {
		t.Errorf("a Task call should report what the sub-agent was asked to do:\n%s", stdout)
	}
	if strings.Contains(stdout, "very long briefing") {
		t.Errorf("a Task call inlined its whole prompt:\n%s", stdout)
	}
	if !strings.Contains(stdout, "text    [agent parent99] reading parse.go") {
		t.Errorf("sub-agent output is not marked as such:\n%s", stdout)
	}
	if strings.Contains(stdout, "[agent parent99] back in the main agent") {
		t.Errorf("main-agent output was marked as a sub-agent's:\n%s", stdout)
	}
}

// TestLogsFullShowsWholeToolOutput covers --full, for when the preview is not
// enough.
func TestLogsFullShowsWholeToolOutput(t *testing.T) {
	body := "line one\nline two\nline three\n"
	log := assistantLine(t, "", toolUseBlock("toolu_01", "Bash", map[string]any{
		"command": "printf '" + strings.Repeat("x", 300) + "'",
	})) + userLine(t, toolResultBlock("toolu_01", body, false))
	f := aRunWithALog(t, log)

	code, clipped, stderr := f.run("logs", "123", "--only", "tool,output")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if strings.Contains(clipped, "line three") {
		t.Errorf("the default view inlined the whole tool output:\n%s", clipped)
	}
	if !strings.Contains(clipped, "…") {
		t.Errorf("a shortened value is not marked as shortened:\n%s", clipped)
	}

	code, full, stderr := f.run("logs", "123", "--only", "tool,output", "--full")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	for _, want := range []string{"output  line one", "output  line two", "output  line three", strings.Repeat("x", 300)} {
		if !strings.Contains(full, want) {
			t.Errorf("--full is missing %q:\n%s", want, full)
		}
	}
}

// TestLogsRendersWhatItCannotDecode is the "nothing is silently dropped" rule.
// A run log is written by another program; a line that is not an event we know
// is still a line that was written.
func TestLogsRendersWhatItCannotDecode(t *testing.T) {
	log := "not json at all\n" +
		streamLine(t, map[string]any{"type": "stream_event", "event": map[string]any{"type": "ping"}}) +
		assistantLine(t, "", textBlock("still here"))
	f := aRunWithALog(t, log)

	code, stdout, stderr := f.run("logs", "123", "--only", "other,text")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	for _, want := range []string{"other   not json at all", "other   {\"event\"", "text    still here"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the rendered log is missing %q:\n%s", want, stdout)
		}
	}
}

// TestLogsRenderedOutputIsUnredacted extends AC-5.7 to the rendered view. The
// rendering is a new place a well-meant redaction could be inserted, and the
// rule has not changed: `logs` is the local, complete truth.
func TestLogsRenderedOutputIsUnredacted(t *testing.T) {
	const secret = "ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"
	f := aRunWithALog(t, assistantLine(t, "", textBlock("the token is "+secret))+
		userLine(t, toolResultBlock("toolu_01", secret, false)))

	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, secret) {
		t.Errorf("the rendered view redacted %q. `logs` is the unredacted view by design (AC-5.7):\n%s",
			secret, stdout)
	}
}

func TestLogsRendersCodexThroughTheSameView(t *testing.T) {
	log := streamLine(t, map[string]any{"type": "thread.started", "thread_id": "01a0622b-f3d3-74c0-8a18-b80eff0ae0b0"}) +
		streamLine(t, map[string]any{"type": "turn.started"}) +
		streamLine(t, map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "done"}}) +
		streamLine(t, map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 10}})
	f := aRunWithALog(t, log)
	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}
	for _, want := range []string{"system  init  session=b80eff0ae0b0", "text    done", "result  ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("rendered Codex log missing %q:\n%s", want, stdout)
		}
	}
}

// TestLogsFollowRendersAsItStreams is the second half of the ask: watching a
// live run in a form you can read, ending at the terminal event.
func TestLogsFollowRendersAsItStreams(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC()) // left running
	path := f.runLogPath(session, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}

	lines := []string{
		initLine(t, "claude-opus-5", "sess-live"),
		assistantLine(t, "", textBlock("reading the issue")),
		assistantLine(t, "", toolUseBlock("toolu_01", "Edit", map[string]any{"file_path": "/work/api/fix.go"})),
		streamLine(t, map[string]any{"type": "result", "is_error": false, "result": "done"}),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	out := &recordingWriter{}
	writerDone := make(chan error, 1)
	go func() {
		defer close(writerDone)
		for i, line := range lines {
			if err := appendToFile(path, line); err != nil {
				writerDone <- err
				return
			}
			// Hand over the next event only once the follower has rendered
			// this one. A follower that waits for the file to be complete
			// stalls here rather than passing by accident.
			if err := out.waitForWrites(ctx, i+1); err != nil {
				writerDone <- err
				return
			}
		}
	}()

	var stderr bytes.Buffer
	code := f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--only", "agent,system,result"})

	if err := <-writerDone; err != nil {
		t.Fatalf("the live writer never finished, so the follower was not streaming: %v", err)
	}
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatal("the follow ran to the deadline instead of stopping at the terminal event")
	}

	want := "system  init  model=claude-opus-5  session=sess-live  cwd=/work/api  tools=3\n" +
		"text    reading the issue\n" +
		"tool    Edit  /work/api/fix.go\n" +
		"result  ok\n" +
		"result  done\n"
	if got := out.joined(); got != want {
		t.Errorf("followed output\n got %q\nwant %q", got, want)
	}
}

// TestLogsFollowRenderedNeverEmitsAPartialLine is the rendered half of the
// mid-write case: a half-written event must not be rendered as an event, and
// must not be lost when it is completed either.
func TestLogsFollowRenderedNeverEmitsAPartialLine(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC())
	path := f.runLogPath(session, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}

	whole := assistantLine(t, "", textBlock("a complete event"))
	split := assistantLine(t, "", textBlock("an event written in two halves"))
	head, tail := split[:len(split)/2], split[len(split)/2:]

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	out := &recordingWriter{}
	writerDone := make(chan error, 1)
	go func() {
		defer close(writerDone)
		if err := appendToFile(path, whole+head); err != nil {
			writerDone <- err
			return
		}
		if err := out.waitForWrites(ctx, 1); err != nil {
			writerDone <- err
			return
		}
		select {
		case <-ctx.Done():
			writerDone <- ctx.Err()
			return
		case <-time.After(4 * followInterval):
		}
		if n := out.count(); n != 1 {
			writerDone <- fmt.Errorf("follower emitted %d writes while only a partial line was pending, want 1", n)
			return
		}
		if err := appendToFile(path, tail+resultLine("done")); err != nil {
			writerDone <- err
		}
	}()

	var stderr bytes.Buffer
	code := f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--only", "text"})

	if err := <-writerDone; err != nil {
		t.Fatalf("live writer: %v", err)
	}
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	want := "text    a complete event\ntext    an event written in two halves\n"
	if got := out.joined(); got != want {
		t.Errorf("followed output\n got %q\nwant %q", got, want)
	}
}

// TestLogsAcceptsATaskRef covers the other way a task is named. The canonical
// ref is what every comment marker and log line calls a task, so it is what
// people copy; it also carries the repository, so it settles an issue number
// that exists twice without --repo.
func TestLogsAcceptsATaskRef(t *testing.T) {
	f := newLogsHome(t)
	api := f.session(f.repo("acme", "api"), 123)
	web := f.session(f.repo("acme", "web"), 123)
	for _, session := range []store.Session{api, web} {
		run := f.allocateRun(session, 1001, time.Now().UTC())
		f.finishRun(run, store.RunSucceeded, 0, time.Second)
		f.writeLog(session, 1, assistantLine(t, "", textBlock("the log of "+session.TaskRef)))
	}

	code, stdout, stderr := f.run("logs", web.TaskRef)
	if code != ExitOK {
		t.Fatalf("logs %s exited %d, want %d\nstderr: %s", web.TaskRef, code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "the log of github:acme/web#123") {
		t.Errorf("a task ref did not select its own task:\n%s", stdout)
	}
	if strings.Contains(stdout, "acme/api") {
		t.Errorf("a task ref still matched the other repository:\n%s", stdout)
	}

	// A ref plus --repo is a contradiction waiting to happen, not a default.
	if code, _, stderr := f.run("logs", web.TaskRef, "--repo", "acme/api"); code != ExitUsage {
		t.Errorf("a ref with --repo exited %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	// And a malformed ref is a usage error naming what was wrong with it.
	code, _, stderr = f.run("logs", "github:acme/web")
	if code != ExitUsage {
		t.Errorf("a malformed ref exited %d, want %d\nstderr: %s", code, ExitUsage, stderr)
	}
	if !strings.Contains(stderr, "task ref") {
		t.Errorf("stderr = %q, want it to say what the argument should have looked like", stderr)
	}
}

// TestLogsRunWithNoEventLogIsReportedNotFatal covers a run that was allocated
// and killed before claude wrote a byte: the summary is still worth having.
func TestLogsRunWithNoEventLogIsReportedNotFatal(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunFailed, store.SignalExitCode, time.Second)

	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "run 0001") || !strings.Contains(stdout, "no event log at") {
		t.Errorf("stdout should carry the summary and say the log is missing:\n%s", stdout)
	}

	// --raw has no summary to fall back on, so it stays an error.
	if code, _, stderr := f.run("logs", "123", "--raw"); code == ExitOK {
		t.Errorf("--raw on a run with no log exited %d, want non-zero\nstderr: %s", code, stderr)
	}
}
