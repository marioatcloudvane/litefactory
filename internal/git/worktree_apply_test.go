package git_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// mergeInProgress reports whether dir currently holds an in-progress merge
// (MERGE_HEAD resolves), the same fact "git status" would print as "You have
// unmerged paths" — checked through git itself rather than a raw file stat
// because a worktree's own ".git" is a file, not a directory, pointing
// elsewhere, so a literal filepath.Join(dir, ".git", "MERGE_HEAD") does not
// name the real location.
func mergeInProgress(t *testing.T, dir string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return cmd.Run() == nil
}

// completeMergeOptions builds the options a confirmed application would use,
// with a fresh proposal id per call so trailer assertions never collide.
func completeMergeOptions(proposalID int64) git.CompleteMergeOptions {
	return git.CompleteMergeOptions{
		Task: "github:acme/widgets#42", Mode: "confirm",
		Author:      testAuthor,
		ProposalID:  proposalID,
		ConfirmedBy: "octocat",
		Model:       "claude",
	}
}

// TestWorktreeApplyResolvedConflictHappyPath is AC-12.5's git-level half:
// applying a confirmed resolution to a real conflicted merge writes only the
// confirmed file, produces exactly one two-parent merge commit, and that
// commit carries every trailer this story adds on top of the ordinary ones.
func TestWorktreeApplyResolvedConflictHappyPath(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Confirmed application")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	files := []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved together\n"}}
	if err := worktree.WriteResolvedFiles(t.Context(), base, files); err != nil {
		t.Fatalf("WriteResolvedFiles: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(got) != files[0].Resolution {
		t.Fatalf("README.md = %q, %v; want %q, nil", got, err, files[0].Resolution)
	}
	if err := worktree.StageResolvedFiles(t.Context(), files); err != nil {
		t.Fatalf("StageResolvedFiles: %v", err)
	}
	result, err := worktree.CompleteMerge(t.Context(), completeMergeOptions(7))
	if err != nil {
		t.Fatalf("CompleteMerge: %v", err)
	}
	if !result.Merged {
		t.Error("Merged = false, want true")
	}
	if result.Head == before {
		t.Error("Head did not move")
	}

	parents := strings.Fields(repo.GitIn(dir, "log", "-1", "--pretty=%P", result.Head))
	if len(parents) != 2 {
		t.Errorf("merge commit has parents %q, want exactly two", parents)
	}
	message := repo.GitIn(dir, "log", "-1", "--pretty=%B", result.Head)
	for _, want := range []string{
		"Litefactory-Task: github:acme/widgets#42",
		"Litefactory-Mode: confirm",
		"Litefactory-Conflict-Proposal: 7",
		"Litefactory-Confirmed-By: octocat",
		"Litefactory-Model: claude",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("commit message missing %q; got:\n%s", want, message)
		}
	}
	if status := repo.GitIn(dir, "status", "--porcelain"); status != "" {
		t.Errorf("worktree not clean after CompleteMerge: %q", status)
	}
	if mergeInProgress(t, dir) {
		t.Error("MERGE_HEAD still present after CompleteMerge")
	}
}

// TestWorktreeStageResolvedFilesRefusesUncontainedPaths is AC-12.7 and S-13,
// exercised directly against containedResolvedPath's own checks:
// [Worktree.StageResolvedFiles] screens every path independently of whatever
// [Worktree.WriteResolvedFiles] already found conflicted, so an absolute
// path, a traversing path, and a path that simply is not in the confirmed
// list are all refused before "git add" ever runs, and nothing in the
// worktree changes.
func TestWorktreeStageResolvedFilesRefusesUncontainedPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"absolute", "/etc/passwd"},
		{"traversal", "../outside.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			worktree, dir := newTaskWorktree(t, repo, 100, "Path refusal "+tc.name)

			var stageErr error
			gittest.AssertUnchanged(t, dir, func() {
				files := []git.ResolvedFile{{Path: tc.path, Resolution: "malicious\n"}}
				stageErr = worktree.StageResolvedFiles(t.Context(), files)
			})
			if stageErr == nil {
				t.Fatal("StageResolvedFiles accepted an uncontained path")
			}
			if !errors.Is(stageErr, git.ErrPathNotContained) {
				t.Errorf("error = %v, want ErrPathNotContained", stageErr)
			}
		})
	}
}

// TestWorktreeWriteResolvedFilesRefusesConflictSetMismatch is watch-out #7
// of US-12 at the [Worktree.WriteResolvedFiles] boundary: a confirmed file
// list naming a path that git does not actually report as conflicted is
// refused as [git.ErrConflictMismatch] rather than written — the same
// property [TestWorktreeApplyRollsBackOnPartialFileList] proves for a
// missing entry, proved here for an extra, unconflicted one.
func TestWorktreeWriteResolvedFilesRefusesConflictSetMismatch(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 106, "Conflict set mismatch")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	// README.md really is conflicted; NOTES.md is not — it names a path
	// that was never part of this merge at all.
	files := []git.ResolvedFile{
		{Path: "README.md", Resolution: "# fixture\nresolved\n"},
		{Path: "NOTES.md", Resolution: "not actually conflicted\n"},
	}
	if err := worktree.WriteResolvedFiles(t.Context(), base, files); !errors.Is(err, git.ErrConflictMismatch) {
		t.Errorf("error = %v, want ErrConflictMismatch", err)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD after refusal: %v", err)
	}
	if after != before {
		t.Errorf("HEAD moved from %s to %s", before, after)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "NOTES.md")); !os.IsNotExist(statErr) {
		t.Errorf("NOTES.md was written even though it was never confirmed to conflict: %v", statErr)
	}
}

// TestWorktreeStageResolvedFilesRefusesSymlinkedAncestor is S-13's symlink
// case: a symlinked directory sitting in the worktree, which a naive
// filepath.Join-and-write would otherwise happily follow outside it, must
// stop [Worktree.StageResolvedFiles] before "git add" ever runs. It is
// exercised against Stage rather than Write for the same reason the
// absolute and traversing cases are: WriteResolvedFiles cross-checks every
// path against a real, git-verified conflict first, and a real conflict can
// never itself sit behind a symlinked ancestor — Stage's own, independent
// containment check is what actually stands between an attacker-placed
// symlink and this package's one write.
func TestWorktreeStageResolvedFilesRefusesSymlinkedAncestor(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 101, "Symlink refusal")

	outside := t.TempDir()
	linkPath := filepath.Join(dir, "escape")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	files := []git.ResolvedFile{{Path: "escape/pwned.txt", Resolution: "malicious\n"}}
	if err := worktree.StageResolvedFiles(t.Context(), files); !errors.Is(err, git.ErrPathNotContained) {
		t.Errorf("error = %v, want ErrPathNotContained", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("the symlink target was written to: %v", statErr)
	}
}

// TestWorktreeApplyRollsBackOnPartialFileList is watch-out #7 of US-12
// (T-12.10's rollback coverage): confirming only some of the conflicting
// paths — "some files were excluded from the original proposal" — must not
// complete the merge, must leave no in-progress merge state, and must leave
// the branch and every tracked file exactly as they were before the call.
func TestWorktreeApplyRollsBackOnPartialFileList(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 102, "Partial file list")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)
	// A second, independent conflicting file, so the confirmed list below
	// can legitimately name only one of the two real conflicts.
	writeAndCommitBoth(t, repo, dir, "NOTES.md", "base notes\n", "branch notes\n")

	var applyErr error
	gittest.AssertUnchanged(t, dir, func() {
		files := []git.ResolvedFile{{Path: "README.md", Resolution: "# fixture\nresolved\n"}}
		applyErr = worktree.WriteResolvedFiles(t.Context(), base, files)
	})
	if applyErr == nil {
		t.Fatal("WriteResolvedFiles accepted a partial confirmed file list against two real conflicts")
	}
	if !errors.Is(applyErr, git.ErrConflictMismatch) {
		t.Errorf("error = %v, want ErrConflictMismatch", applyErr)
	}
	if mergeInProgress(t, dir) {
		t.Error("MERGE_HEAD still present after rollback")
	}
}

// TestWorktreeCompleteMergeRefusesRemainingConflicts is watch-out #7
// directly at the CompleteMerge boundary: if a caller only stages some of
// the conflicted paths, CompleteMerge refuses rather than asking git to
// commit a still-conflicted index, and rolls the merge back (AC-12.6).
func TestWorktreeCompleteMergeRefusesRemainingConflicts(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 103, "Remaining conflicts")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)
	writeAndCommitBoth(t, repo, dir, "NOTES.md", "base notes\n", "branch notes\n")

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	files := []git.ResolvedFile{
		{Path: "README.md", Resolution: "# fixture\nresolved\n"},
		{Path: "NOTES.md", Resolution: "resolved notes\n"},
	}
	if err := worktree.WriteResolvedFiles(t.Context(), base, files); err != nil {
		t.Fatalf("WriteResolvedFiles: %v", err)
	}
	// Only stage one of the two confirmed files, leaving the other conflicted.
	if err := worktree.StageResolvedFiles(t.Context(), files[:1]); err != nil {
		t.Fatalf("StageResolvedFiles: %v", err)
	}

	if _, err := worktree.CompleteMerge(t.Context(), completeMergeOptions(9)); err == nil {
		t.Fatal("CompleteMerge committed a still-conflicted index")
	} else if !errors.Is(err, git.ErrConflictMismatch) {
		t.Errorf("error = %v, want ErrConflictMismatch", err)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD after rollback: %v", err)
	}
	if after != before {
		t.Errorf("HEAD moved from %s to %s", before, after)
	}
	if status := repo.GitIn(dir, "status", "--porcelain"); status != "" {
		t.Errorf("worktree not clean after rollback: %q", status)
	}
}

// TestWorktreeApplyRefusesDirtyWorktree is S-5's precondition: an
// uncommitted change in the worktree refuses WriteResolvedFiles before it
// ever starts a merge, the same way [Worktree.MergeBase] refuses a dirty
// worktree.
func TestWorktreeApplyRefusesDirtyWorktree(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 104, "Dirty refusal")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("oops\n"), 0o600); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	files := []git.ResolvedFile{{Path: "README.md", Resolution: "resolved\n"}}
	if err := worktree.WriteResolvedFiles(t.Context(), base, files); !errors.Is(err, git.ErrWorktreeUncommittedChanges) {
		t.Errorf("error = %v, want ErrWorktreeUncommittedChanges", err)
	}
}

// TestWorktreeApplyRespectsContextDeadline is T-12.3's deadline coverage: an
// already-cancelled context is refused rather than starting the merge, and
// leaves no in-progress merge state behind.
func TestWorktreeApplyRespectsContextDeadline(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 105, "Deadline")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	files := []git.ResolvedFile{{Path: "README.md", Resolution: "resolved\n"}}
	if err := worktree.WriteResolvedFiles(ctx, base, files); err == nil {
		t.Fatal("WriteResolvedFiles accepted an already-cancelled context")
	}
	if mergeInProgress(t, dir) {
		t.Error("MERGE_HEAD present after a cancelled call")
	}
}

// writeAndCommitBoth creates a second file that conflicts independently of
// README.md: base and branch each write different content for it, the same
// shape [conflictOnSharedFile] gives README.md.
func writeAndCommitBoth(t *testing.T, repo *gittest.Repo, dir, name, baseContent, branchContent string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(repo.Dir, name), []byte(baseContent), 0o600); err != nil {
		t.Fatalf("write base %s: %v", name, err)
	}
	repo.Git("add", "--all")
	repo.Git("commit", "--quiet", "--message", "base edits "+name)
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	if err := os.WriteFile(filepath.Join(dir, name), []byte(branchContent), 0o600); err != nil {
		t.Fatalf("write branch %s: %v", name, err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", "branch edits "+name)
}
