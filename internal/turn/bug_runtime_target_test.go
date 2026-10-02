package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

var bugTargetAuthor = git.Author{Name: "Factory Owner", Email: "owner@example.invalid"}

func TestMergeBugTargetAlreadyAncestorMergesNothing(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 41, "ancestor target")
	commitInSyncDir(t, repo, w.Path(), "repair.txt", "repair work")
	before := gittest.Capture(t, w.Path())
	reason, err := mergeBugTarget(t.Context(), w, repo.Head(), bugTargetAuthor)
	if err != nil || reason != "" {
		t.Fatalf("merge = %q, %v", reason, err)
	}
	if diff := before.Diff(gittest.Capture(t, w.Path())); diff != "" {
		t.Fatalf("worktree changed although main was an ancestor:\n%s", diff)
	}
}

func TestMergeBugTargetAheadCreatesFactoryMerge(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 42, "ahead target")
	commitInSyncDir(t, repo, w.Path(), "repair.txt", "repair work")
	repair := repo.GitIn(w.Path(), "rev-parse", "HEAD")
	advanceSyncBase(t, repo, 2)
	target := repo.Head()
	reason, err := mergeBugTarget(t.Context(), w, target, bugTargetAuthor)
	if err != nil || reason != "" {
		t.Fatalf("merge = %q, %v", reason, err)
	}
	head := repo.GitIn(w.Path(), "rev-parse", "HEAD")
	if parents := repo.GitIn(w.Path(), "rev-list", "--parents", "-n", "1", "HEAD"); parents != head+" "+repair+" "+target {
		t.Fatalf("HEAD parents = %q, want merge of %s and %s", parents, repair, target)
	}
	if author := repo.GitIn(w.Path(), "log", "-1", "--format=%an <%ae>|%cn <%ce>"); author != "Factory Owner <owner@example.invalid>|Factory Owner <owner@example.invalid>" {
		t.Fatalf("merge identity = %q", author)
	}
	if status := repo.GitIn(w.Path(), "status", "--porcelain"); status != "" {
		t.Fatalf("merge left changes: %q", status)
	}
	// The next attempt reads this merge as its source; repeating is a no-op.
	if reason, err = mergeBugTarget(t.Context(), w, target, bugTargetAuthor); err != nil || reason != "" || repo.GitIn(w.Path(), "rev-parse", "HEAD") != head {
		t.Fatalf("repeated merge = %q, %v", reason, err)
	}
}

func TestMergeBugTargetConflictBlocksAndLeavesWorktree(t *testing.T) {
	t.Parallel()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 43, "conflicting target")
	conflictBaseAndBranch(t, repo, w.Path())
	before := gittest.Capture(t, w.Path())
	reason, err := mergeBugTarget(t.Context(), w, repo.Head(), bugTargetAuthor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "conflicts in README.md") || !strings.Contains(reason, repo.Head()) {
		t.Fatalf("blocking reason = %q, want the conflicting path and target", reason)
	}
	if diff := before.Diff(gittest.Capture(t, w.Path())); diff != "" {
		t.Fatalf("conflicting merge changed the worktree:\n%s", diff)
	}
}
