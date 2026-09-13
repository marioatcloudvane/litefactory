package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type retirementExecFixture struct {
	f           *fixture
	repo        *gittest.Repo
	session     store.Session
	clone       *git.Clone
	path        string
	now         time.Time
	options     daemon.RetirementExecutionOptions
	calls       atomic.Int32
	executionDB *store.DB
	onRead      func(int) (string, int)
}

func newRetirementExecFixture(t *testing.T) *retirementExecFixture {
	t.Helper()
	x := &retirementExecFixture{f: newFixture(t), repo: gittest.New(t)}
	root := filepath.Join(filepath.Dir(x.repo.Dir), "tasks")
	x.path = filepath.Join(root, "task-213")
	r, err := x.f.db.UpsertRepo(t.Context(), store.Repo{Path: x.repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: x.repo.Origin, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	s, err := x.f.db.GetOrCreateSession(t.Context(), r, 213)
	if err != nil {
		t.Fatal(err)
	}
	x.session, err = x.f.db.FreezeSessionSlug(t.Context(), s.ID, "retire", "lf/213-retire")
	if err != nil {
		t.Fatal(err)
	}
	if err = x.f.db.SetSessionWorktree(t.Context(), s.ID, x.path); err != nil {
		t.Fatal(err)
	}
	x.session.WorktreePath = x.path
	x.now = x.session.CreatedAt.Add(31 * 24 * time.Hour)
	x.repo.Git("worktree", "add", "-b", x.session.Branch, x.path, "main")
	x.repo.Git("push", "origin", x.session.Branch)
	b, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	x.clone, err = b.Open(t.Context(), x.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(x.calls.Add(1))
		if r.URL.Path != "/repos/acme/api/issues/213" {
			t.Errorf("wrong closure URL %s", r.URL)
		}
		if r.Header.Get("If-None-Match") != "" {
			t.Error("conditional closure read")
		}
		state, status := "closed", 200
		if x.onRead != nil {
			state, status = x.onRead(n)
		}
		w.Header().Set("ETag", `"closed-v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"id":2130,"number":213,"state":%q,"state_reason":"not_planned","url":"https://api.github.com/repos/acme/api/issues/213"}`, state)
	}))
	t.Cleanup(server.Close)
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("fixture"), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	x.options = daemon.RetirementExecutionOptions{Issues: client, CloneGate: make(lifecycleCloneGate, 1), LockDir: x.f.paths.StoryProgressLocksDir(), RetainDays: 14}
	return x
}
func (x *retirementExecFixture) daemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	db := x.f.db
	if x.executionDB != nil {
		db = x.executionDB
	}
	d, err := daemon.New(daemon.Options{DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): fakeModeClassifier{}}, ToolProfiles: config.DefaultToolProfiles().Permissions, DB: db, Forge: x.f.client, Filter: x.f.filter, Turn: x.f.turns, Reporter: x.f.reporter, InstanceID: "retirement-test", Logger: x.f.logger, Retirement: x.options})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func (x *retirementExecFixture) execute(t *testing.T) (daemon.RetirementEvaluation, error) {
	t.Helper()
	var result daemon.RetirementEvaluation
	var err error
	gittest.AssertUnchanged(t, x.repo.Dir, func() { result, err = x.daemon(t).ExecuteRetirement(t.Context(), "scratch-sweep", x.session.ID, x.now) })
	return result, err
}
func retirementAction(t *testing.T, e daemon.RetirementEvaluation, kind, want string) {
	t.Helper()
	for _, d := range e.Decisions {
		if d.Kind == kind {
			if d.Action != want {
				t.Fatalf("%s=%+v, want %s; error %v", kind, d, want, e.Error)
			}
			return
		}
	}
	t.Fatalf("missing %s in %+v", kind, e)
}

// AC-001/002/005/006: full executor uses real migrated SQLite, production Git
// and HTTP client. All fixture resources and remote effects are temporary.
func TestRetirementExecutionRealLifecycle(t *testing.T) {
	for _, scenario := range []string{"merged_external", "abandoned_unmerged", "open_merged", "reopened_final", "unavailable_final", "ignored", "untracked", "unpushed", "missing_upstream", "missing_registration", "stale_registration", "remote_only", "owner_locked", "active", "stale_intent"} {
		t.Run(scenario, func(t *testing.T) {
			x := newRetirementExecFixture(t)
			work, local, remote := "removed", "removed", "removed"
			switch scenario {
			case "abandoned_unmerged", "unpushed", "missing_upstream":
				if err := os.WriteFile(filepath.Join(x.path, "unmerged.txt"), []byte("valuable original history\n"), 0600); err != nil {
					t.Fatal(err)
				}
				x.repo.GitIn(x.path, "add", ".")
				x.repo.GitIn(x.path, "commit", "-m", "unmerged")
				if scenario != "unpushed" {
					x.repo.GitIn(x.path, "push", "origin", x.session.Branch)
				}
				if scenario == "abandoned_unmerged" {
					local, remote = "kept", "kept"
				} else {
					work, local, remote = "kept", "kept", "kept"
				}
				if scenario == "missing_upstream" {
					x.repo.Git("push", "origin", "--delete", x.session.Branch)
				}
			case "open_merged":
				x.onRead = func(int) (string, int) { return "open", 200 }
				work, local, remote = "kept", "kept", "kept"
			case "reopened_final", "unavailable_final":
				x.onRead = func(n int) (string, int) {
					if n >= 3 {
						if scenario == "unavailable_final" {
							return "", 404
						}
						return "open", 200
					}
					return "closed", 200
				}
				work, local, remote = "kept", "kept", "kept"
				if scenario == "unavailable_final" {
					work = "unknown"
				}
			case "ignored", "untracked":
				if scenario == "ignored" {
					if err := os.WriteFile(filepath.Join(x.repo.Dir, ".git", "info", "exclude"), []byte("private.txt\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(x.path, "private.txt"), []byte("only copy\n"), 0600); err != nil {
					t.Fatal(err)
				}
				work, local, remote = "kept", "kept", "kept"
			case "missing_registration":
				x.repo.Git("worktree", "remove", x.path)
				work = "already_absent"
			case "stale_registration":
				if err := os.RemoveAll(x.path); err != nil {
					t.Fatal(err)
				}
				work = "already_absent"
			case "remote_only":
				x.repo.Git("worktree", "remove", x.path)
				x.repo.Git("branch", "-D", x.session.Branch)
				work, local = "already_absent", "already_absent"
			case "owner_locked":
				ok, err := x.f.db.AcquireSessionLock(t.Context(), x.session.ID, "other")
				if err != nil || !ok {
					t.Fatalf("lock %v %v", ok, err)
				}
				work, local, remote = "kept", "kept", "kept"
			case "active":
				if err := x.f.db.SetSessionState(t.Context(), x.session.ID, store.SessionRunning); err != nil {
					t.Fatal(err)
				}
				work, local, remote = "kept", "kept", "kept"
			case "stale_intent":
				_, err := x.f.db.RecordRetirementIntent(t.Context(), store.RetirementIntent{SessionID: x.session.ID, SweepID: "crashed", Kind: "worktree", Resource: x.path, ExpectedSHA: "old-replaced-checkout", Action: "remove", ObservedAt: x.now.Add(-time.Hour)})
				if err != nil {
					t.Fatal(err)
				}
				work, local = "kept", "kept"
			}
			x.repo.Dirty()
			e, err := x.execute(t)
			if scenario == "unavailable_final" {
				if err == nil {
					t.Fatal("expected unavailable lookup failure")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			retirementAction(t, e, "worktree", work)
			retirementAction(t, e, "local_branch", local)
			retirementAction(t, e, "remote_branch", remote)
			if x.calls.Load() > 7 {
				t.Fatalf("unbounded issue lookups %d", x.calls.Load())
			}
			s, err := x.f.db.GetSession(t.Context(), x.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (work == "removed" || work == "already_absent") && s.WorktreePath != "" {
				t.Fatal("confirmed path not cleared")
			}
			if work == "kept" && s.WorktreePath != x.path {
				t.Fatal("kept path cleared")
			}
			if s.Branch != x.session.Branch || s.FeatureSlug != x.session.FeatureSlug {
				t.Fatal("retirement erased identity")
			}
			if scenario == "merged_external" || scenario == "abandoned_unmerged" || scenario == "remote_only" {
				// Same setup capability used by the next authorized run: surviving local
				// branch for abandonment, or fresh remote base after full retirement.
				got, err := git.EnsureWorktree(t.Context(), x.clone, git.EnsureWorktreeOptions{Dir: x.path, Branch: x.session.Branch, BaseBranch: "main"})
				if err != nil {
					t.Fatal(err)
				}
				if got.Worktree == nil {
					t.Fatal("reopen did not recreate worktree")
				}
			}
		})
	}
}

// The real Git boundary can lose a response after it performed an effect. The
// wrapper injects that loss only; underlying observation/mutation is production.
type retirementUncertainGit struct {
	clone        *git.Clone
	path, branch string
	failConfirm  bool
	afterRemove  func()
}

func (g *retirementUncertainGit) Observe(ctx context.Context) (git.RetirementObservation, error) {
	return git.ObserveRetirement(ctx, g.clone, g.path, g.branch, "main")
}
func (g *retirementUncertainGit) Remove(ctx context.Context, o git.RetirementObservation, kind string) error {
	err := git.GuardedRetirement(ctx, g.clone, o, "main", kind)
	if err == nil && g.afterRemove != nil {
		g.afterRemove()
	}
	return err
}
func (g *retirementUncertainGit) Absent(ctx context.Context, kind string) (bool, error) {
	if g.failConfirm {
		return false, errors.New("lost confirmation")
	}
	return git.RetirementAbsent(ctx, g.clone, g.path, g.branch, kind)
}

func TestRetirementExecutionRealUncertainRetry(t *testing.T) {
	x := newRetirementExecFixture(t)
	ops := &retirementUncertainGit{clone: x.clone, path: x.path, branch: x.session.Branch, failConfirm: true}
	x.options.OpenGit = func(context.Context, store.RetirementOwner) (daemon.RetirementGit, error) { return ops, nil }
	e, err := x.execute(t)
	if err == nil {
		t.Fatal("lost confirmation claimed success")
	}
	retirementAction(t, e, "worktree", "unknown")
	pending, err := x.f.db.PendingRetirementIntents(t.Context(), x.session.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	s, err := x.f.db.GetSession(t.Context(), x.session.ID)
	if err != nil || s.WorktreePath != x.path {
		t.Fatalf("uncertain ownership lost: %+v %v", s, err)
	}
	ops.failConfirm = false
	e, err = x.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	retirementAction(t, e, "worktree", "already_absent")
	pending, err = x.f.db.PendingRetirementIntents(t.Context(), x.session.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
}

func TestRetirementExecutionRealStorageFailures(t *testing.T) {
	for _, phase := range []string{"intent", "outcome"} {
		t.Run(phase, func(t *testing.T) {
			x := newRetirementExecFixture(t)
			db, err := store.Open(t.Context(), x.f.paths.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			x.executionDB = db
			closed := false
			closeDB := func() {
				if !closed {
					if err := db.Close(); err != nil {
						t.Error(err)
					}
					closed = true
				}
			}
			t.Cleanup(closeDB)
			if phase == "intent" {
				x.onRead = func(n int) (string, int) {
					if n == 3 {
						closeDB()
					}
					return "closed", 200
				}
			} else {
				ops := &retirementUncertainGit{clone: x.clone, path: x.path, branch: x.session.Branch, afterRemove: closeDB}
				x.options.OpenGit = func(context.Context, store.RetirementOwner) (daemon.RetirementGit, error) { return ops, nil }
			}
			e, err := x.execute(t)
			if err == nil {
				t.Fatal("storage failure reported success")
			}
			want := "failed"
			if phase == "outcome" {
				want = "unknown"
			}
			retirementAction(t, e, "worktree", want)
			s, err := x.f.db.GetSession(t.Context(), x.session.ID)
			if err != nil || s.WorktreePath != x.path {
				t.Fatalf("ownership lost: %+v %v", s, err)
			}
			pending, err := x.f.db.PendingRetirementIntents(t.Context(), x.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if phase == "intent" {
				if len(pending) != 0 {
					t.Fatalf("unexpected intent %+v", pending)
				}
				if _, err := os.Stat(x.path); err != nil {
					t.Fatalf("intent failure allowed deletion: %v", err)
				}
			} else {
				if len(pending) != 1 {
					t.Fatalf("lost uncertain intent %+v", pending)
				}
				if _, err := os.Stat(x.path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("effect not exercised: %v", err)
				}
			}
		})
	}
}

type retirementNotifyingGate struct {
	inner   turn.CloneGate
	entered chan struct{}
}

func (g retirementNotifyingGate) Acquire(ctx context.Context, path string) (func(), error) {
	close(g.entered)
	return g.inner.Acquire(ctx, path)
}

func TestRetirementExecutionRealCoordination(t *testing.T) {
	t.Run("parent_busy", func(t *testing.T) {
		x := newRetirementExecFixture(t)
		release, err := x.f.db.LockStoryProgress(t.Context(), x.session.TaskRef, x.options.LockDir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := release(); err != nil {
				t.Error(err)
			}
		}()
		e, err := x.execute(t)
		if err != nil {
			t.Fatal(err)
		}
		retirementAction(t, e, "worktree", "kept")
		if x.calls.Load() != 0 {
			t.Fatal("busy parent reached external reads")
		}
	})
	t.Run("clone_busy_cancellation", func(t *testing.T) {
		x := newRetirementExecFixture(t)
		gate := make(lifecycleCloneGate, 1)
		release, err := gate.Acquire(t.Context(), x.repo.Dir)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		entered := make(chan struct{})
		x.options.CloneGate = retirementNotifyingGate{inner: gate, entered: entered}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		d := x.daemon(t)
		go func() { _, err := d.ExecuteRetirement(ctx, "contention", x.session.ID, x.now); done <- err }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("executor never reached clone gate")
		}
		s, err := x.f.db.GetSession(t.Context(), x.session.ID)
		if err != nil || s.LockOwner != "retirement-test" {
			t.Fatalf("clone acquired before canonical owner: %+v %v", s, err)
		}
		if x.calls.Load() != 0 {
			t.Fatal("blocked clone reached external reads")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("clone wait ignored cancellation")
		}
		s, err = x.f.db.GetSession(t.Context(), x.session.ID)
		if err != nil || s.LockOwner != "" {
			t.Fatalf("owner lock leaked %+v %v", s, err)
		}
	})
}

func TestRetirementExecutionRealSharedChild(t *testing.T) {
	x := newRetirementExecFixture(t)
	if err := x.f.db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: x.session.TaskRef, Generation: 1, State: "planning"}); err != nil {
		t.Fatal(err)
	}
	if err := x.f.db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: x.session.TaskRef, Generation: 1, Key: "US-001", IssueNumber: 255, IssueID: 2550, BodyHash: "frozen", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	repo, err := x.f.db.GetRepo(t.Context(), x.session.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	child, err := x.f.db.GetOrCreateSession(t.Context(), repo, 255)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.f.db.FreezeSessionSlug(t.Context(), child.ID, "retire", x.session.Branch); err != nil {
		t.Fatal(err)
	}
	if err := x.f.db.SetSessionWorktree(t.Context(), child.ID, x.path); err != nil {
		t.Fatal(err)
	}
	d := x.daemon(t)
	e, err := d.ExecuteRetirement(t.Context(), "shared", child.ID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	retirementAction(t, e, "worktree", "kept")
	if err := x.f.db.SetSessionState(t.Context(), child.ID, store.SessionRunning); err != nil {
		t.Fatal(err)
	}
	e, err = x.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	retirementAction(t, e, "worktree", "kept")
	if x.calls.Load() != 0 {
		t.Fatal("shared child or active family reached closure reads")
	}
}

func TestRetirementExecutionRealRecreatedCheckout(t *testing.T) {
	x := newRetirementExecFixture(t)
	ops := &retirementUncertainGit{clone: x.clone, path: x.path, branch: x.session.Branch, failConfirm: true}
	x.options.OpenGit = func(context.Context, store.RetirementOwner) (daemon.RetirementGit, error) { return ops, nil }
	if _, err := x.execute(t); err == nil {
		t.Fatal("missing confirmation should fail")
	}
	if _, err := git.EnsureWorktree(t.Context(), x.clone, git.EnsureWorktreeOptions{Dir: x.path, Branch: x.session.Branch, BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	ops.failConfirm = false
	e, err := x.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	retirementAction(t, e, "worktree", "kept")
	if _, err := os.Stat(x.path); err != nil {
		t.Fatalf("old intent deleted recreated checkout: %v", err)
	}
}

func TestRetirementExecutionRealRemoteReopen(t *testing.T) {
	x := newRetirementExecFixture(t)
	if err := os.WriteFile(filepath.Join(x.path, "original.txt"), []byte("abandoned history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	x.repo.GitIn(x.path, "add", ".")
	x.repo.GitIn(x.path, "commit", "-m", "abandoned")
	x.repo.GitIn(x.path, "push", "origin", x.session.Branch)
	tip := x.repo.GitIn(x.path, "rev-parse", "HEAD")
	e, err := x.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	retirementAction(t, e, "worktree", "removed")
	retirementAction(t, e, "remote_branch", "kept")
	x.repo.Git("branch", "-D", x.session.Branch)
	got, err := git.EnsureWorktree(t.Context(), x.clone, git.EnsureWorktreeOptions{Dir: x.path, Branch: x.session.Branch, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.BranchCreated || x.repo.GitIn(x.path, "rev-parse", "HEAD") != tip {
		t.Fatal("remote reopening lost original branch history")
	}
}

func TestRetirementExecutionRealIntentPathIsolation(t *testing.T) {
	x := newRetirementExecFixture(t)
	id, err := x.f.db.RecordRetirementIntent(t.Context(), store.RetirementIntent{SessionID: x.session.ID, SweepID: "old", Kind: "worktree", Resource: x.path, ExpectedSHA: x.repo.Head(), Action: "remove", ObservedAt: x.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := x.f.db.RecordRetirementOutcome(t.Context(), id, "unknown", "unconfirmed", true); err == nil {
		t.Fatal("uncertain effect cleared path")
	}
	replacement := filepath.Join(filepath.Dir(x.path), "replacement")
	if err := x.f.db.SetSessionWorktree(t.Context(), x.session.ID, replacement); err != nil {
		t.Fatal(err)
	}
	if err := x.f.db.RecordRetirementOutcome(t.Context(), id, "removed", "confirmed_old_absence", true); err != nil {
		t.Fatal(err)
	}
	s, err := x.f.db.GetSession(t.Context(), x.session.ID)
	if err != nil || s.WorktreePath != replacement {
		t.Fatalf("old intent cleared replacement ownership %+v %v", s, err)
	}
}
