package turn

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

// Capture the raw index, not just its staged diff: status inspection must not
// refresh index metadata while deciding to preserve retained work.
type retainedSnapshot struct {
	head, index string
	files       map[string]string
	modes       map[string]os.FileMode
}

func captureRetained(t *testing.T, f *fixture, dir string) retainedSnapshot {
	t.Helper()
	indexPath := f.repo.GitIn(dir, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(dir, indexPath)
	}
	b, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	s := retainedSnapshot{head: f.repo.GitIn(dir, "rev-parse", "HEAD"), index: string(b), files: map[string]string{}, modes: map[string]os.FileMode{}}
	for _, name := range []string{"README.md", "retained.txt"} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		s.files[name] = string(b)
		s.modes[name] = info.Mode()
	}
	return s
}
func assertRetained(t *testing.T, want, got retainedSnapshot) {
	t.Helper()
	if want.head != got.head {
		t.Error("retained HEAD changed")
	}
	if !bytes.Equal([]byte(want.index), []byte(got.index)) {
		t.Error("retained raw index changed")
	}
	for _, name := range []string{"README.md", "retained.txt"} {
		b := want.files[name]
		if got.files[name] != b || got.modes[name] != want.modes[name] {
			t.Errorf("retained bytes/mode changed: %s", name)
		}
	}
}
func dirtyRetained(t *testing.T, f *fixture, dir string) {
	t.Helper()
	p := filepath.Join(dir, "README.md")
	if err := os.WriteFile(p, []byte("staged retained\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.repo.GitIn(dir, "add", "README.md")
	if err := os.WriteFile(p, []byte("unstaged retained\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("untracked retained\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteDeferredDirtyRetryLifecycle(t *testing.T) {
	for _, kind := range []agent.Kind{store.AgentClaude, store.AgentCodex} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			var f *fixture
			var snapshot retainedSnapshot
			phase := -1
			sawFallback := false
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				if phase > 0 && phase < 5 {
					assertRetained(t, snapshot, captureRetained(t, f, req.WorktreePath))
					for _, text := range []string{"preserved uncommitted changes", "2 commit(s) behind and 0 ahead", "The base was not merged"} {
						if !strings.Contains(req.Prompt, text) {
							t.Errorf("agent prompt lacks %q: %s", text, req.Prompt)
						}
					}
				}
				if phase == 2 && req.Path == agent.PathResume {
					return nil, agent.ErrSessionNotResumable
				}
				if phase == 2 {
					if req.Path != agent.PathColdAfterResumeRefused {
						t.Errorf("want fallback path, got %s", req.Path)
					}
					sawFallback = true
				}
				r := succeeded(req, f.paths.Home())
				r.ResumeToken = "retained-thread"
				switch phase {
				case 0:
					dirtyRetained(t, f, req.WorktreePath)
					fallthrough
				case 1:
					r.Outcome = harness.OutcomeFailed
					r.ExitCode = 2
					r.Reason = "failed"
				case 2:
					r.Outcome = harness.OutcomeTimedOut
					r.Reason = "timed out"
				case 3:
					r.Outcome = harness.OutcomeAbandoned
					r.Reason = "abandoned"
				}
				return r, nil
			})
			f = newFixture(t, runner)
			f.opts.Runners = RunnerSet{kind: runner}
			f.opts.AutoSyncBase = true
			req := f.request()
			req.Agent = kind
			// Establish a real successful conversation before a later failed run.
			// Codex only persists its resume token after success.
			if setup := f.execute(req); !setup.Succeeded() {
				t.Fatalf("conversation setup failed: %+v", setup)
			}
			req.Trigger.CommentID++
			phase = 0
			first := f.execute(req)
			if first.Committed || first.Succeeded() {
				t.Fatalf("failed output saved: %+v", first)
			}
			snapshot = captureRetained(t, f, f.worktreeDir())
			advanceSyncBase(t, f.repo, 2)
			for phase = 1; phase <= 5; phase++ {
				req.Trigger.CommentID++
				result := f.execute(req)
				if phase <= 3 {
					if result.Committed || result.CommitSHA != "" {
						t.Fatalf("failed retry committed: %+v", result)
					}
					assertRetained(t, snapshot, captureRetained(t, f, f.worktreeDir()))
				}
				if phase == 4 && (!result.Succeeded() || !result.Committed) {
					t.Fatalf("recovery save failed: %+v logs %s", result, f.logs)
				}
				runs := f.runs()
				row := runs[len(runs)-1]
				if phase < 5 {
					if row.SyncOutcome != store.SyncDeferredDirty || row.SyncBehind != 2 || row.SyncAhead != 0 || row.SyncCommitSHA != "" {
						t.Errorf("deferred persistence: %+v", row)
					}
				} else if row.SyncOutcome != store.SyncSynced || row.SyncCommitSHA == "" {
					t.Errorf("clean retry did not sync: %+v", row)
				}
			}
			if !sawFallback {
				t.Error("fallback was never exercised")
			}
		})
	}
}

func TestSyncBaseDirtyPolicyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		behind  int
		enabled bool
		cap     int
		want    syncOutcome
	}{{"up to date", 0, true, 0, syncOutcomeUpToDate}, {"disabled", 1, false, 0, syncOutcomeSkipped}, {"over cap", 2, true, 1, syncOutcomeTooFarBehind}, {"eligible", 1, true, 0, syncOutcomeDeferredDirty}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 42, "dirty policy")
			if tc.behind > 0 {
				advanceSyncBase(t, repo, tc.behind)
			}
			if err := os.WriteFile(filepath.Join(w.Path(), "retained.txt"), []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := syncBase(t.Context(), Options{AutoSyncBase: tc.enabled, AutoSyncMaxBehind: tc.cap}, Request{}, w, store.Repo{BaseBranch: gittest.DefaultBranch}, syncTestAuthor, discardLogger())
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != tc.want {
				t.Errorf("outcome %s want %s", result.Outcome, tc.want)
			}
		})
	}
}

func TestSyncBaseStatusErrorRetainsCause(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 42, "broken index")
	advanceSyncBase(t, repo, 1)
	indexPath := repo.GitIn(w.Path(), "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(w.Path(), indexPath)
	}
	if err := os.WriteFile(indexPath, []byte("invalid index"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := syncBase(t.Context(), Options{AutoSyncBase: true}, Request{}, w, store.Repo{BaseBranch: gittest.DefaultBranch}, syncTestAuthor, discardLogger())
	var cause *git.CommandError
	if !errors.As(err, &cause) || !strings.Contains(err.Error(), "status") {
		t.Fatalf("want wrapped status command error, got %v", err)
	}
}

func TestReporterDisclosesDeferredDirtyWithoutSuccessClaim(t *testing.T) {
	t.Parallel()
	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		r := succeeded(req, f.paths.Home())
		r.Outcome = harness.OutcomeFailed
		r.ExitCode = 2
		r.Reason = "failed"
		return r, nil
	})
	f = newFixture(t, runner)
	f.opts.AutoSyncBase = true
	dir := seedTaskWorktree(t, f)
	dirtyRetained(t, f, dir)
	advanceSyncBase(t, f.repo, 2)
	result := f.execute(f.request())
	if result.Succeeded() {
		t.Fatal("failed run classified succeeded")
	}
	forge := newFakeForge(t)
	reporter := newReporter(t, f, forge, nil)
	if _, err := reporter.ReportTurn(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	body := forge.only(t).body
	for _, want := range []string{"deferred syncing", "preserve uncommitted changes", "2 commit(s) behind, 0 ahead", "The base was not merged"} {
		if !strings.Contains(body, want) {
			t.Errorf("report lacks %q: %s", want, body)
		}
	}
	// The existing failure lead names where unfinished work lives; the sync
	// disclosure must not repeat that local path.
	if strings.Count(body, dir) > 1 {
		t.Error("sync disclosure repeats the worktree path")
	}
}

func TestExecuteStatusInspectionErrorDoesNotStartAgent(t *testing.T) {
	t.Parallel()
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		t.Error("agent launched against unknown status")
		return nil, errors.New("unexpected agent")
	})
	f := newFixture(t, runner)
	f.opts.AutoSyncBase = true
	dir := seedTaskWorktree(t, f)
	advanceSyncBase(t, f.repo, 1)
	indexPath := f.repo.GitIn(dir, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(dir, indexPath)
	}
	if err := os.WriteFile(indexPath, []byte("invalid index"), 0600); err != nil {
		t.Fatal(err)
	}
	result := f.execute(f.request())
	var cause *git.CommandError
	if !errors.As(result.Err, &cause) {
		t.Fatalf("inspection cause was lost: %+v", result)
	}
	if runner.callCount() != 0 {
		t.Fatal("agent started despite inspection error")
	}
}
