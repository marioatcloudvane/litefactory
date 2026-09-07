package turn

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// syncTestAuthor is the identity these tests merge as. It is unrelated to
// gittest's own commit identity, the same way [Options.Author] is unrelated
// to a developer's global git config.
var syncTestAuthor = git.Author{Name: "Mario Meir-Huber", Email: "mario@example.invalid"}

// newSyncWorktree builds the worktree these tests exercise, the same way
// [prepareClone] does through git.EnsureWorktree: a branch cut from
// origin/<base>, in a linked worktree next to the registered clone.
func newSyncWorktree(t *testing.T, repo *gittest.Repo, issue int, title string) *git.Worktree {
	t.Helper()

	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatalf("git.Resolve: %v", err)
	}
	clone, err := binary.Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", repo.Dir, err)
	}
	slug, err := naming.Slug(title)
	if err != nil {
		t.Fatalf("naming.Slug(%q): %v", title, err)
	}
	branch, err := naming.Branch("", issue, slug)
	if err != nil {
		t.Fatalf("naming.Branch: %v", err)
	}
	root := filepath.Join(filepath.Dir(repo.Dir), ".lf-worktrees", filepath.Base(repo.Dir))
	dir, err := naming.WorktreePath(root, issue)
	if err != nil {
		t.Fatalf("naming.WorktreePath: %v", err)
	}
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     branch,
		BaseBranch: gittest.DefaultBranch,
	})
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	return result.Worktree
}

// commitInSyncDir writes a uniquely named file into dir and commits it there,
// the way [internal/git]'s own merge tests advance a branch.
func commitInSyncDir(t *testing.T, repo *gittest.Repo, dir, name, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(message+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", message)
}

// advanceSyncBase pushes count new commits to origin's default branch from
// the registered clone's own working tree. Because the clone tracks origin,
// the push itself updates origin/<base> for every worktree sharing that
// repository, so no separate fetch is needed for [Worktree.Divergence] to see
// the new commits as Behind.
func advanceSyncBase(t *testing.T, repo *gittest.Repo, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("base-only-%d.txt", i)
		commitInSyncDir(t, repo, repo.Dir, name, fmt.Sprintf("base commit %d", i))
	}
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
}

// conflictBaseAndBranch makes base and branch each edit README.md
// differently from the same starting content, so merge-tree cannot
// reconcile them.
func conflictBaseAndBranch(t *testing.T, repo *gittest.Repo, dir string) {
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

// conflictBaseAndBranchWithOversizedFile is [conflictBaseAndBranch] plus a
// second conflicting file, huge.txt, whose base and branch versions are
// wholly different (not just one differing line) so the extracted conflict
// hunk [collectConflictContent] would compute for it exceeds
// conflictFilePerFileMaxBytes on its own and is excluded whole (AC-11.5):
// huge.txt conflicts (its path is in [git.MergePreview.Conflicts], i.e. in a
// syncRefusal's own conflicts list) but is never one of the files actually
// collected and sent to a suggestion advisor.
func conflictBaseAndBranchWithOversizedFile(t *testing.T, repo *gittest.Repo, dir string) {
	t.Helper()
	conflictBaseAndBranch(t, repo, dir)

	hugeBase := strings.Repeat("x", conflictFilePerFileMaxBytes+1)
	hugeBranch := strings.Repeat("y", conflictFilePerFileMaxBytes+1)

	if err := os.WriteFile(filepath.Join(repo.Dir, "huge.txt"), []byte(hugeBase), 0o600); err != nil {
		t.Fatalf("write base huge.txt: %v", err)
	}
	repo.Git("add", "--all")
	repo.Git("commit", "--quiet", "--message", "base edits huge.txt")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	if err := os.WriteFile(filepath.Join(dir, "huge.txt"), []byte(hugeBranch), 0o600); err != nil {
		t.Fatalf("write branch huge.txt: %v", err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", "branch edits huge.txt")
}

// TestSyncBaseUpToDateShortCircuitsOnZeroBehind is the architecture's first
// decision-tree branch: Behind==0 reaches syncOutcomeUpToDate even with
// auto-sync enabled, and the branch is left exactly as it was — there is
// nothing to preview or merge when the branch already has everything base
// does.
func TestSyncBaseUpToDateShortCircuitsOnZeroBehind(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Up to date already")
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: true}

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	result, err := syncBase(t.Context(), opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeUpToDate {
		t.Errorf("Outcome = %q, want %q", result.Outcome, syncOutcomeUpToDate)
	}
	if result.Behind != 0 {
		t.Errorf("Behind = %d, want 0", result.Behind)
	}
	if result.SyncCommitSHA != "" {
		t.Errorf("SyncCommitSHA = %q, want empty", result.SyncCommitSHA)
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("Conflicts = %v, want empty", result.Conflicts)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}
	if after != before {
		t.Errorf("branch HEAD moved from %s to %s though Behind was 0", before, after)
	}
}

// TestSyncBaseSkippedWhenAutoSyncDisabled covers the disabled branch of the
// decision tree: behind but auto-sync is off, so the outcome is skipped and
// the branch is untouched — drift is measured (Behind is still reported) but
// nothing is attempted.
func TestSyncBaseSkippedWhenAutoSyncDisabled(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Sync disabled")
	advanceSyncBase(t, repo, 2)
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: false}

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	result, err := syncBase(t.Context(), opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeSkipped {
		t.Errorf("Outcome = %q, want %q", result.Outcome, syncOutcomeSkipped)
	}
	if result.Behind != 2 {
		t.Errorf("Behind = %d, want 2", result.Behind)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}
	if after != before {
		t.Error("branch HEAD moved though auto-sync was disabled")
	}
}

// TestSyncBaseSkippedOverCap covers [withinSyncCap]: behind commits exceeding
// [Options.AutoSyncMaxBehind] is skipped exactly like a disabled default,
// never previewed or merged.
func TestSyncBaseSkippedOverCap(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Over the sync cap")
	advanceSyncBase(t, repo, 3)
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: true, AutoSyncMaxBehind: 2}

	result, err := syncBase(t.Context(), opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeSkipped {
		t.Errorf("Outcome = %q, want %q (3 behind exceeds the cap of 2)", result.Outcome, syncOutcomeSkipped)
	}
}

// TestSyncBaseSyncOverrideBeatsConfigDefault is AC-7.1's half that belongs to
// this story: a non-nil [Request.SyncOverride] wins over
// [Options.AutoSyncBase] in either direction. Here it turns a configured
// default of "off" into "on".
func TestSyncBaseSyncOverrideBeatsConfigDefault(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Override enables sync")
	advanceSyncBase(t, repo, 1)
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: false}
	enable := true

	result, err := syncBase(t.Context(), opts, Request{SyncOverride: &enable}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeSynced {
		t.Fatalf("Outcome = %q, want %q: SyncOverride=true must beat AutoSyncBase=false", result.Outcome, syncOutcomeSynced)
	}
	if result.SyncCommitSHA == "" {
		t.Error("SyncCommitSHA is empty, want the merge commit sha")
	}
}

// TestSyncBaseMergesCleanly is AC-2.1: a clean preview is followed by a real
// merge, the outcome is synced, and SyncCommitSHA names the commit that is
// now the worktree's actual HEAD.
func TestSyncBaseMergesCleanly(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Clean sync")
	advanceSyncBase(t, repo, 3)
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: true}

	result, err := syncBase(t.Context(), opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeSynced {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, syncOutcomeSynced)
	}
	if result.Behind != 3 {
		t.Errorf("Behind = %d, want 3", result.Behind)
	}
	if result.SyncCommitSHA == "" {
		t.Fatal("SyncCommitSHA is empty, want the merge commit sha")
	}

	head, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}
	if head != result.SyncCommitSHA {
		t.Errorf("worktree HEAD = %s, want it to match SyncCommitSHA %s", head, result.SyncCommitSHA)
	}
}

// TestSyncBaseBlockedOnConflictLeavesBranchUntouched is AC-2.2 read through
// syncBase's own decision tree: an unclean preview reaches syncOutcomeBlocked
// with the conflicting paths attached, SyncCommitSHA stays empty, and the
// branch is exactly as [ensureWorktree] would have produced it — MergeBase is
// never called.
func TestSyncBaseBlockedOnConflictLeavesBranchUntouched(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree := newSyncWorktree(t, repo, 42, "Blocked by a real conflict")
	conflictBaseAndBranch(t, repo, worktree.Path())
	repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}
	opts := Options{AutoSyncBase: true}

	before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}

	result, err := syncBase(t.Context(), opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
	if err != nil {
		t.Fatalf("syncBase: %v", err)
	}

	if result.Outcome != syncOutcomeBlocked {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, syncOutcomeBlocked)
	}
	if !slices.Contains(result.Conflicts, "README.md") {
		t.Errorf("Conflicts = %v, want to contain README.md", result.Conflicts)
	}
	if result.SyncCommitSHA != "" {
		t.Errorf("SyncCommitSHA = %q, want empty on a blocked outcome", result.SyncCommitSHA)
	}

	after, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD: %v", err)
	}
	if after != before {
		t.Errorf("branch HEAD moved from %s to %s though the merge was blocked", before, after)
	}
}

// TestSyncBaseNeverTouchesOriginAcrossEveryOutcome is AC-2.3's "in every
// outcome" read literally: no code path syncBase can take may ever write to
// origin's copy of the base branch, and that is checked here for all four
// outcomes syncBase's decision tree can reach — not just the synced path
// [merge_test.go]'s TestWorktreeMergeBaseNeverTouchesOrigin already covers on
// [git.Worktree.MergeBase] directly. Proven per outcome rather than assumed
// from "the up-to-date and skipped paths issue no write commands at all",
// following this codebase's own S-5 discipline of proving a guarantee rather
// than asserting it holds by construction.
func TestSyncBaseNeverTouchesOriginAcrossEveryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, repo *gittest.Repo, dir string)
		opts  Options
		want  syncOutcome
	}{
		{
			name:  "up to date",
			setup: func(*testing.T, *gittest.Repo, string) {},
			opts:  Options{AutoSyncBase: true},
			want:  syncOutcomeUpToDate,
		},
		{
			name: "skipped: auto-sync disabled",
			setup: func(t *testing.T, repo *gittest.Repo, dir string) {
				advanceSyncBase(t, repo, 2)
			},
			opts: Options{AutoSyncBase: false},
			want: syncOutcomeSkipped,
		},
		{
			name: "synced: clean merge",
			setup: func(t *testing.T, repo *gittest.Repo, dir string) {
				advanceSyncBase(t, repo, 2)
			},
			opts: Options{AutoSyncBase: true},
			want: syncOutcomeSynced,
		},
		{
			name: "blocked: real conflict",
			setup: func(t *testing.T, repo *gittest.Repo, dir string) {
				conflictBaseAndBranch(t, repo, dir)
			},
			opts: Options{AutoSyncBase: true},
			want: syncOutcomeBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			worktree := newSyncWorktree(t, repo, 42, "Origin untouched: "+tt.name)
			tt.setup(t, repo, worktree.Path())
			repoRow := store.Repo{BaseBranch: gittest.DefaultBranch}

			originBefore := repo.GitIn(repo.Origin, "rev-parse", gittest.DefaultBranch)

			result, err := syncBase(t.Context(), tt.opts, Request{}, worktree, repoRow, syncTestAuthor, discardLogger())
			if err != nil {
				t.Fatalf("syncBase: %v", err)
			}
			if result.Outcome != tt.want {
				t.Fatalf("Outcome = %q, want %q", result.Outcome, tt.want)
			}

			originAfter := repo.GitIn(repo.Origin, "rev-parse", gittest.DefaultBranch)
			if originAfter != originBefore {
				t.Errorf("origin's %s moved from %s to %s on outcome %q; syncBase must never write to the base branch",
					gittest.DefaultBranch, originBefore, originAfter, tt.want)
			}
		})
	}
}
