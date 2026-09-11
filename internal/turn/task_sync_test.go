package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/harness"
)

// taskOperator advances the remote task independently of the factory checkout.
func taskOperator(t *testing.T, repo *gittest.Repo, branch, start, name, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "operator")
	repo.Git("worktree", "add", "--detach", dir, start)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
		t.Fatal(err)
	}
	commitInSyncDir(t, repo, dir, name, body)
	tip := repo.GitIn(dir, "rev-parse", "HEAD")
	repo.GitIn(dir, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch)
	return tip
}

func TestSyncTaskBranchStates(t *testing.T) {
	for _, scenario := range []string{"absent", "equal", "ahead", "behind", "diverged", "validate", "dirty", "conflict", "unrelated", "reserved", "index-lock"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 42, "task states")
			binary, err := git.Resolve(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			clone, err := binary.Open(t.Context(), repo.Dir)
			if err != nil {
				t.Fatal(err)
			}
			initial := repo.GitIn(w.Path(), "rev-parse", "HEAD")
			remote := ""
			if scenario != "absent" {
				repo.GitIn(w.Path(), "push", "origin", "HEAD:refs/heads/"+w.Branch())
				remote = initial
			}
			if scenario == "ahead" || scenario == "diverged" {
				commitInSyncDir(t, repo, w.Path(), "local.txt", "local work")
			}
			if scenario == "conflict" {
				commitInSyncDir(t, repo, w.Path(), "README.md", "local conflicting work")
			}
			if scenario == "behind" || scenario == "diverged" || scenario == "validate" || scenario == "dirty" || scenario == "index-lock" {
				remote = taskOperator(t, repo, w.Branch(), initial, "operator.txt", "operator work")
			}
			if scenario == "conflict" {
				remote = taskOperator(t, repo, w.Branch(), initial, "README.md", "remote conflicting work")
			}
			if scenario == "reserved" {
				remote = taskOperator(t, repo, w.Branch(), initial, ".litefactory-story-events/payload", "reserved work")
			}
			if scenario == "unrelated" {
				dir := filepath.Join(t.TempDir(), "unrelated")
				repo.Git("worktree", "add", "--detach", dir, initial)
				repo.GitIn(dir, "checkout", "--orphan", "unrelated-task")
				repo.GitIn(dir, "rm", "-rf", ".")
				commitInSyncDir(t, repo, dir, "other.txt", "unrelated")
				remote = repo.GitIn(dir, "rev-parse", "HEAD")
				repo.GitIn(dir, "push", "--force", "origin", "HEAD:refs/heads/"+w.Branch())
			}
			if scenario == "dirty" {
				if err := os.WriteFile(filepath.Join(w.Path(), "unfinished.txt"), []byte("unfinished"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := clone.Fetch(t.Context(), "origin"); err != nil {
				t.Fatal(err)
			}
			if scenario == "index-lock" {
				indexPath := repo.GitIn(w.Path(), "rev-parse", "--git-path", "index")
				if !filepath.IsAbs(indexPath) {
					indexPath = filepath.Join(w.Path(), indexPath)
				}
				if err := os.WriteFile(indexPath+".lock", []byte("another process owns the index"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := repo.GitIn(w.Path(), "rev-parse", "HEAD")
			req := Request{}
			if scenario == "validate" {
				req.Mode = agent.ModeValidate
			}
			blocked := scenario == "dirty" || scenario == "conflict" || scenario == "unrelated" || scenario == "reserved" || scenario == "index-lock"
			var got syncResult
			act := func() { got, err = syncTask(t.Context(), Options{}, req, clone, w, syncTestAuthor, discardLogger()) }
			if blocked {
				gittest.AssertUnchanged(t, w.Path(), act)
			} else {
				act()
			}
			after := repo.GitIn(w.Path(), "rev-parse", "HEAD")
			if blocked {
				if err == nil {
					t.Fatal("obstruction did not refuse")
				}
				if after != before {
					t.Fatal("refusal changed HEAD")
				}
				if scenario == "dirty" {
					b, e := os.ReadFile(filepath.Join(w.Path(), "unfinished.txt"))
					if e != nil || string(b) != "unfinished" {
						t.Fatal("lost unfinished work")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.BranchSHA != before || got.BaseSHA != remote || got.Phase != "task" {
				t.Fatalf("wrong compared inputs: %+v", got)
			}
			switch scenario {
			case "behind":
				if after != remote || got.SyncCommitSHA != remote || got.Behind != 1 || got.Ahead != 0 {
					t.Fatalf("not a precise fast-forward: %+v", got)
				}
			case "diverged":
				parents := strings.Fields(repo.GitIn(w.Path(), "show", "-s", "--format=%P", "HEAD"))
				if !reflect.DeepEqual(parents, []string{before, remote}) {
					t.Fatalf("merge discarded history: %v", parents)
				}
			default:
				if after != before {
					t.Fatalf("unexpected HEAD change: %s -> %s", before, after)
				}
			}
			if scenario == "validate" && (got.Behind != 1 || got.SyncCommitSHA != "") {
				t.Fatalf("Validate did not retain observation: %+v", got)
			}
		})
	}
}

func TestExecuteTaskReconciliationOrdering(t *testing.T) {
	for _, scenario := range []string{"reused", "recreated", "remote-only", "narrow-fetch", "resumed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var f *fixture
			var wantTask, wantBase string
			runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
				if wantTask != "" {
					head := f.repo.GitIn(req.WorktreePath, "rev-parse", "HEAD")
					if f.repo.GitIn(req.WorktreePath, "merge-base", head, wantTask) != wantTask || f.repo.GitIn(req.WorktreePath, "merge-base", head, wantBase) != wantBase {
						t.Error("runner missing task or base commits")
					}
				}
				return succeeded(req, f.paths.Home()), nil
			})
			f = newFixture(t, runner)
			f.opts.AutoSyncBase = true
			dir := seedTaskWorktree(t, f)
			initial := f.repo.GitIn(dir, "rev-parse", "HEAD")
			if scenario == "resumed" {
				r := f.execute(f.request())
				if r.Harness == nil {
					t.Fatalf("initial run: %+v", r)
				}
			}
			wantTask = taskOperator(t, f.repo, testBranch, initial, "operator.txt", "operator fix")
			advanceSyncBase(t, f.repo, 1)
			wantBase = f.repo.Git("rev-parse", "HEAD")
			if scenario == "narrow-fetch" {
				f.repo.Git("config", "--replace-all", "remote.origin.fetch", "+refs/heads/main:refs/remotes/origin/main")
				f.repo.Git("update-ref", "-d", "refs/remotes/origin/"+testBranch)
			}
			if scenario == "recreated" || scenario == "remote-only" {
				f.repo.Git("worktree", "remove", dir)
				if scenario == "remote-only" {
					f.repo.Git("branch", "-D", testBranch)
				}
			}
			req := f.request()
			if scenario == "resumed" {
				req.Trigger.CommentID++
			}
			result := f.execute(req)
			if result.Harness == nil {
				t.Fatalf("runner not reached: %s %s", result.Disposition, result.Reason)
			}
			observations := result.Run.SyncObservations
			if len(observations) != 2 {
				t.Fatalf("want independent task/base observations, got %+v", observations)
			}
			if observations[0].Phase != "task" || observations[0].RemoteSHA != wantTask || observations[1].Phase != "base" || observations[1].RemoteSHA != wantBase || observations[1].LocalSHA != wantTask {
				t.Fatalf("wrong ordering or inputs: %+v", observations)
			}
			runs := f.runs()
			if !reflect.DeepEqual(runs[len(runs)-1].SyncObservations, observations) {
				t.Fatal("persisted observations differ")
			}
			if result.TaskSync == nil || result.Sync == nil || result.TaskSync.RemoteSHA != wantTask || result.Sync.RemoteSHA != wantBase {
				t.Fatalf("report loses phase: task %+v base %+v", result.TaskSync, result.Sync)
			}
		})
	}
}

func TestExecuteTaskObstructionPreservesWork(t *testing.T) {
	for _, scenario := range []string{"dirty", "conflict", "unrelated", "reserved"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
				return nil, errors.New("runner must not start")
			})
			f := newFixture(t, runner)
			f.opts.AutoSyncBase = false
			f.opts.BlockRunOnConflict = false
			dir := seedTaskWorktree(t, f)
			initial := f.repo.GitIn(dir, "rev-parse", "HEAD")
			name := "operator.txt"
			if scenario == "conflict" {
				name = "README.md"
				commitInSyncDir(t, f.repo, dir, name, "local conflict")
			}
			if scenario == "reserved" {
				name = ".litefactory-story-events/payload"
			}
			taskOperator(t, f.repo, testBranch, initial, name, "operator work")
			if scenario == "unrelated" {
				f.repo.GitIn(dir, "checkout", "--orphan", "replacement-local")
				f.repo.GitIn(dir, "rm", "-rf", ".")
				commitInSyncDir(t, f.repo, dir, "different.txt", "unrelated")
				f.repo.GitIn(dir, "branch", "-D", testBranch)
				f.repo.GitIn(dir, "branch", "-m", testBranch)
			}
			if scenario == "dirty" {
				dirtyRetained(t, f, dir)
			}
			before := f.repo.GitIn(dir, "rev-parse", "HEAD")
			var snapshot retainedSnapshot
			if scenario == "dirty" {
				snapshot = captureRetained(t, f, dir)
			}
			result := f.execute(f.request())
			if result.Disposition != DispositionSyncRefused || result.Run.ID != 0 || result.Harness != nil || runner.callCount() != 0 {
				t.Fatalf("ordinary execution not refused: %+v", result)
			}
			if f.repo.GitIn(dir, "rev-parse", "HEAD") != before {
				t.Fatal("local commits changed")
			}
			if scenario == "dirty" {
				assertRetained(t, snapshot, captureRetained(t, f, dir))
			}
			if !strings.Contains(result.SyncDetail, "agent was not started") || !strings.Contains(result.SyncDetail, "origin/"+testBranch) {
				t.Fatalf("missing prelaunch detail: %s", result.SyncDetail)
			}
		})
	}
}

func TestExecuteValidateTaskFreshnessDoesNotChangeSnapshot(t *testing.T) {
	var f *fixture
	var before string
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		if head := f.repo.GitIn(req.WorktreePath, "rev-parse", "HEAD"); head != before {
			t.Fatalf("Validate moved snapshot %s -> %s", before, head)
		}
		v := validationPromptContext(t, req.Prompt)
		r := succeeded(req, f.paths.Home())
		data, err := json.Marshal(validationOutputFor(v))
		if err != nil {
			t.Fatal(err)
		}
		r.ResultText = string(data)
		return r, nil
	})
	f = newFixture(t, runner)
	forge, _ := setupVersionedProgressScope(t, f, 0)
	f.opts.AutoSyncBase = true
	dir := seedTaskWorktree(t, f)
	before = f.repo.GitIn(dir, "rev-parse", "HEAD")
	remote := taskOperator(t, f.repo, testBranch, before, "operator.txt", "remote newer work")
	advanceSyncBase(t, f.repo, 1)
	req := f.request()
	req.Mode = agent.ModeValidate
	req.Command = ParseCommand("/validate assess")
	req.Trigger.Body = "/validate assess"
	result := f.execute(req)
	if !result.Succeeded() {
		t.Fatalf("Validate failed: %s", result.Reason)
	}
	if result.TaskSync == nil || result.TaskSync.RemoteSHA != remote || result.TaskSync.Behind != 1 {
		t.Fatalf("freshness not disclosed: %+v", result.TaskSync)
	}
	if f.repo.GitIn(dir, "rev-parse", "HEAD") != before || result.CommitSHA != "" || forge.children[200].State != "open" {
		t.Fatal("Validate mutated snapshot or acceptance")
	}
}

func TestExecuteEqualTaskKeepsDirtyRecoveryAvailable(t *testing.T) {
	var f *fixture
	var retained retainedSnapshot
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		assertRetained(t, retained, captureRetained(t, f, req.WorktreePath))
		return nil, errors.New("recovery fixture stops without publishing unfinished work")
	})
	f = newFixture(t, runner)
	f.opts.AutoSyncBase = true
	dir := seedTaskWorktree(t, f)
	f.repo.GitIn(dir, "push", "origin", "HEAD:refs/heads/"+testBranch)
	advanceSyncBase(t, f.repo, 1)
	dirtyRetained(t, f, dir)
	retained = captureRetained(t, f, dir)
	result := f.execute(f.request())
	if runner.callCount() != 1 || result.Run.ID == 0 {
		t.Fatalf("equal task blocked existing dirty recovery: %s %s", result.Disposition, result.Reason)
	}
	assertRetained(t, retained, captureRetained(t, f, dir))
}

func TestExecuteTaskFetchErrorDoesNotTrustStaleTrackingRef(t *testing.T) {
	runner := newStubRunner(func(context.Context, harness.Request) (*harness.RunResult, error) {
		return nil, errors.New("must not execute with unknown remote state")
	})
	f := newFixture(t, runner)
	dir := seedTaskWorktree(t, f)
	f.repo.GitIn(dir, "push", "origin", "HEAD:refs/heads/"+testBranch)
	before := f.repo.GitIn(dir, "rev-parse", "HEAD")
	f.repo.Git("remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing-origin"))
	// An unreachable origin is infrastructure, not an answer: the turn refuses
	// with an error and the trigger stays unconsumed for a later tick, rather
	// than being spent on a report (#210 US-001 AC-002).
	result, err := f.executeRefused(f.request())
	if result.Run.ID != 0 || runner.callCount() != 0 {
		t.Fatalf("unknown remote was treated as safe: %+v", result)
	}
	if !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("refusal does not name the fetch: %v", err)
	}
	if f.repo.GitIn(dir, "rev-parse", "HEAD") != before {
		t.Fatal("fetch refusal changed retained branch")
	}
}
