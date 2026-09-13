package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
)

// taskCleanFixture is a registered clone with one task whose worktree exists on
// disk and is recorded on the session row — the only state `task clean` has
// anything to do in.
type taskCleanFixture struct {
	paths    config.Paths
	repo     *gittest.Repo
	session  store.Session
	worktree string
}

const (
	taskCleanIssue  = 123
	taskCleanBranch = "lf/123-add-a-health-endpoint"
)

func newTaskCleanFixture(t *testing.T) *taskCleanFixture {
	t.Helper()

	paths := newRepoHome(t)
	repo := gittest.New(t)
	worktree := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "task-123")

	// A real worktree, made by real git: the removal under test is git's own,
	// and a directory that merely looks like one would not exercise it.
	repo.Git("worktree", "add", "-b", taskCleanBranch, worktree)

	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	}()

	storeRepo, err := db.UpsertRepo(t.Context(), store.Repo{
		Path:          repo.Dir,
		Forge:         store.ForgeGitHub,
		Owner:         "acme",
		Name:          "acme-api",
		RemoteURL:     repo.Origin,
		BaseBranch:    gittest.DefaultBranch,
		FeaturePrefix: "lf",
		WorktreeRoot:  filepath.Dir(worktree),
	})
	if err != nil {
		t.Fatalf("register repository: %v", err)
	}
	session, err := db.GetOrCreateSession(t.Context(), storeRepo, taskCleanIssue)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err = db.FreezeSessionSlug(t.Context(), session.ID, "add-a-health-endpoint", taskCleanBranch); err != nil {
		t.Fatalf("freeze slug: %v", err)
	}
	if err = db.SetSessionWorktree(t.Context(), session.ID, worktree); err != nil {
		t.Fatalf("record the worktree: %v", err)
	}
	session, err = db.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("reload session: %v", err)
	}

	return &taskCleanFixture{paths: paths, repo: repo, session: session, worktree: worktree}
}

// reloadSession reads the session row back, which is where the recorded
// worktree path either survived the refusal or did not.
func (f *taskCleanFixture) reloadSession(t *testing.T) store.Session {
	t.Helper()

	db, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	}()

	session, err := db.GetSession(t.Context(), f.session.ID)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	return session
}

// TestTaskCleanRemovesTheWorktree is the happy half of AC-3.25.
func TestTaskCleanRemovesTheWorktree(t *testing.T) {
	f := newTaskCleanFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if _, err := os.Stat(f.worktree); !os.IsNotExist(err) {
		t.Errorf("worktree %s still exists: %v", f.worktree, err)
	}
	if path := f.reloadSession(t).WorktreePath; path != "" {
		t.Errorf("recorded worktree path = %q, want it cleared", path)
	}
	for _, want := range []string{"removed the worktree", f.worktree, taskCleanBranch} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}
	// The branch is not the worktree. Removing the directory must not remove
	// the work.
	if refs := f.repo.Git("for-each-ref", "--format=%(refname:short)", "refs/heads/"+taskCleanBranch); refs == "" {
		t.Errorf("branch %s was deleted along with the worktree", taskCleanBranch)
	}
}

// TestTaskCleanRefusesADirtyWorktree is the half of AC-3.25 that matters: the
// refusal has to be total. Nothing removed, nothing cleared, and a message that
// says what is in the way.
func TestTaskCleanRefusesADirtyWorktree(t *testing.T) {
	f := newTaskCleanFixture(t)
	if err := os.WriteFile(filepath.Join(f.worktree, "unsaved.txt"), []byte("hours of work\n"), 0o600); err != nil {
		t.Fatalf("dirty the worktree: %v", err)
	}
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s", code, ExitFailure, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "unsaved.txt")); err != nil {
		t.Errorf("the uncommitted file is gone: %v", err)
	}
	if path := f.reloadSession(t).WorktreePath; path != f.worktree {
		t.Errorf("recorded worktree path = %q, want it left at %q", path, f.worktree)
	}
	for _, want := range []string{"uncommitted changes", "nothing was removed", "unsaved.txt", "--force"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not mention %q:\n%s", want, stderr)
		}
	}
}

// TestTaskCleanForceRemovesADirtyWorktree is the escape hatch: the same
// worktree, the same command, one flag, and the user has said they mean it.
func TestTaskCleanForceRemovesADirtyWorktree(t *testing.T) {
	f := newTaskCleanFixture(t)
	if err := os.WriteFile(filepath.Join(f.worktree, "unsaved.txt"), []byte("expendable\n"), 0o600); err != nil {
		t.Fatalf("dirty the worktree: %v", err)
	}
	router, _, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123", "--force"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if _, err := os.Stat(f.worktree); !os.IsNotExist(err) {
		t.Errorf("worktree %s still exists after --force: %v", f.worktree, err)
	}
	if path := f.reloadSession(t).WorktreePath; path != "" {
		t.Errorf("recorded worktree path = %q, want it cleared", path)
	}
}

// TestTaskCleanReportsTheRetentionSetting keeps manual cleanup output aligned
// with active automatic retention (retirement US-003 AC-004).
func TestTaskCleanReportsTheRetentionSetting(t *testing.T) {
	newTaskCleanFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	for _, want := range []string{"worktree_retain_days is 14", "last completed run", "creation", "daemon checks closed issues daily", "task retire"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout does not mention %q:\n%s", want, stdout)
		}
	}
}

// TestTaskCleanWithoutAWorktreeIsANoOp: a task that has never run, or that has
// already been cleaned, is not an error to clean again.
func TestTaskCleanWithoutAWorktreeIsANoOp(t *testing.T) {
	newTaskCleanFixture(t)
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitOK {
		t.Fatalf("first clean: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	stdout.Reset()
	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitOK {
		t.Fatalf("second clean: exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if !strings.Contains(stdout.String(), "no worktree recorded") {
		t.Errorf("stdout does not say there is nothing to remove:\n%s", stdout)
	}
}

// TestTaskCleanClearsThePathWhenTheDirectoryIsGone: a user who deleted the
// worktree by hand must still be able to clear the column, or `task clean`
// becomes a command that can never succeed again.
func TestTaskCleanClearsThePathWhenTheDirectoryIsGone(t *testing.T) {
	f := newTaskCleanFixture(t)
	if err := os.RemoveAll(f.worktree); err != nil {
		t.Fatalf("remove the worktree by hand: %v", err)
	}
	router, stdout, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "123"}); code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, ExitOK, stderr)
	}
	if path := f.reloadSession(t).WorktreePath; path != "" {
		t.Errorf("recorded worktree path = %q, want it cleared", path)
	}
	if !strings.Contains(stdout.String(), "already gone") {
		t.Errorf("stdout does not say the directory was already gone:\n%s", stdout)
	}
}

// TestTaskCleanRejectsAnUnknownIssue keeps the failure legible: never a bare
// store error.
func TestTaskCleanRejectsAnUnknownIssue(t *testing.T) {
	newTaskCleanFixture(t)
	router, _, stderr := newTestRouter(t)

	if code := router.Run(t.Context(), []string{"task", "clean", "999"}); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr.String(), "no task for issue 999") {
		t.Errorf("stderr = %q, want it to name the issue", stderr)
	}
}
