package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// Every test in this file runs against a real $LF_HOME: a real temporary
// directory, a real migrated SQLite file and real JSONL files on disk. Nothing
// about the filesystem is faked, because the thing under test *is* the reading
// of those files — and because AC-5.8 (works with the daemon stopped) is only
// meaningful if the command really does reach the disk and nothing else.
//
// None of these tests call t.Parallel: they set LF_HOME with t.Setenv, which
// forbids it.

// logsHome is one $LF_HOME with a database, a router and captured output.
type logsHome struct {
	t    *testing.T
	home string
	db   *store.DB
	env  *Env
}

// newLogsHome creates an empty $LF_HOME, migrates a database into it and
// points the LF_HOME environment variable at it.
func newLogsHome(t *testing.T) *logsHome {
	t.Helper()

	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	// An implicit issue follower uses the agent recorded on its completed run
	// to obtain its successor deadline. Keep every follow fixture configured
	// just as a real LF_HOME is; an absent config is an actionable error, not a
	// legacy success fallback.
	if err := os.WriteFile(config.NewPaths(home).ConfigPath(), config.DefaultFileContents(), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	db, err := store.Open(t.Context(), config.NewPaths(home).DBPath())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &logsHome{t: t, home: home, db: db}
}

// run dispatches a command line and returns its exit code and output. The
// fixture's database handle stays open across the call on purpose: the command
// opens its own connection, and that it can is part of AC-5.8.
func (f *logsHome) run(args ...string) (code int, stdout, stderr string) {
	f.t.Helper()

	var out, errOut bytes.Buffer
	code = f.newRouter(&out, &errOut).Run(f.t.Context(), args)
	return code, out.String(), errOut.String()
}

// newRouter builds a router over the real command surface writing to out.
func (f *logsHome) newRouter(out, errOut io.Writer) *Router {
	f.t.Helper()

	f.env = &Env{
		Stdout:  out,
		Stderr:  errOut,
		Stdin:   strings.NewReader(""),
		Logger:  slog.New(slog.DiscardHandler),
		Version: "test",
	}
	return NewRouter("litefactory", f.env, Commands())
}

// repo registers a clone.
func (f *logsHome) repo(owner, name string) store.Repo {
	f.t.Helper()

	repo, err := f.db.UpsertRepo(f.t.Context(), store.Repo{
		Path:          filepath.Join(f.t.TempDir(), name),
		Forge:         store.ForgeGitHub,
		Owner:         owner,
		Name:          name,
		RemoteURL:     "git@github.com:" + owner + "/" + name + ".git",
		BaseBranch:    "main",
		FeaturePrefix: "lf",
		WorktreeRoot:  filepath.Join(f.t.TempDir(), ".lf-worktrees", name),
	})
	if err != nil {
		f.t.Fatalf("upsert repo %s/%s: %v", owner, name, err)
	}
	return repo
}

// session creates the task for an issue in repo and freezes its branch, the
// way the first run would.
func (f *logsHome) session(repo store.Repo, issue int) store.Session {
	f.t.Helper()

	session, err := f.db.GetOrCreateSession(f.t.Context(), repo, issue)
	if err != nil {
		f.t.Fatalf("create session: %v", err)
	}
	slug := fmt.Sprintf("%d-a-task", issue)
	session, err = f.db.FreezeSessionSlug(f.t.Context(), session.ID, slug, "lf/"+slug)
	if err != nil {
		f.t.Fatalf("freeze slug: %v", err)
	}
	return session
}

// ref is the naming ref a session's run files live under.
func (f *logsHome) ref(session store.Session) naming.Ref {
	f.t.Helper()

	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		f.t.Fatalf("parse ref %q: %v", session.TaskRef, err)
	}
	return ref
}

// allocateRun inserts a run row with the log path internal/naming derives, so
// the fixture and the command agree on where the JSONL lives without either
// being told.
func (f *logsHome) allocateRun(session store.Session, triggerCommentID int64, startedAt time.Time) store.Run {
	f.t.Helper()

	ref := f.ref(session)
	segment, err := f.db.GetOrCreateSegment(f.t.Context(), session.ID, store.FirstSegmentOrdinal, "claude-session-1", store.AgentClaude)
	if err != nil {
		f.t.Fatalf("segment: %v", err)
	}
	run, err := f.db.AllocateRun(f.t.Context(), store.AllocateRunParams{
		SessionID:        session.ID,
		SegmentID:        segment.ID,
		Mode:             store.ModeSpike,
		Agent:            store.AgentClaude,
		TriggerCommentID: triggerCommentID,
		StartedAt:        startedAt,
		LogPath: func(ordinal int) string {
			path, err := naming.RunLogPath(f.home, ref, ordinal)
			if err != nil {
				f.t.Errorf("run log path: %v", err)
				return ""
			}
			return path
		},
	})
	if err != nil {
		f.t.Fatalf("allocate run: %v", err)
	}
	return run
}

// finishRun records a terminal outcome on a run.
func (f *logsHome) finishRun(run store.Run, status store.RunStatus, exitCode int64, after time.Duration) store.Run {
	f.t.Helper()

	cost := 0.1234
	finished, err := f.db.FinishRun(f.t.Context(), run.ID, store.RunOutcome{
		Status:     status,
		ExitCode:   &exitCode,
		FinishedAt: run.StartedAt.Add(after),
		CostUSD:    &cost,
		CommitSHA:  "abc1234",
	})
	if err != nil {
		f.t.Fatalf("finish run: %v", err)
	}
	return finished
}

// reportRun records the result comment for a run.
func (f *logsHome) reportRun(run store.Run, resultCommentID int64) store.Run {
	f.t.Helper()

	reported, err := f.db.CompleteRunReport(f.t.Context(), run.ID, resultCommentID)
	if err != nil {
		f.t.Fatalf("complete report: %v", err)
	}
	return reported
}

// writeLog writes a run's JSONL verbatim and returns its path. content is not
// normalised in any way: several tests depend on writing bytes that a
// line-oriented writer would tidy up.
func (f *logsHome) writeLog(session store.Session, ordinal int, content string) string {
	f.t.Helper()

	path, err := naming.RunLogPath(f.home, f.ref(session), ordinal)
	if err != nil {
		f.t.Fatalf("run log path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatalf("create run dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		f.t.Fatalf("write run log: %v", err)
	}
	return path
}

// writeMeta writes a run's NNNN.meta.json.
func (f *logsHome) writeMeta(session store.Session, ordinal int, meta map[string]any) string {
	f.t.Helper()

	path, err := naming.RunMetaPath(f.home, f.ref(session), ordinal)
	if err != nil {
		f.t.Fatalf("run meta path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		f.t.Fatalf("create run dir: %v", err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		f.t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatalf("write run meta: %v", err)
	}
	return path
}

// runLogPath is where a run's JSONL belongs, without writing it.
func (f *logsHome) runLogPath(session store.Session, ordinal int) string {
	f.t.Helper()

	path, err := naming.RunLogPath(f.home, f.ref(session), ordinal)
	if err != nil {
		f.t.Fatalf("run log path: %v", err)
	}
	return path
}

// event renders one line of stream-json.
func event(kind, text string) string {
	line, err := json.Marshal(map[string]any{"type": kind, "text": text})
	if err != nil {
		panic(err)
	}
	return string(line) + "\n"
}

// resultLine renders the terminal result event, the line that ends a follow.
func resultLine(text string) string {
	line, err := json.Marshal(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": text, "total_cost_usd": 0.1234,
	})
	if err != nil {
		panic(err)
	}
	return string(line) + "\n"
}

// TestLogsListsRunsNewestLast is AC-5.1: one line per run carrying every field
// the criterion names, oldest first.
//
// The listing moved behind --runs when the default view became "show me the
// log". The criterion is about the listing, not about which flag reaches it.
func TestLogsListsRunsNewestLast(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	start := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	first := f.allocateRun(session, 1001, start)
	f.reportRun(f.finishRun(first, store.RunSucceeded, 0, 90*time.Second), 9001)
	second := f.allocateRun(session, 1002, start.Add(time.Hour))
	f.finishRun(second, store.RunFailed, 3, 2*time.Second)
	f.allocateRun(session, 1003, start.Add(2*time.Hour)) // still running

	code, stdout, stderr := f.run("logs", "123", "--runs")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	// One line per run, in ordinal order: newest last.
	var runLines []string
	for line := range strings.Lines(stdout) {
		if strings.HasPrefix(strings.TrimSpace(line), "000") {
			runLines = append(runLines, line)
		}
	}
	if len(runLines) != 3 {
		t.Fatalf("got %d run lines, want 3:\n%s", len(runLines), stdout)
	}
	for i, wantOrdinal := range []string{"0001", "0002", "0003"} {
		if !strings.HasPrefix(strings.TrimSpace(runLines[i]), wantOrdinal) {
			t.Errorf("run line %d starts %q, want run %s: the newest run must be last",
				i, strings.TrimSpace(runLines[i]), wantOrdinal)
		}
	}

	// Every field AC-5.1 names, on the line for the run that carries it.
	wantFields := []struct {
		line   int
		fields []string
	}{
		{0, []string{
			"0001",                    // ordinal
			"2026-08-23T09:00:00.000", // start time
			"1m30s",                   // duration
			"0",                       // exit code
			store.ModeSpike,           // mode
			string(store.RunReported), // status
			"1001",                    // trigger comment id
			"9001",                    // result comment id
			f.runLogPath(session, 1),  // log path
		}},
		{1, []string{"0002", "2s", "3", string(store.RunFailed), "1002", f.runLogPath(session, 2)}},
		{2, []string{"0003", store.ModeSpike, string(store.RunRunning), "1003", f.runLogPath(session, 3)}},
	}
	for _, want := range wantFields {
		for _, field := range want.fields {
			if !strings.Contains(runLines[want.line], field) {
				t.Errorf("run line %d is missing %q:\n%s", want.line, field, runLines[want.line])
			}
		}
	}

	// A run with no exit code, duration or result comment yet prints a
	// placeholder rather than a zero that would read as "exited 0".
	running := strings.Fields(runLines[2])
	if got := running[3]; got != noValue {
		t.Errorf("the running run's exit code column is %q, want %q: it has not exited", got, noValue)
	}
}

// TestLogsListsATaskWithNoRuns covers the empty case: a task that exists but
// has never run is reported, not treated as missing.
func TestLogsListsATaskWithNoRuns(t *testing.T) {
	f := newLogsHome(t)
	f.session(f.repo("acme", "api"), 123)

	code, stdout, stderr := f.run("logs", "123")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "no runs yet") {
		t.Errorf("stdout = %q, want it to say the task has no runs yet", stdout)
	}
}

// TestLogsAmbiguousIssuePrintsCandidates is AC-5.2. The store reports
// ambiguity as a typed error rather than a sentinel precisely so this output
// is possible, so the assertion is on the candidate list, not just the exit
// code.
func TestLogsAmbiguousIssuePrintsCandidates(t *testing.T) {
	f := newLogsHome(t)
	f.session(f.repo("acme", "api"), 123)
	f.session(f.repo("acme", "web"), 123)

	code, _, stderr := f.run("logs", "123")
	if code == ExitOK {
		t.Fatalf("an ambiguous issue exited %d, want non-zero\nstderr: %s", code, stderr)
	}
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d: the command line needs --repo", code, ExitUsage)
	}
	for _, want := range []string{"acme/api", "acme/web", "--repo"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr is missing %q — the candidates are the whole point:\n%s", want, stderr)
		}
	}

	// And --repo resolves it.
	code, stdout, stderr := f.run("logs", "123", "--repo", "acme/web")
	if code != ExitOK {
		t.Fatalf("--repo acme/web exited %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "github:acme/web#123") {
		t.Errorf("--repo acme/web resolved to the wrong task:\n%s", stdout)
	}
	if strings.Contains(stdout, "acme/api") {
		t.Errorf("--repo acme/web still shows acme/api:\n%s", stdout)
	}
}

// TestLogsRejectsAMalformedRepoFlag keeps --repo owner/name from being read as
// something it is not.
func TestLogsRejectsAMalformedRepoFlag(t *testing.T) {
	f := newLogsHome(t)
	f.session(f.repo("acme", "api"), 123)

	for _, value := range []string{"api", "acme/", "/api", "acme/api/extra"} {
		code, _, stderr := f.run("logs", "123", "--repo", value)
		if code != ExitUsage {
			t.Errorf("--repo %q exited %d, want %d\nstderr: %s", value, code, ExitUsage, stderr)
		}
	}
}

// TestLogsUnknownTaskIsCleanAndNonZero is AC-5.3: a clear message and no stack
// trace.
func TestLogsUnknownTaskIsCleanAndNonZero(t *testing.T) {
	f := newLogsHome(t)
	f.session(f.repo("acme", "api"), 123)

	code, stdout, stderr := f.run("logs", "999")
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitFailure, stderr)
	}
	if !strings.Contains(stderr, "no task for issue 999") {
		t.Errorf("stderr = %q, want a clear message naming the issue", stderr)
	}
	// No stack trace, and no store-internal wording leaking through either.
	for _, unwanted := range []string{"goroutine", "panic:", ".go:", "store:"} {
		if strings.Contains(stderr, unwanted) || strings.Contains(stdout, unwanted) {
			t.Errorf("output contains %q, which is a stack trace or an internal detail:\nstdout: %s\nstderr: %s",
				unwanted, stdout, stderr)
		}
	}
}

// TestLogsRejectsANonNumericIssue keeps the positional honest.
func TestLogsRejectsANonNumericIssue(t *testing.T) {
	f := newLogsHome(t)

	for _, arg := range []string{"abc", "0", "-1x", "12.5"} {
		code, _, stderr := f.run("logs", arg)
		if code != ExitUsage {
			t.Errorf("logs %q exited %d, want %d\nstderr: %s", arg, code, ExitUsage, stderr)
		}
	}
}

// TestLogsWithNoDatabaseIsAPrecondition covers the machine that has never run
// `litefactory init`.
func TestLogsWithNoDatabaseIsAPrecondition(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)

	var stdout, stderr bytes.Buffer
	env := &Env{Stdout: &stdout, Stderr: &stderr, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
	code := NewRouter("litefactory", env, Commands()).Run(t.Context(), []string{"logs", "123"})

	if code != ExitPrecondition {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitPrecondition, stderr.String())
	}
	if !strings.Contains(stderr.String(), "litefactory init") {
		t.Errorf("stderr = %q, want it to name the fix", stderr.String())
	}
}

// TestLogsRunSummary is AC-5.4: the S-3 metadata summary plus the final result
// text.
func TestLogsRunSummary(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	start := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	run := f.allocateRun(session, 1001, start)
	f.reportRun(f.finishRun(run, store.RunSucceeded, 0, 42*time.Second), 9001)

	f.writeMeta(session, 1, map[string]any{
		"run_ordinal":       1,
		"task_ref":          session.TaskRef,
		"session_id":        session.ID,
		"segment_ordinal":   1,
		"claude_session_id": "sess-abcdef",
		"mode":              store.ModeSpike,
		"model":             "claude-sonnet-4-5",
		"prompt":            strings.Repeat("p", 4096),
		"worktree_path":     "/tmp/wt/task-123",
		"branch":            session.Branch,
		"sha_before":        "1111111",
		"sha_after":         "abc1234",
		"exit_code":         0,
		"started_at":        start.Format(time.RFC3339),
		"finished_at":       start.Add(42 * time.Second).Format(time.RFC3339),
		"duration_ms":       42000,
		"cost_usd":          0.1234,
		"files_touched":     []string{"main.go", "README.md"},
		"result_comment_id": 9001,
		"error":             nil,
	})
	logPath := f.writeLog(session, 1,
		event("assistant", "thinking")+resultLine("the fix is in main.go"))

	code, stdout, stderr := f.run("logs", "123", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	for _, want := range []string{
		"run 0001 of github:acme/api#123",
		string(store.RunReported),
		store.ModeSpike,
		"claude-sonnet-4-5",
		"sess-abcdef",
		"2026-08-23T09:00:00.000",
		"42s",
		"$0.1234",
		session.Branch,
		"/tmp/wt/task-123",
		"1111111",
		"abc1234",
		"main.go README.md",
		"1001",
		"9001",
		logPath,
		"the fix is in main.go",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("summary is missing %q:\n%s", want, stdout)
		}
	}

	// The prompt is summarised, never inlined: it would bury the result.
	if strings.Contains(stdout, strings.Repeat("p", 100)) {
		t.Errorf("the summary printed the whole prompt:\n%s", stdout)
	}
	if !strings.Contains(stdout, "4096 bytes") {
		t.Errorf("the summary does not report the prompt's size:\n%s", stdout)
	}
}

// TestLogsRunSummaryAgentAndSessionRef is AC-4.7 and T-4.6's human label: a
// meta.json carrying the new agent and session_ref keys is what a run written
// after this story lands looks like, and the human summary must show them
// under the "<agent> session" label rather than the old fixed "claude
// session" one.
func TestLogsRunSummaryAgentAndSessionRef(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 456)
	start := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	run := f.allocateRun(session, 2001, start)
	f.reportRun(f.finishRun(run, store.RunSucceeded, 0, 10*time.Second), 9002)

	f.writeMeta(session, 1, map[string]any{
		"run_ordinal":       1,
		"task_ref":          session.TaskRef,
		"session_id":        session.ID,
		"segment_ordinal":   1,
		"agent":             store.AgentClaude,
		"session_ref":       "new-format-session-ref",
		"claude_session_id": "new-format-session-ref",
		"mode":              store.ModeSpike,
		"started_at":        start.Format(time.RFC3339),
		"finished_at":       start.Add(10 * time.Second).Format(time.RFC3339),
		"duration_ms":       10000,
	})
	f.writeLog(session, 1, event("assistant", "thinking")+resultLine("done"))

	code, stdout, stderr := f.run("logs", "456", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	if !strings.Contains(stdout, "claude session") {
		t.Errorf("summary is missing the %q label:\n%s", "claude session", stdout)
	}
	if !strings.Contains(stdout, "new-format-session-ref") {
		t.Errorf("summary is missing the session ref:\n%s", stdout)
	}
}

// todoWriteResolved and todoWriteRequested are the measured finding this story
// exists to make visible ("Settled before planning", IMPLEMENTATION_PLAN.md):
// eight tools requested, seven resolved, TodoWrite silently dropped with no
// error raised anywhere. The resolved order is copied verbatim from a real
// init event.
var (
	todoWriteResolved  = []string{"Task", "Bash", "Edit", "Glob", "Grep", "Read", "Write"}
	todoWriteRequested = append(append([]string{}, todoWriteResolved...), "TodoWrite")
)

// TestLogsRunSummaryTodoWriteWorkedExample is AC-4.4: `litefactory logs`
// prints the resolved tools, the profile, and the requested tools when they
// differ from what resolved. This is the named regression for the finding
// that motivates the whole story — eight requested, seven resolved, no error
// raised anywhere else.
func TestLogsRunSummaryTodoWriteWorkedExample(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 321)
	start := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	run := f.allocateRun(session, 2001, start)
	f.finishRun(run, store.RunSucceeded, 0, 5*time.Second)

	f.writeMeta(session, 1, map[string]any{
		"run_ordinal":     1,
		"task_ref":        session.TaskRef,
		"mode":            store.ModeSpike,
		"model":           "claude-sonnet-4-5",
		"tools":           todoWriteResolved,
		"tools_requested": todoWriteRequested,
		"tool_profile":    "research",
		"prompt":          "do the thing",
	})
	f.writeLog(session, 1, resultLine("done"))

	code, stdout, stderr := f.run("logs", "321", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}

	for _, want := range []string{
		"Task Bash Edit Glob Grep Read Write", // resolved
		"research",                            // profile
		"tools requested",                     // the row's own label
		"(not granted: TodoWrite)",            // the tool that was asked for and dropped
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("summary is missing %q:\n%s", want, stdout)
		}
	}
}

// TestLogsRunSummaryOmitsRequestedToolsWhenTheyMatchResolved is the other half
// of AC-4.4's "when they differ": printing the requested list a second time
// when it agrees with what resolved would only make a real disagreement harder
// to spot, so the row is omitted rather than repeated.
func TestLogsRunSummaryOmitsRequestedToolsWhenTheyMatchResolved(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 322)
	start := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	run := f.allocateRun(session, 2002, start)
	f.finishRun(run, store.RunSucceeded, 0, 5*time.Second)

	same := []string{"Read", "Edit"}
	f.writeMeta(session, 1, map[string]any{
		"run_ordinal":     1,
		"task_ref":        session.TaskRef,
		"mode":            store.ModeSpike,
		"tools":           same,
		"tools_requested": same,
		"prompt":          "do the thing",
	})
	f.writeLog(session, 1, resultLine("done"))

	code, stdout, stderr := f.run("logs", "322", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if strings.Contains(stdout, "tools requested") {
		t.Errorf("the requested-tools row should be omitted when it agrees with what resolved:\n%s", stdout)
	}
}

// TestLogsRequestedTools is the direct, table-driven counterpart to the two
// tests above: it pins the exact formatting logsRequestedTools produces,
// including the case a full command-level fixture cannot easily set up — a run
// whose meta carries a requested surface but no resolved one at all.
func TestLogsRequestedTools(t *testing.T) {
	tests := []struct {
		name string
		meta runMeta
		want string
	}{
		{
			name: "nothing requested/omitted",
			meta: runMeta{Tools: todoWriteResolved},
			want: "",
		},
		{
			name: "requested matches resolved/omitted",
			meta: runMeta{Tools: todoWriteResolved, ToolsRequested: todoWriteResolved},
			want: "",
		},
		{
			name: "TodoWrite worked example/names the missing tool",
			meta: runMeta{Tools: todoWriteResolved, ToolsRequested: todoWriteRequested},
			want: "Task Bash Edit Glob Grep Read Write TodoWrite (not granted: TodoWrite)",
		},
		{
			name: "no resolved surface at all/reports the full requested list unmarked",
			meta: runMeta{ToolsRequested: todoWriteRequested},
			want: "Task Bash Edit Glob Grep Read Write TodoWrite",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logsRequestedTools(tt.meta); got != tt.want {
				t.Errorf("logsRequestedTools() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLogsOutputCarriesNoLabelSyntax is the C2/S-13 guardrail applied to
// `logs`: nothing originating from GitHub — a label body, the "lf:tools="
// syntax itself — may ever reach the printed summary.
func TestLogsOutputCarriesNoLabelSyntax(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 323)
	start := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)

	run := f.allocateRun(session, 2003, start)
	f.finishRun(run, store.RunSucceeded, 0, 5*time.Second)

	f.writeMeta(session, 1, map[string]any{
		"run_ordinal":     1,
		"task_ref":        session.TaskRef,
		"mode":            store.ModeSpike,
		"tools":           todoWriteResolved,
		"tools_requested": todoWriteRequested,
		"tool_profile":    "research",
		"prompt":          "do the thing",
	})
	f.writeLog(session, 1, resultLine("done"))

	code, stdout, stderr := f.run("logs", "323", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if strings.Contains(stdout, "lf:tools=") {
		t.Errorf("`logs` output leaked label syntax:\n%s", stdout)
	}
}

// TestRunMetaCarriesNoLabelField is the structural half of the same guardrail:
// runMeta has no field for raw label text at all, so there is nothing for a
// future change to wire up by mistake (S-13).
func TestRunMetaCarriesNoLabelField(t *testing.T) {
	rt := reflect.TypeOf(runMeta{})
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		if strings.Contains(strings.ToLower(name), "label") {
			t.Errorf("runMeta has a field named %q; labels must never reach the logs pipeline (S-13)", name)
		}
	}
}

// TestLogsRunSummaryWithoutMetadata covers the abnormal-termination case: S-3
// says the metadata file is only best-effort when the daemon is killed, so a
// run with a row and a log but no meta.json must still be showable.
func TestLogsRunSummaryWithoutMetadata(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	run := f.allocateRun(session, 1001, time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC))
	f.finishRun(run, store.RunFailed, store.SignalExitCode, time.Second)
	f.writeLog(session, 1, event("assistant", "started")) // no result event either

	code, stdout, stderr := f.run("logs", "123", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	for _, want := range []string{"run 0001", string(store.RunFailed), "not written", "no result event"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("summary is missing %q:\n%s", want, stdout)
		}
	}
}

// TestLogsUnknownRunOrdinal keeps --run N honest about what exists.
func TestLogsUnknownRunOrdinal(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC())

	code, _, stderr := f.run("logs", "123", "--run", "7")
	if code == ExitOK {
		t.Fatalf("an unknown run ordinal exited %d, want non-zero", code)
	}
	if !strings.Contains(stderr, "no run 7") {
		t.Errorf("stderr = %q, want it to say run 7 does not exist", stderr)
	}
}

// rawFixtureLog is a run log written to be hostile to any implementation that
// "tidies up" what it copies: a CRLF line ending, a blank line, a lone byte
// that is not valid JSON, non-ASCII text and — deliberately — no trailing
// newline.
const rawFixtureLog = "{\"type\":\"system\",\"subtype\":\"init\"}\r\n" +
	"\n" +
	"{\"type\":\"assistant\",\"text\":\"héllo — ünicode\"}\n" +
	"not json at all\n" +
	"{\"type\":\"assistant\",\"text\":\"trailing line with no newline\"}"

// TestLogsRawIsByteForByte is AC-5.5. The comparison is on bytes, not on lines
// or on decoded events: --raw is the command people reach for when they need to
// know exactly what claude emitted, so a re-encode that happened to be
// semantically equal would still be a bug.
func TestLogsRawIsByteForByte(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunSucceeded, 0, time.Second)
	path := f.writeLog(session, 1, rawFixtureLog)

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := f.newRouter(&stdout, &stderr).Run(t.Context(), []string{"logs", "123", "--run", "1", "--raw"})
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), onDisk) {
		t.Errorf("--raw is not byte-for-byte.\n got %q\nwant %q", stdout.Bytes(), onDisk)
	}
	if stderr.Len() != 0 {
		t.Errorf("--raw wrote %q to stderr; the JSONL and nothing else belongs on stdout", stderr.String())
	}
}

// TestLogsRawDefaultsToTheNewestRun covers --raw without --run.
func TestLogsRawDefaultsToTheNewestRun(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	first := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(first, store.RunSucceeded, 0, time.Second)
	second := f.allocateRun(session, 1002, time.Now().UTC())
	f.finishRun(second, store.RunSucceeded, 0, time.Second)
	f.writeLog(session, 1, event("assistant", "the older run"))
	f.writeLog(session, 2, event("assistant", "the newer run"))

	code, stdout, stderr := f.run("logs", "123", "--raw")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout, "the newer run") || strings.Contains(stdout, "the older run") {
		t.Errorf("--raw did not default to the newest run:\n%s", stdout)
	}
}

// TestLogsOutputIsUnredacted is AC-5.7.
//
// S-7 redaction is outbound-only by design: the whole point of `logs` is that
// the local copy is the full truth, so a token-shaped string in the JSONL comes
// back verbatim. If a future change routes this output through internal/redact
// — however well meant — this test is what says no.
func TestLogsOutputIsUnredacted(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	// Token shapes S-7 redacts on the way out to a forge comment.
	secrets := []string{
		"ghp_0123456789abcdefghijklmnopqrstuvwxyzAB",
		"github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNO",
		"gho_0123456789abcdefghijklmnopqrstuvwxyzAB",
		"sk-ant-api03-0123456789abcdefghijklmnopqrstuvwxyz",
	}
	var log strings.Builder
	for _, secret := range secrets {
		log.WriteString(event("assistant", "the token is "+secret))
	}
	log.WriteString(resultLine("done, using " + secrets[0]))

	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunSucceeded, 0, time.Second)
	path := f.writeLog(session, 1, log.String())

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := f.newRouter(&stdout, &stderr).Run(t.Context(), []string{"logs", "123", "--run", "1", "--raw"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), onDisk) {
		t.Fatalf("--raw altered the log; redaction is outbound-only (AC-5.7)")
	}
	for _, secret := range secrets {
		if !strings.Contains(stdout.String(), secret) {
			t.Errorf("--raw redacted %q. `logs` is the unredacted view by design (AC-5.7)", secret)
		}
	}

	// The summary's result text is the same rule.
	code, summary, stderrText := f.run("logs", "123", "--run", "1")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderrText)
	}
	if !strings.Contains(summary, secrets[0]) {
		t.Errorf("the run summary redacted the result text:\n%s", summary)
	}
}

// TestLogsNeverImportsRedact is the structural half of AC-5.7. The behavioural
// test above only catches a redaction that changes these particular fixtures;
// this catches the import itself, which is the thing that is actually
// forbidden.
func TestLogsNeverImportsRedact(t *testing.T) {
	for _, name := range logsSourceFiles {
		for _, imported := range importsOf(t, name) {
			if strings.Contains(imported, "internal/redact") {
				t.Errorf("%s imports %s. `logs` output is unredacted by design (AC-5.7): "+
					"S-7 redaction is outbound-only, and you want the token in the log at 23:00 "+
					"and never in a public comment.", name, imported)
			}
		}
	}
}

// logsSourceFiles is every file the `logs` command is implemented in. The
// structural guards below hold for the command, not for one file of it, so
// splitting the renderer out must not be a way around them.
var logsSourceFiles = []string{"cmd_logs.go", "cmd_logs_render.go"}

// importsOf returns the import paths of a source file in this package.
func importsOf(t *testing.T, name string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	paths := make([]string, 0, len(file.Imports))
	for _, imported := range file.Imports {
		paths = append(paths, imported.Path.Value)
	}
	return paths
}

// TestLogsUsesNoDaemonIPC is AC-5.8. The database and the files are the only
// things `logs` may touch, so no watcher dependency and no socket, signal or
// lock coordination may appear.
func TestLogsUsesNoDaemonIPC(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunSucceeded, 0, time.Second)
	f.writeLog(session, 1, resultLine("done"))

	// No daemon is running, and none is started.
	for _, args := range [][]string{
		{"logs", "123"},
		{"logs", "123", "--run", "1"},
		{"logs", "123", "--run", "1", "--raw"},
	} {
		if code, _, stderr := f.run(args...); code != ExitOK {
			t.Fatalf("%v exited %d with the daemon stopped\nstderr: %s", args, code, stderr)
		}
	}

	// The daemon's singleton lock is never created or taken by a read.
	if _, err := os.Stat(config.NewPaths(f.home).LockPath()); err == nil {
		t.Error("`logs` created the daemon lock file; it must not coordinate with the daemon at all")
	}

	// And the package brings in no filesystem watcher: -f is a ticker.
	for _, name := range logsSourceFiles {
		for _, imported := range importsOf(t, name) {
			for _, forbidden := range []string{"fsnotify", "net/rpc", "os/signal"} {
				if strings.Contains(imported, forbidden) {
					t.Errorf("%s imports %s; -f follows with a ticker and `logs` uses no IPC",
						name, imported)
				}
			}
		}
	}
}

// TestLogsRunFlagSelectsTheRunAfterThePositional is the AC-B.4 regression
// guard with teeth: it is not enough that "logs 123 --run 2" parses, the value
// 2 has to reach the handler and select run 2.
func TestLogsRunFlagSelectsTheRunAfterThePositional(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	first := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(first, store.RunSucceeded, 0, time.Second)
	second := f.allocateRun(session, 1002, time.Now().UTC())
	f.finishRun(second, store.RunSucceeded, 0, time.Second)
	f.writeLog(session, 1, resultLine("the first run's result"))
	f.writeLog(session, 2, resultLine("the second run's result"))

	// Both orders must behave identically: Go's flag package stops at the
	// first non-flag token, so the positional is sliced off before parsing.
	for _, args := range [][]string{
		{"logs", "123", "--run", "2"},
		{"logs", "--run", "2", "123"},
		{"logs", "123", "-run=2"},
	} {
		code, stdout, stderr := f.run(args...)
		if code != ExitOK {
			t.Fatalf("%v exited %d, want %d\nstderr: %s", args, code, ExitOK, stderr)
		}
		if !strings.Contains(stdout, "run 0002") || !strings.Contains(stdout, "the second run's result") {
			t.Errorf("%v did not select run 2:\n%s", args, stdout)
		}
		if strings.Contains(stdout, "the first run's result") {
			t.Errorf("%v selected run 1:\n%s", args, stdout)
		}
	}
}

// recordingWriter captures each Write separately so a test can assert on what
// was handed to the writer, not merely on the concatenation. It is what makes
// "a partial line is never emitted as an event" checkable: byte-wise, an
// emitted partial followed by its completion is indistinguishable from the
// whole line.
type recordingWriter struct {
	mu     sync.Mutex
	writes []string
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, string(p))
	return len(p), nil
}

// count returns how many writes have landed so far.
func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.writes)
}

// snapshot returns a copy of the writes so far.
func (w *recordingWriter) snapshot() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.writes...)
}

// joined returns everything written, concatenated.
func (w *recordingWriter) joined() string {
	return strings.Join(w.snapshot(), "")
}

// waitForWrites blocks until the follower has emitted at least n writes.
//
// This is what turns "it streams" from a timing guess into an assertion: the
// live writer below refuses to append its next event until the follower has
// already emitted the previous one, so a follower that waited for the file to
// be complete could never make progress and the test fails on the deadline
// rather than passing by accident.
func (w *recordingWriter) waitForWrites(ctx context.Context, n int) error {
	for w.count() < n {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %d writes, saw %d: %w", n, w.count(), ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

// appendToFile appends s to the file at path, the way the daemon appends an
// event to a run it is in the middle of.
func appendToFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// TestLogsFollowStreamsFromALiveWriter is AC-5.6 against a real second writer:
// a goroutine appends to the JSONL while the command follows it, and the
// terminal event ends the follow.
//
// It follows with --raw. What AC-5.6 is about is the streaming — that an event
// reaches stdout as it is appended rather than when the run ends — and --raw
// is the view whose output can be compared to the file byte for byte, so it is
// the one that can prove it. That the rendered follow streams the same way is
// TestLogsFollowRendersAsItStreams.
func TestLogsFollowStreamsFromALiveWriter(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC()) // left running: nothing has finished it
	path := f.runLogPath(session, 1)

	// The file does not exist yet, which is the real starting state: the run
	// row is allocated before the subprocess writes its first event.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}

	lines := []string{
		event("system", "init"),
		event("assistant", "reading the issue"),
		event("assistant", "writing the fix"),
		resultLine("done"),
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
			// Hand over the next event only once the follower has emitted this
			// one. A follower that is not streaming stalls here.
			if err := out.waitForWrites(ctx, i+1); err != nil {
				writerDone <- err
				return
			}
		}
	}()

	var stderr bytes.Buffer
	code := f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"})

	if err := <-writerDone; err != nil {
		t.Fatalf("the live writer never finished, so the follower was not streaming: %v", err)
	}
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}

	// The terminal event ended the follow, rather than the context timing out.
	if ctx.Err() != nil {
		t.Fatalf("the follow ran to the deadline instead of stopping at the terminal event")
	}
	if got, want := out.joined(), strings.Join(lines, ""); got != want {
		t.Errorf("followed output\n got %q\nwant %q", got, want)
	}
	for i, write := range out.snapshot() {
		if !strings.HasSuffix(write, "\n") {
			t.Errorf("write %d is %q, which is not a whole line", i, write)
		}
	}
}

// TestLogsFollowNeverEmitsAPartialLine covers the daemon caught mid-write. A
// line without its newline yet must not reach stdout as a truncated event, and
// must not be dropped once it is completed either.
func TestLogsFollowNeverEmitsAPartialLine(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC())
	path := f.runLogPath(session, 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}

	whole := event("assistant", "a complete event")
	split := event("assistant", "an event written in two halves")
	head, tail := split[:len(split)/2], split[len(split)/2:]

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	out := &recordingWriter{}
	writerDone := make(chan error, 1)
	go func() {
		defer close(writerDone)
		// A whole event, then only the first half of the next one.
		if err := appendToFile(path, whole+head); err != nil {
			writerDone <- err
			return
		}
		if err := out.waitForWrites(ctx, 1); err != nil {
			writerDone <- err
			return
		}
		// Give the follower several ticker periods with the half line sitting
		// in the file. It must not emit it.
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
	code := f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"})

	if err := <-writerDone; err != nil {
		t.Fatalf("live writer: %v", err)
	}
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}

	// Nothing truncated went out, and nothing was lost.
	for i, write := range out.snapshot() {
		if !strings.HasSuffix(write, "\n") {
			t.Errorf("write %d is %q: a partial line was emitted as an event", i, write)
		}
		if !json.Valid([]byte(write)) {
			t.Errorf("write %d is not a whole JSON event: %q", i, write)
		}
	}
	if got, want := out.joined(), whole+split+resultLine("done"); got != want {
		t.Errorf("followed output\n got %q\nwant %q", got, want)
	}
}

// TestLogsFollowOnAFinishedRunPrintsAndExits is the other half of AC-5.6: a run
// that has already ended must not wait for events that will never come. The
// fixture log has no terminal result event on purpose — waiting for one is
// exactly the bug this guards against.
func TestLogsFollowOnAFinishedRunPrintsAndExits(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunFailed, store.SignalExitCode, time.Second)
	content := event("system", "init") + event("assistant", "killed before it finished")
	f.writeLog(session, 1, content)

	// Long enough that a hang is unmistakable, short enough that the test
	// still ends.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := f.newRouter(&stdout, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"})
	elapsed := time.Since(start)

	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatal("-f on a finished run waited for the context deadline instead of exiting")
	}
	if elapsed > 2*time.Second {
		t.Errorf("-f on a finished run took %s; it should print what is there and exit", elapsed)
	}
	if stdout.String() != content {
		t.Errorf("-f on a finished run printed\n got %q\nwant %q", stdout.String(), content)
	}
}

// TestLogsFollowOnAFinishedRunPrintsAnUnterminatedTail covers the daemon that
// was killed mid-line. Once the run has ended no newline is ever coming, so the
// remaining bytes are the file's real tail rather than half of a future event,
// and -f stays as complete as --raw.
func TestLogsFollowOnAFinishedRunPrintsAnUnterminatedTail(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)

	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunFailed, store.SignalExitCode, time.Second)
	content := event("system", "init") + `{"type":"assistant","text":"cut off mid`
	f.writeLog(session, 1, content)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	if code := f.newRouter(&stdout, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if stdout.String() != content {
		t.Errorf("-f dropped the unterminated tail of a finished run\n got %q\nwant %q", stdout.String(), content)
	}
}

// TestLogsFollowStopsOnAnInterrupt is the SIGINT half of AC-5.6. main turns
// SIGINT into a cancelled context, so cancelling the context is what a Ctrl-C
// looks like from inside the handler.
func TestLogsFollowStopsOnAnInterrupt(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	f.allocateRun(session, 1001, time.Now().UTC()) // still running: no terminal event will arrive
	content := event("system", "init")
	f.writeLog(session, 1, content)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	out := &recordingWriter{}
	interrupted := make(chan error, 1)
	go func() {
		defer close(interrupted)
		// Interrupt only once the follower has printed what was already there,
		// so the cancellation lands in the middle of a live follow.
		waitCtx, waitCancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer waitCancel()
		if err := out.waitForWrites(waitCtx, 1); err != nil {
			interrupted <- err
			return
		}
		cancel()
	}()

	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "-f", "--raw"}) }()

	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("an interrupted follow exited %d, want %d: Ctrl-C is a normal way to end one\nstderr: %s",
				code, ExitOK, stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("-f did not stop after the context was cancelled")
	}
	if err := <-interrupted; err != nil {
		t.Fatalf("waiting to interrupt: %v", err)
	}
	if got := out.joined(); got != content {
		t.Errorf("the interrupted follow printed %q, want %q", got, content)
	}
}

// TestLogsFollowWithoutAnEventLog reports a finished run whose file never
// appeared rather than polling forever for it.
func TestLogsFollowWithoutAnEventLog(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	run := f.allocateRun(session, 1001, time.Now().UTC())
	f.finishRun(run, store.RunFailed, store.SignalExitCode, time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	code := f.newRouter(&stdout, &stderr).Run(ctx, []string{"logs", "123", "-f"})
	if code == ExitOK {
		t.Fatalf("a missing event log exited %d, want non-zero", code)
	}
	if !strings.Contains(stderr.String(), "no event log at") {
		t.Errorf("stderr = %q, want it to name the missing file", stderr.String())
	}
}

// TestLogsFollowIssueRollsForward keeps the issue-level form useful after the
// run that was current at invocation has ended. Raw stdout remains suitable
// for piping: rollover context goes exclusively to stderr.
func TestLogsFollowIssueRollsForward(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	first := f.allocateRun(session, 1001, time.Now().UTC())
	firstLog := event("assistant", "first") + resultLine("first done")
	f.writeLog(session, first.Ordinal, firstLog)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	out := &recordingWriter{}
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "-f", "--raw"}) }()

	if err := out.waitForWrites(ctx, 2); err != nil {
		t.Fatalf("wait for first run: %v", err)
	}
	second := f.allocateRun(session, 1002, time.Now().UTC())
	secondLog := event("assistant", "second")
	f.writeLog(session, second.Ordinal, secondLog)

	if err := out.waitForWrites(ctx, 3); err != nil {
		t.Fatalf("wait for second run: %v", err)
	}
	cancel()
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("follow did not stop after cancellation")
	}
	if got, want := out.joined(), firstLog+secondLog; got != want {
		t.Errorf("raw stdout\n got %q\nwant %q", got, want)
	}
	if got, want := stderr.String(), "--- run 0002 ---\n"; got != want {
		t.Errorf("raw rollover stderr\n got %q\nwant %q", got, want)
	}
}

// TestLogsFollowExplicitRunStaysPinned makes the explicit ordinal contract
// distinct from issue-level following even when a newer log already exists.
func TestLogsFollowExplicitRunStaysPinned(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	first := f.allocateRun(session, 1001, time.Now().UTC())
	firstLog := event("assistant", "first") + resultLine("first done")
	f.writeLog(session, first.Ordinal, firstLog)
	second := f.allocateRun(session, 1002, time.Now().UTC())
	f.writeLog(session, second.Ordinal, event("assistant", "second")+resultLine("second done"))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if code := f.newRouter(&stdout, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}
	if got := stdout.String(); got != firstLog {
		t.Errorf("explicit follow output\n got %q\nwant %q", got, firstLog)
	}
	if stderr.Len() != 0 {
		t.Errorf("explicit follow emitted rollover context: %q", stderr.String())
	}
}

// TestLogsFollowTerminalEventsAfterEOF regresses the retained-file EOF path:
// each supported terminal protocol event is appended only after a prior line
// has proved the follower drained to EOF.
func TestLogsFollowTerminalEventsAfterEOF(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"claude result/exits", resultLine("done")},
		{"codex completed/exits", event("turn.completed", "done")},
		{"codex failed/exits", event("turn.failed", "provider limit")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLogsHome(t)
			session := f.session(f.repo("acme", "api"), 123)
			run := f.allocateRun(session, 1001, time.Now().UTC())
			path := f.writeLog(session, run.Ordinal, event("assistant", "before eof"))
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			out := &recordingWriter{}
			var stderr bytes.Buffer
			done := make(chan int, 1)
			go func() {
				done <- f.newRouter(out, &stderr).Run(ctx, []string{"logs", "123", "--run", "1", "-f", "--raw"})
			}()
			if err := out.waitForWrites(ctx, 1); err != nil {
				t.Fatalf("wait for initial EOF drain: %v", err)
			}
			if err := appendToFile(path, tt.line); err != nil {
				t.Fatalf("append terminal event: %v", err)
			}
			select {
			case code := <-done:
				if code != ExitOK {
					t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
				}
			case <-ctx.Done():
				t.Fatalf("wait for terminal event: %v", ctx.Err())
			}
		})
	}
}
