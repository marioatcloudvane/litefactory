package git_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// conflictOnSharedFile makes base and branch each edit README.md differently
// from the same starting content — base directly in the registered clone
// (then pushed), branch directly in dir — so the two edits land on the same
// line and neither [git.Worktree.MergePreview] nor [git.Worktree.MergeBase]
// can reconcile them.
func conflictOnSharedFile(t *testing.T, repo *gittest.Repo, dir string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(repo.Dir, "README.md"), []byte("# fixture\nbase change\n"), 0o600); err != nil {
		t.Fatalf("write base README.md: %v", err)
	}
	repo.Git("add", "--all")
	repo.Git("commit", "--quiet", "--message", "base edits README.md")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\nbranch change\n"), 0o600); err != nil {
		t.Fatalf("write branch README.md: %v", err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", "branch edits README.md")
}

// TestWorktreeMergePreviewClean is AC-2.1's dry-run half: a branch behind by
// commits that touch nothing the task branch also touches previews clean and
// reports the tree merge-tree computed. gittest.AssertUnchanged is what earns
// the "preview" name in the doc comment (AC-2.2): the ref, the branch and the
// worktree must be exactly as MergePreview found them once it returns.
func TestWorktreeMergePreviewClean(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Clean preview")
	base := "origin/" + gittest.DefaultBranch
	advanceBase(t, repo, 3)
	advanceBranch(t, repo, dir, 1)

	var preview git.MergePreview
	var err error
	gittest.AssertUnchanged(t, dir, func() {
		preview, err = worktree.MergePreview(t.Context(), base)
	})
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}

	if !preview.Clean {
		t.Fatalf("Clean = false, want true; conflicts = %v", preview.Conflicts)
	}
	if len(preview.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want empty on a clean preview", preview.Conflicts)
	}
	if preview.TreeSHA == "" {
		t.Error("TreeSHA is empty, want the tree id merge-tree computed")
	}
}

// TestWorktreeMergePreviewConflict is AC-2.2: an unclean preview reports the
// exact conflicting paths and, exactly as the clean case, leaves the ref, the
// branch and the worktree untouched.
func TestWorktreeMergePreviewConflict(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Conflicted preview")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	var preview git.MergePreview
	var err error
	gittest.AssertUnchanged(t, dir, func() {
		preview, err = worktree.MergePreview(t.Context(), base)
	})
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}

	if preview.Clean {
		t.Fatal("Clean = true, want false: base and branch both edited README.md")
	}
	if !slices.Contains(preview.Conflicts, "README.md") {
		t.Errorf("Conflicts = %v, want to contain %q", preview.Conflicts, "README.md")
	}
	if preview.TreeSHA == "" {
		t.Error("TreeSHA is empty, want the conflicted tree id merge-tree still computed")
	}
}

// conflictOnSharedNonASCIIFile is conflictOnSharedFile's counterpart with a
// non-ASCII filename: base and branch each edit the same UTF-8-named file
// differently, so a real conflicted "git merge-tree" run is forced to report
// a path core.quotePath would otherwise C-quote and escape.
func conflictOnSharedNonASCIIFile(t *testing.T, repo *gittest.Repo, dir string) {
	t.Helper()

	const path = "café.md"
	if err := os.WriteFile(filepath.Join(repo.Dir, path), []byte("# fixture\nbase change\n"), 0o600); err != nil {
		t.Fatalf("write base %s: %v", path, err)
	}
	repo.Git("add", "--all")
	repo.Git("commit", "--quiet", "--message", "base edits "+path)
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	if err := os.WriteFile(filepath.Join(dir, path), []byte("# fixture\nbranch change\n"), 0o600); err != nil {
		t.Fatalf("write branch %s: %v", path, err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", "branch edits "+path)
}

// TestWorktreeMergePreviewConflictNonASCIIPath is CR-B-001: a conflicting
// path carrying a non-ASCII byte must come back exactly as the literal path
// ("café.md"), not core.quotePath's C-quoted escape ("caf\303\251.md").
// Without "-z" on the merge-tree invocation this is exactly what happens —
// confirmed empirically against a real conflicted merge before this fix
// landed — and TestWorktreeMergePreviewConflict alone never caught it because
// its one conflicting filename ("README.md") is plain ASCII.
func TestWorktreeMergePreviewConflictNonASCIIPath(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Conflicted preview, non-ASCII path")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedNonASCIIFile(t, repo, dir)

	var preview git.MergePreview
	var err error
	gittest.AssertUnchanged(t, dir, func() {
		preview, err = worktree.MergePreview(t.Context(), base)
	})
	if err != nil {
		t.Fatalf("MergePreview: %v", err)
	}

	if preview.Clean {
		t.Fatal("Clean = true, want false: base and branch both edited café.md")
	}
	if !slices.Contains(preview.Conflicts, "café.md") {
		t.Errorf("Conflicts = %v, want to contain the literal path %q, not a C-quoted escape", preview.Conflicts, "café.md")
	}
	if preview.TreeSHA == "" {
		t.Error("TreeSHA is empty, want the conflicted tree id merge-tree still computed")
	}
}

// TestWorktreeMergePreviewRefusesUnscreenedBaseNames is S-1's MergePreview
// half, mirroring TestWorktreeDivergenceRefusesUnscreenedBaseNames: a base
// git would read as an option, or one carrying control characters, is refused
// by checkName before merge-tree ever runs.
func TestWorktreeMergePreviewRefusesUnscreenedBaseNames(t *testing.T) {
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
			worktree, _ := newTaskWorktree(t, repo, 42, "Bad merge-tree base "+tt.name)

			_, err := worktree.MergePreview(t.Context(), tt.base)
			if err == nil {
				t.Fatalf("MergePreview succeeded with base %q, want a refusal", tt.base)
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

// TestWorktreeMergeBaseCleanMerge is AC-2.1's real-merge half: a branch behind
// by non-overlapping commits is merged --no-ff, MergeResult reports the move,
// and the merge commit carries the identity MergeOptions.Author supplied
// (S-4).
func TestWorktreeMergeBaseCleanMerge(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Clean merge")
	base := "origin/" + gittest.DefaultBranch
	advanceBase(t, repo, 3)

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}

	result, err := worktree.MergeBase(t.Context(), base, git.MergeOptions{Author: testAuthor})
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}

	if !result.Merged {
		t.Error("Merged = false, want true: base had commits the branch did not")
	}
	if result.Head == "" {
		t.Error("Head is empty")
	}
	if result.Head == before {
		t.Error("Head did not move, want a new merge commit")
	}

	parents := strings.Fields(repo.GitIn(dir, "log", "-1", "--format=%P", result.Head))
	if len(parents) != 2 {
		t.Fatalf("merge commit %s has %d parents (%v), want 2 (--no-ff)", result.Head, len(parents), parents)
	}

	authorLine := repo.GitIn(dir, "log", "-1", "--format=%an <%ae>", result.Head)
	want := testAuthor.Name + " <" + testAuthor.Email + ">"
	if authorLine != want {
		t.Errorf("author = %q, want %q", authorLine, want)
	}

	// merge-base --is-ancestor exits non-zero (which GitIn treats as fatal) if
	// base is not actually reachable from the new HEAD.
	repo.GitIn(dir, "merge-base", "--is-ancestor", repo.GitIn(dir, "rev-parse", base), result.Head)
}

// TestWorktreeMergeBaseAlreadyUpToDate covers base already being an ancestor
// of the branch: [git.MergeResult.Merged] is false and nothing moves, exactly
// as [git.CommitResult.Committed] behaves when there is nothing to commit.
func TestWorktreeMergeBaseAlreadyUpToDate(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, _ := newTaskWorktree(t, repo, 42, "Already up to date")
	base := "origin/" + gittest.DefaultBranch

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}

	result, err := worktree.MergeBase(t.Context(), base, git.MergeOptions{Author: testAuthor})
	if err != nil {
		t.Fatalf("MergeBase: %v", err)
	}

	if result.Merged {
		t.Error("Merged = true, want false: base was already an ancestor of the branch")
	}
	if result.Head != before {
		t.Errorf("Head = %s, want unchanged %s", result.Head, before)
	}
}

// TestWorktreeMergeBaseConflictRollsBack is S-5, the rollback guarantee: a
// merge that cannot complete is aborted and the branch tip is confirmed back
// where it started, reported as [git.ErrMergeConflict] rather than a raw exit
// code — and the worktree is byte-identical to before the call, proven by
// gittest.AssertUnchanged rather than assumed.
func TestWorktreeMergeBaseConflictRollsBack(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Conflicted merge rolls back")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}

	var result git.MergeResult
	gittest.AssertUnchanged(t, dir, func() {
		result, err = worktree.MergeBase(t.Context(), base, git.MergeOptions{Author: testAuthor})
	})

	if err == nil {
		t.Fatal("MergeBase succeeded against a conflicting base, want ErrMergeConflict")
	}
	if !errors.Is(err, git.ErrMergeConflict) {
		t.Fatalf("error = %v, want ErrMergeConflict", err)
	}
	if result != (git.MergeResult{}) {
		t.Errorf("result = %+v, want the zero value on failure", result)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD after aborted merge: %v", err)
	}
	if after != before {
		t.Errorf("branch tip moved from %s to %s despite the rollback", before, after)
	}
}

// TestWorktreeMergeBaseRefusesDirtyWorktree is S-5's other half: MergeBase
// refuses before touching anything when the worktree already holds
// uncommitted work, because that is what makes "the merge left this behind"
// impossible to tell apart from "this was already here".
func TestWorktreeMergeBaseRefusesDirtyWorktree(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Dirty worktree refuses to merge")
	base := "origin/" + gittest.DefaultBranch
	advanceBase(t, repo, 1)

	untracked := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(untracked, []byte("uncommitted\n"), 0o600); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}

	_, err = worktree.MergeBase(t.Context(), base, git.MergeOptions{Author: testAuthor})
	if !errors.Is(err, git.ErrWorktreeUncommittedChanges) {
		t.Fatalf("error = %v, want ErrWorktreeUncommittedChanges", err)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD after refused merge: %v", err)
	}
	if after != before {
		t.Errorf("branch tip moved from %s to %s though the merge was refused before starting", before, after)
	}
	if _, statErr := os.Stat(untracked); statErr != nil {
		t.Errorf("uncommitted file disappeared though the merge was refused before touching anything: %v", statErr)
	}
}

// TestWorktreeMergeBaseRefusesMissingBase covers a base that no longer
// resolves, mirroring Divergence's equivalent case: reported as
// [git.ErrRevisionNotFound] rather than a raw exit code.
func TestWorktreeMergeBaseRefusesMissingBase(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, _ := newTaskWorktree(t, repo, 42, "Missing merge base")

	_, err := worktree.MergeBase(t.Context(), "origin/does-not-exist", git.MergeOptions{Author: testAuthor})
	if err == nil {
		t.Fatal("MergeBase succeeded against a base ref that does not exist")
	}
	if !errors.Is(err, git.ErrRevisionNotFound) {
		t.Fatalf("error = %v, want ErrRevisionNotFound", err)
	}
}

// TestWorktreeMergeBaseRefusesUnscreenedBaseNames is S-1's MergeBase half.
func TestWorktreeMergeBaseRefusesUnscreenedBaseNames(t *testing.T) {
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
			worktree, _ := newTaskWorktree(t, repo, 42, "Bad merge base "+tt.name)

			_, err := worktree.MergeBase(t.Context(), tt.base, git.MergeOptions{Author: testAuthor})
			if err == nil {
				t.Fatalf("MergeBase succeeded with base %q, want a refusal", tt.base)
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

// TestWorktreeMergeBaseNeverTouchesOrigin is AC-2.3: no code path this story
// introduces ever writes to the base branch. MergeBase never pushes, so
// origin's own copy of the base branch must be exactly where it was before
// and after a successful merge.
func TestWorktreeMergeBaseNeverTouchesOrigin(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, _ := newTaskWorktree(t, repo, 42, "Never touches origin")
	base := "origin/" + gittest.DefaultBranch
	advanceBase(t, repo, 2)

	originBefore := repo.GitIn(repo.Origin, "rev-parse", gittest.DefaultBranch)

	if _, err := worktree.MergeBase(t.Context(), base, git.MergeOptions{Author: testAuthor}); err != nil {
		t.Fatalf("MergeBase: %v", err)
	}

	originAfter := repo.GitIn(repo.Origin, "rev-parse", gittest.DefaultBranch)
	if originAfter != originBefore {
		t.Errorf("origin's %s moved from %s to %s; MergeBase must never write to the base branch",
			gittest.DefaultBranch, originBefore, originAfter)
	}
}

// TestWorktreeStartMergeLeavesConflictForResolution: StartMerge leaves a
// conflicted merge in progress, reports its conflicting paths again on a
// second call without restarting it, and a later Commit concludes it as a
// merge commit on top of the base tip.
func TestWorktreeStartMergeLeavesConflictForResolution(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Start a conflicted merge")
	base := "origin/" + gittest.DefaultBranch
	conflictOnSharedFile(t, repo, dir)
	baseTip, err := worktree.RevParse(t.Context(), base, git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse base: %v", err)
	}

	conflicts, err := worktree.StartMerge(t.Context(), base)
	if err != nil {
		t.Fatalf("StartMerge: %v", err)
	}
	if !slices.Equal(conflicts, []string{"README.md"}) {
		t.Fatalf("conflicts = %v, want [README.md]", conflicts)
	}
	mergeHead, err := worktree.RevParse(t.Context(), "MERGE_HEAD", git.RevParseOptions{})
	if err != nil || mergeHead != baseTip {
		t.Fatalf("MERGE_HEAD = %q, %v; want %s", mergeHead, err, baseTip)
	}
	again, err := worktree.StartMerge(t.Context(), base)
	if err != nil || !slices.Equal(again, conflicts) {
		t.Fatalf("second StartMerge = %v, %v; want the same conflicts", again, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\nbranch change\nbase change\n"), 0o600); err != nil {
		t.Fatalf("resolve README.md: %v", err)
	}
	result, err := worktree.Commit(t.Context(), commitOptions("Resolve the merge.", 1))
	if err != nil || !result.Committed {
		t.Fatalf("Commit = %+v, %v", result, err)
	}
	parents := strings.Fields(repo.GitIn(dir, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 || parents[2] != baseTip {
		t.Fatalf("HEAD parents = %v, want a merge commit with second parent %s", parents, baseTip)
	}
}

// TestWorktreeStartMergeRefusesDirtyWorktree: without a merge in progress,
// uncommitted work is never mixed into a new merge.
func TestWorktreeStartMergeRefusesDirtyWorktree(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Dirty start merge")
	conflictOnSharedFile(t, repo, dir)
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.StartMerge(t.Context(), "origin/"+gittest.DefaultBranch); !errors.Is(err, git.ErrWorktreeUncommittedChanges) {
		t.Fatalf("StartMerge error = %v, want ErrWorktreeUncommittedChanges", err)
	}
}
