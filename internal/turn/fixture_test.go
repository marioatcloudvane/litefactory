package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// runnerSet wraps a single claude double as the one-entry [RunnerSet] every
// test in this package needs — every task this suite drives is a Claude
// segment. A nil runner produces a nil set rather than a set carrying a nil
// Runner, so a fixture built without one still fails Options.validate() the
// same way it did before RunnerSet existed, instead of reaching a nil Runner
// at call time.
func runnerSet(r Runner) RunnerSet {
	if r == nil {
		return nil
	}
	return RunnerSet{agent.Kind(store.AgentClaude): r}
}

// The task every test in this package works on. The slug and branch are spelled
// out rather than derived so that a change to the derivation rules shows up
// here as a failing expectation instead of a quietly renamed branch (D-4).
const (
	testIssue      = 123
	testIssueTitle = "Add a /health endpoint"
	testSlug       = "add-a-health-endpoint"
	testBranch     = "lf/123-" + testSlug
	testComment    = int64(9001)
	testInstance   = "daemon-instance-a"
	testOtherOwner = "daemon-instance-b"
)

// fixture is one prepared turn: a real git clone with a real origin, a migrated
// database with the repository and task registered, and a claude that is
// whatever the test needs it to be.
//
// Everything here is real except claude. The guarantees this story makes are
// about git's behaviour and SQLite's, and a mock of either would only confirm
// what we already believed.
type fixture struct {
	t         *testing.T
	repo      *gittest.Repo
	db        *store.DB
	paths     config.Paths
	storeRepo store.Repo
	session   store.Session
	logs      *bytes.Buffer
	opts      Options
}

func newFixture(t *testing.T, runner Runner) *fixture {
	t.Helper()

	repo := gittest.New(t)
	// A sibling of the clone, per S-6, and already symlink-resolved because it
	// is derived from the fixture's own resolved path.
	root := filepath.Dir(repo.Dir)
	home := filepath.Join(root, "lfhome")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create LF_HOME: %v", err)
	}

	paths := config.NewPaths(home)
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	storeRepo, err := db.UpsertRepo(t.Context(), store.Repo{
		Path:          repo.Dir,
		Forge:         store.ForgeGitHub,
		Owner:         "acme",
		Name:          "acme-api",
		RemoteURL:     repo.Origin,
		BaseBranch:    gittest.DefaultBranch,
		FeaturePrefix: naming.DefaultFeaturePrefix,
		WorktreeRoot:  filepath.Join(root, "worktrees"),
	})
	if err != nil {
		t.Fatalf("register repository: %v", err)
	}
	session, err := db.GetOrCreateSession(t.Context(), storeRepo, testIssue)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	return &fixture{
		t:         t,
		repo:      repo,
		db:        db,
		paths:     paths,
		storeRepo: storeRepo,
		session:   session,
		logs:      logs,
		opts: Options{
			ConflictPreviewer: renderer,
			DB:                db,
			Git:               binary,
			Runners:           runnerSet(runner),
			Paths:             paths,
			InstanceID:        testInstance,
			// Set explicitly so the tests do not depend on the developer's
			// global git identity. resolveAuthor's fallback has its own test.
			Author: git.Author{Name: "Mario Meir-Huber", Email: "mario@example.invalid"},
			// A real serialising gate, not a no-op: every test in this package
			// runs the same git the daemon runs, and a fixture that skipped the
			// gate would be testing a path production does not take. Tests that
			// need two fixtures to share one gate overwrite this field.
			CloneGate: newTestGate(),
			Logger:    slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		},
	}
}

// request is the turn every test runs unless it says otherwise: the owner
// asking for a spike on issue 123.
func (f *fixture) request() Request {
	body := "/implement add a health endpoint that reports the build sha"
	return Request{
		Mode:      agent.ModeImplement,
		Agent:     agent.Kind(store.AgentClaude),
		SessionID: f.session.ID,
		Issue: Issue{
			Number: testIssue,
			Title:  testIssueTitle,
			Body:   "The service has no way to say whether it is up.",
			Author: "octo-reporter",
		},
		Trigger: Trigger{
			CommentID: testComment,
			Body:      body,
			Author:    "marioatcloudvane",
		},
		Command:      ParseCommand(body),
		ToolProfile:  "implement",
		Tools:        []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"},
		AllowedTools: config.DefaultClaude().AllowedTools,
	}
}

// execute runs the turn and fails the test on an error return, which is the
// shape every test but the argument-validation ones expects: a turn that went
// badly is a Result, not an error.
func (f *fixture) execute(req Request) Result {
	f.t.Helper()

	result, err := Execute(f.t.Context(), f.opts, req)
	if err != nil {
		f.t.Fatalf("Execute: %v\nlogs:\n%s", err, f.logs)
	}
	return result
}

// executeRefused runs a turn that is expected to fail before it consumed its
// trigger. Since US-001 of #210 such a turn returns an error rather than a
// reported result: the comment stays unconsumed so a blip does not burn it.
func (f *fixture) executeRefused(req Request) (Result, error) {
	f.t.Helper()

	result, err := Execute(f.t.Context(), f.opts, req)
	if err == nil {
		f.t.Fatalf("turn was expected to refuse: %+v", result)
	}
	return result, err
}

func (f *fixture) worktreeDir() string {
	f.t.Helper()

	dir, err := naming.WorktreePath(f.storeRepo.WorktreeRoot, testIssue)
	if err != nil {
		f.t.Fatalf("derive the worktree path: %v", err)
	}
	return dir
}

// reloadSession reads the session row back. Nothing is asserted from the
// in-memory copy: the point of most of these tests is what was persisted.
func (f *fixture) reloadSession() store.Session {
	f.t.Helper()

	session, err := f.db.GetSession(f.t.Context(), f.session.ID)
	if err != nil {
		f.t.Fatalf("read session: %v", err)
	}
	return session
}

func (f *fixture) runs() []store.Run {
	f.t.Helper()

	runs, err := f.db.ListRuns(f.t.Context(), f.session.ID)
	if err != nil {
		f.t.Fatalf("list runs: %v", err)
	}
	return runs
}

func (f *fixture) repoRow() store.Repo {
	f.t.Helper()

	repo, err := f.db.GetRepo(f.t.Context(), f.storeRepo.ID)
	if err != nil {
		f.t.Fatalf("read repository: %v", err)
	}
	return repo
}

// readMeta decodes the run's NNNN.meta.json, which is the on-disk half of the
// contract with `logs` and with LF-4.
func (f *fixture) readMeta(ordinal int) harness.Meta {
	f.t.Helper()

	path, err := harness.MetaPath(f.paths.Home(), f.ref(), ordinal)
	if err != nil {
		f.t.Fatalf("derive the metadata path: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path is derived from the fixture's own $LF_HOME.
	if err != nil {
		f.t.Fatalf("read %s: %v", path, err)
	}
	var meta harness.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		f.t.Fatalf("decode %s: %v", path, err)
	}
	return meta
}

func (f *fixture) ref() naming.Ref {
	f.t.Helper()

	ref, err := naming.ParseRef(f.session.TaskRef)
	if err != nil {
		f.t.Fatalf("parse task ref: %v", err)
	}
	return ref
}

// originHas reports whether the bare origin carries the branch — the only
// honest way to tell a push that happened from one that was merely attempted.
func (f *fixture) originHas(branch string) bool {
	f.t.Helper()

	return f.repo.GitIn(f.repo.Origin,
		"for-each-ref", "--format=%(refname:short)", "refs/heads/"+branch) != ""
}

// stubRunner is the claude double. It is a function so that a test can decide
// what the run does — including touching the worktree, or the database, or
// panicking — at the exact moment the subprocess would have been running.
type stubRunner struct {
	mu       sync.Mutex
	fn       func(ctx context.Context, req harness.Request) (*harness.RunResult, error)
	calls    int
	requests []harness.Request
}

func newStubRunner(fn func(ctx context.Context, req harness.Request) (*harness.RunResult, error)) *stubRunner {
	return &stubRunner{fn: fn}
}

func (s *stubRunner) Run(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
	s.mu.Lock()
	s.calls++
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return s.fn(ctx, req)
}

func (s *stubRunner) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// stubConflictRunner is the claude double for US-11's suggested-resolution
// advisor: it satisfies [Runner] the same way [stubRunner] does, and
// additionally [ConflictAdvisor], so a test can register it under
// [RunnerSet] and drive both call sites the way a real *harness.Runner
// eventually will.
type stubConflictRunner struct {
	*stubRunner
	mu      sync.Mutex
	propose func(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error)
	calls   int
}

func (s *stubConflictRunner) ProposeConflictResolution(ctx context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.propose(ctx, req)
}

func (s *stubConflictRunner) proposeCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// succeeded builds the RunResult a clean run produces. The timestamps are real
// because FinishRun computes the stored duration from them; nothing in
// internal/turn reads a clock of its own.
func succeeded(req harness.Request, home string) *harness.RunResult {
	now := time.Now()
	logPath, _ := naming.RunLogPath(home, req.Ref, req.RunOrdinal)
	return &harness.RunResult{
		Outcome:    harness.OutcomeSucceeded,
		ExitCode:   0,
		SessionID:  "1a17a10a-0000-5000-8000-000000000001",
		LogPath:    logPath,
		Model:      "claude-test",
		Tools:      []string{"Read", "Edit"},
		ResultText: "Added a /health endpoint.",
		CostUSD:    0.4242,
		HasResult:  true,
		Events:     3,
		StartedAt:  now.Add(-2 * time.Second),
		FinishedAt: now,
		Duration:   2 * time.Second,
	}
}

// writeInWorktree puts a file in the run's worktree, which is how a stubbed run
// produces the changes the commit path then has to find.
func writeInWorktree(t *testing.T, req harness.Request, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(req.WorktreePath, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s in the worktree: %v", name, err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedRun writes the rows a previous run of this task would have left: the
// conversation segment carrying the claude session id, and a run on it in the
// status it ended in. It is how a test says "this task has already talked to
// claude", which is the only evidence the resume decision has.
//
// segmentOrdinal is a parameter because a run on a *different* conversation is
// not evidence about this one, and that has to be expressible.
func (f *fixture) seedRun(
	segmentOrdinal int,
	claudeSessionID string,
	commentID int64,
	mode string,
	status store.RunStatus,
) store.Run {
	f.t.Helper()

	segment, err := f.db.GetOrCreateSegment(f.t.Context(), f.session.ID, segmentOrdinal, claudeSessionID, store.AgentClaude)
	if err != nil {
		f.t.Fatalf("seed segment %d: %v", segmentOrdinal, err)
	}
	run, err := f.db.AllocateRun(f.t.Context(), store.AllocateRunParams{
		SessionID:        f.session.ID,
		SegmentID:        segment.ID,
		Mode:             mode,
		Agent:            store.AgentClaude,
		TriggerCommentID: commentID,
		LogPath: func(ordinal int) string {
			path, pathErr := naming.RunLogPath(f.paths.Home(), f.ref(), ordinal)
			if pathErr != nil {
				f.t.Fatalf("derive the seeded log path: %v", pathErr)
			}
			return path
		},
	})
	if err != nil {
		f.t.Fatalf("seed run: %v", err)
	}
	if status == store.RunRunning {
		return run
	}

	exitCode := int64(0)
	finished := store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode}
	if status == store.RunFailed {
		exitCode = int64(store.SignalExitCode)
		finished = store.RunOutcome{Status: store.RunFailed, ExitCode: &exitCode, Error: store.ReasonAbandoned}
	}
	run, err = f.db.FinishRun(f.t.Context(), run.ID, finished)
	if err != nil {
		f.t.Fatalf("seed run outcome: %v", err)
	}
	if status == store.RunReported {
		if run, err = f.db.CompleteRunReport(f.t.Context(), run.ID, commentID+1); err != nil {
			f.t.Fatalf("seed run report: %v", err)
		}
	}
	return run
}

// refused is the RunResult and error a claude that cannot find the conversation
// comes back with: it ran, it emitted its one terminal event, and it exited 1
// without ever reaching an init event.
func refused(req harness.Request, home string) (*harness.RunResult, error) {
	now := time.Now()
	logPath, _ := naming.RunLogPath(home, req.Ref, req.RunOrdinal)
	result := &harness.RunResult{
		Outcome:    harness.OutcomeFailed,
		Reason:     "claude exited with status 1",
		ExitCode:   1,
		SessionID:  req.Session.Ref,
		LogPath:    logPath,
		Stderr:     "No conversation found with session ID: " + req.Session.Ref + "\n",
		HasResult:  true,
		IsError:    true,
		Events:     1,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		Duration:   time.Second,
	}
	return result, fmt.Errorf("%w: no conversation to resume for session id %s (exit 1)",
		harness.ErrSessionNotResumable, req.Session.Ref)
}
