package git_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// commitInDir writes a uniquely named file into dir and commits it there,
// advancing whatever branch is checked out at dir. It runs through
// [gittest.Repo.GitIn] rather than [git.Worktree.Commit] because Commit's
// trailer and identity behaviour belongs to a different story and is not
// under test here — only the resulting commit count matters to Divergence.
func commitInDir(t *testing.T, repo *gittest.Repo, dir, name, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(message+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", message)
}

// advanceBase pushes count new commits to origin's default branch, from the
// registered clone's own working tree — which is exactly how [gittest.New]
// leaves things checked out. Because the clone tracks origin, "git push"
// itself updates the clone's origin/<base> remote-tracking ref, so no
// separate fetch is needed for [git.Worktree.Divergence] to see the new
// commits as Behind.
func advanceBase(t *testing.T, repo *gittest.Repo, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("base-only-%d.txt", i)
		commitInDir(t, repo, repo.Dir, name, fmt.Sprintf("base commit %d", i))
	}
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
}

// advanceBranch commits count new commits directly into the worktree's own
// directory, advancing the task branch without touching origin.
func advanceBranch(t *testing.T, repo *gittest.Repo, dir string, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("branch-only-%d.txt", i)
		commitInDir(t, repo, dir, name, fmt.Sprintf("branch commit %d", i))
	}
}

// TestWorktreeDivergence is AC-1.1: Divergence must match
// "git rev-list --left-right --count" exactly, across every ahead/behind
// combination, and must report the exact ref names it was measured against.
func TestWorktreeDivergence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		baseCommits   int
		branchCommits int
		wantAhead     int
		wantBehind    int
	}{
		{
			name:          "identical branches/zero ahead and zero behind",
			baseCommits:   0,
			branchCommits: 0,
			wantAhead:     0,
			wantBehind:    0,
		},
		{
			name:          "branch has commits base does not/ahead only",
			baseCommits:   0,
			branchCommits: 2,
			wantAhead:     2,
			wantBehind:    0,
		},
		{
			name:          "base has commits branch does not/behind only",
			baseCommits:   3,
			branchCommits: 0,
			wantAhead:     0,
			wantBehind:    3,
		},
		{
			name:          "both sides diverged/ahead and behind both nonzero",
			baseCommits:   2,
			branchCommits: 4,
			wantAhead:     4,
			wantBehind:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			worktree, dir := newTaskWorktree(t, repo, 42, tt.name)
			base := "origin/" + gittest.DefaultBranch

			if tt.baseCommits > 0 {
				advanceBase(t, repo, tt.baseCommits)
			}
			if tt.branchCommits > 0 {
				advanceBranch(t, repo, dir, tt.branchCommits)
			}

			got, err := worktree.Divergence(t.Context(), base)
			if err != nil {
				t.Fatalf("Divergence: %v", err)
			}

			if got.Ahead != tt.wantAhead {
				t.Errorf("Ahead = %d, want %d", got.Ahead, tt.wantAhead)
			}
			if got.Behind != tt.wantBehind {
				t.Errorf("Behind = %d, want %d", got.Behind, tt.wantBehind)
			}
			if got.Base != base {
				t.Errorf("Base = %q, want the ref passed in %q", got.Base, base)
			}
			if got.Branch != worktree.Branch() {
				t.Errorf("Branch = %q, want the worktree's own branch %q", got.Branch, worktree.Branch())
			}
		})
	}
}

// TestWorktreeDivergenceSideOrderRegression is the regression test AC-1.1
// calls for: base and branch are advanced by different, distinguishable
// counts (base ahead by 3, branch ahead by 1) so that a future edit which
// swaps "base...branch" to "branch...base", or which swaps which parsed
// field is assigned to Ahead versus Behind, produces a value this test can
// catch — a symmetric case (equal counts on both sides) could not.
func TestWorktreeDivergenceSideOrderRegression(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Side order regression")
	base := "origin/" + gittest.DefaultBranch

	advanceBase(t, repo, 3)
	advanceBranch(t, repo, dir, 1)

	got, err := worktree.Divergence(t.Context(), base)
	if err != nil {
		t.Fatalf("Divergence: %v", err)
	}

	if got.Ahead != 1 {
		t.Errorf("Ahead = %d, want 1 (the branch's own commit count)", got.Ahead)
	}
	if got.Behind != 3 {
		t.Errorf("Behind = %d, want 3 (base's own commit count)", got.Behind)
	}
}

// TestWorktreeDivergenceMissingBaseRef covers a base that no longer resolves
// — a deleted branch, a renamed default branch. It must be reported as
// [git.ErrRevisionNotFound], the classified condition, rather than a raw
// exit-code error or a panic.
func TestWorktreeDivergenceMissingBaseRef(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, _ := newTaskWorktree(t, repo, 42, "Missing base ref")

	_, err := worktree.Divergence(t.Context(), "origin/does-not-exist")
	if err == nil {
		t.Fatal("Divergence succeeded against a base ref that does not exist")
	}
	if !errors.Is(err, git.ErrRevisionNotFound) {
		t.Fatalf("error = %v, want ErrRevisionNotFound", err)
	}

	var cmdErr *git.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error %v is not a *git.CommandError, so it is not the classified condition", err)
	}
}

// TestWorktreeDivergenceRefusesUnscreenedBaseNames confirms base is screened
// by checkName before Divergence ever shells out: a name git would read as an
// option, or one carrying control characters, is refused with
// [git.ErrInvalidArgument] rather than reaching git at all. If it reached git
// unscreened, "-- rev-list" style values or control characters would produce
// a raw git failure or a hang rather than this immediate, classified refusal.
func TestWorktreeDivergenceRefusesUnscreenedBaseNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
	}{
		{"empty", ""},
		{"looks like an option", "--upload-pack=evil"},
		{"contains a newline", "origin/main\nrm -rf /"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			worktree, _ := newTaskWorktree(t, repo, 42, "Bad base name "+tt.name)

			_, err := worktree.Divergence(t.Context(), tt.base)
			if err == nil {
				t.Fatalf("Divergence succeeded with base %q, want a refusal", tt.base)
			}
			if !errors.Is(err, git.ErrInvalidArgument) {
				t.Errorf("error = %v, want ErrInvalidArgument", err)
			}

			var cmdErr *git.CommandError
			if errors.As(err, &cmdErr) {
				t.Errorf("error %v is a *git.CommandError, meaning git ran before the name was screened", err)
			}
		})
	}
}
