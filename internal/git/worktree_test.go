package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
)

// The identity every commit in these tests is authored as. litefactory commits
// as the human who registered the repository, never as a bot: branch
// protection, CODEOWNERS and CI all key off the author.
var testAuthor = git.Author{Name: "Mario Meir-Huber", Email: "mario@example.invalid"}

// task derives the branch and worktree directory exactly the way a turn does,
// through internal/naming, so these tests exercise the real names rather than
// hand-written ones that could drift from S-6.
func task(t *testing.T, worktreeRoot string, issue int, title string) (branch, dir string) {
	t.Helper()

	slug, err := naming.Slug(title)
	if err != nil {
		t.Fatalf("naming.Slug(%q): %v", title, err)
	}
	branch, err = naming.Branch("", issue, slug)
	if err != nil {
		t.Fatalf("naming.Branch(%d, %q): %v", issue, slug, err)
	}
	dir, err = naming.WorktreePath(worktreeRoot, issue)
	if err != nil {
		t.Fatalf("naming.WorktreePath(%q, %d): %v", worktreeRoot, issue, err)
	}
	return branch, dir
}

// worktreeRoot returns a sibling of the clone, the way S-6's default does. It
// deliberately does not exist yet: creating the parents is part of AC-3.9.
func worktreeRoot(repo *gittest.Repo) string {
	return filepath.Join(filepath.Dir(repo.Dir), ".lf-worktrees", filepath.Base(repo.Dir))
}

func commitOptions(summary string, run int) git.CommitOptions {
	return git.CommitOptions{
		Summary: summary,
		Task:    "github:cloudvane/litefactory#42",
		Mode:    "spike",
		Run:     run,
		Author:  testAuthor,
	}
}

// TestEnsureWorktreeCreatesTheBranchFromTheBase is the first-run path of
// AC-3.8 and AC-3.9: a branch that exists nowhere starts at origin/<base>, and
// the worktree's parent directories are created on the way.
func TestEnsureWorktreeCreatesTheBranchFromTheBase(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	root := worktreeRoot(repo)
	branch, dir := task(t, root, 42, "Add a retry to the webhook handler")

	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the worktree root already exists, so parent creation is untested: %v", err)
	}

	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     branch,
		BaseBranch: gittest.DefaultBranch,
	})
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}

	if !result.BranchCreated || !result.WorktreeCreated {
		t.Errorf("result = %+v, want both BranchCreated and WorktreeCreated", result)
	}
	if want := "origin/" + gittest.DefaultBranch; result.BranchStart != want {
		t.Errorf("BranchStart = %q, want %q", result.BranchStart, want)
	}
	if result.Worktree.Path() != dir {
		t.Errorf("Worktree.Path() = %q, want %q", result.Worktree.Path(), dir)
	}
	if result.Worktree.Branch() != branch {
		t.Errorf("Worktree.Branch() = %q, want %q", result.Worktree.Branch(), branch)
	}
	if got := repo.GitIn(dir, "rev-parse", "--abbrev-ref", "HEAD"); got != branch {
		t.Errorf("worktree is on %q, want %q", got, branch)
	}
	if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != repo.Head() {
		t.Errorf("worktree HEAD = %q, want origin/%s at %q", got, gittest.DefaultBranch, repo.Head())
	}
}

// TestEnsureWorktreeReusesAnExistingBranch is the other half of AC-3.8. A
// second run on the same task must continue the first run's branch: recreating
// it from the base would silently throw the first run's commits away.
func TestEnsureWorktreeReusesAnExistingBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare leaves the branch in place and returns the commit it must
		// resolve to.
		prepare   func(t *testing.T, repo *gittest.Repo, branch string) string
		wantStart func(branch string) string
		wantMade  bool
	}{
		{
			name: "the branch exists locally",
			prepare: func(t *testing.T, repo *gittest.Repo, branch string) string {
				t.Helper()
				repo.Git("checkout", "--quiet", "-b", branch)
				repo.WriteFile("earlier-run.txt", "work from run 1\n")
				sha := repo.Commit("run 1")
				repo.Git("checkout", "--quiet", gittest.DefaultBranch)
				return sha
			},
			wantStart: func(string) string { return "" },
			wantMade:  false,
		},
		{
			name: "the branch exists only on the remote",
			prepare: func(t *testing.T, repo *gittest.Repo, branch string) string {
				t.Helper()
				repo.Git("checkout", "--quiet", "-b", branch)
				repo.WriteFile("earlier-run.txt", "work from another machine\n")
				sha := repo.Commit("run 1 elsewhere")
				repo.Git("push", "--quiet", "origin", branch)
				repo.Git("checkout", "--quiet", gittest.DefaultBranch)
				repo.Git("branch", "--delete", "--force", branch)
				return sha
			},
			wantStart: func(branch string) string { return "origin/" + branch },
			wantMade:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			branch, dir := task(t, worktreeRoot(repo), 7, "Continue the earlier run")
			want := tc.prepare(t, repo, branch)

			clone := openFixture(t, repo)
			result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
				Dir:        dir,
				Branch:     branch,
				BaseBranch: gittest.DefaultBranch,
			})
			if err != nil {
				t.Fatalf("EnsureWorktree: %v", err)
			}

			if result.BranchCreated != tc.wantMade {
				t.Errorf("BranchCreated = %v, want %v", result.BranchCreated, tc.wantMade)
			}
			if got := tc.wantStart(branch); result.BranchStart != got {
				t.Errorf("BranchStart = %q, want %q", result.BranchStart, got)
			}
			if got := repo.GitIn(dir, "rev-parse", "HEAD"); got != want {
				t.Errorf("worktree HEAD = %q, want the earlier run's commit %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(dir, "earlier-run.txt")); err != nil {
				t.Errorf("the earlier run's file is missing from the worktree: %v", err)
			}
		})
	}
}

// TestEnsureWorktreeReusesTheWorktree covers the "reused if valid" half of
// AC-3.9. A second call must hand back the same directory with its contents
// intact — an in-progress run's uncommitted work included.
func TestEnsureWorktreeReusesTheWorktree(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	branch, dir := task(t, worktreeRoot(repo), 42, "Reuse me")

	opts := git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch}
	if _, err := git.EnsureWorktree(t.Context(), clone, opts); err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}

	const marker = "work in progress\n"
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte(marker), 0o600); err != nil {
		t.Fatalf("write into the worktree: %v", err)
	}

	result, err := git.EnsureWorktree(t.Context(), clone, opts)
	if err != nil {
		t.Fatalf("second EnsureWorktree: %v", err)
	}
	if result.WorktreeCreated || result.BranchCreated {
		t.Errorf("result = %+v, want nothing created on reuse", result)
	}
	if result.Worktree.Path() != dir {
		t.Errorf("Worktree.Path() = %q, want %q", result.Worktree.Path(), dir)
	}

	content, err := os.ReadFile(filepath.Join(dir, "wip.txt"))
	if err != nil {
		t.Fatalf("read the file back: %v", err)
	}
	if string(content) != marker {
		t.Errorf("the reused worktree's file = %q, want %q", content, marker)
	}

	// One worktree, not two: reuse must not have registered a second entry.
	list, err := clone.WorktreeList(t.Context())
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("WorktreeList returned %d entries, want the clone and one worktree: %+v", len(list), list)
	}
}

// TestEnsureWorktreeRefusesAnOccupiedPath is AC-3.9's loud failure.
//
// Every case here asserts twice: that the call was refused with a named
// condition, and that what was already at the path is byte-for-byte intact.
// The second assertion is the one that matters. Deleting a directory because it
// was in the way is not a recoverable mistake, and a refusal that quietly
// cleaned up first would pass a test that only checked the error.
func TestEnsureWorktreeRefusesAnOccupiedPath(t *testing.T) {
	t.Parallel()

	const precious = "a year of unrelated work\n"

	tests := []struct {
		name string
		// occupy puts something at dir and returns the path whose contents
		// must survive.
		occupy func(t *testing.T, repo *gittest.Repo, clone *git.Clone, dir, branch string) string
	}{
		{
			name: "a non-empty directory",
			occupy: func(t *testing.T, _ *gittest.Repo, _ *git.Clone, dir, _ string) string {
				t.Helper()
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("create the occupying directory: %v", err)
				}
				file := filepath.Join(dir, "notes.txt")
				if err := os.WriteFile(file, []byte(precious), 0o600); err != nil {
					t.Fatalf("write the occupying file: %v", err)
				}
				return file
			},
		},
		{
			name: "a regular file",
			occupy: func(t *testing.T, _ *gittest.Repo, _ *git.Clone, dir, _ string) string {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
					t.Fatalf("create the parent directory: %v", err)
				}
				if err := os.WriteFile(dir, []byte(precious), 0o600); err != nil {
					t.Fatalf("write the occupying file: %v", err)
				}
				return dir
			},
		},
		{
			name: "a registered worktree on another branch",
			occupy: func(t *testing.T, _ *gittest.Repo, clone *git.Clone, dir, _ string) string {
				t.Helper()
				if _, err := clone.WorktreeAdd(t.Context(), dir, git.WorktreeAddOptions{
					Branch:     "lf/999-someone-elses-task",
					CreateFrom: "origin/" + gittest.DefaultBranch,
				}); err != nil {
					t.Fatalf("add the occupying worktree: %v", err)
				}
				file := filepath.Join(dir, "notes.txt")
				if err := os.WriteFile(file, []byte(precious), 0o600); err != nil {
					t.Fatalf("write into the occupying worktree: %v", err)
				}
				return file
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := gittest.New(t)
			clone := openFixture(t, repo)
			branch, dir := task(t, worktreeRoot(repo), 42, "Wants an occupied path")
			survivor := tc.occupy(t, repo, clone, dir, branch)

			result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
				Dir:        dir,
				Branch:     branch,
				BaseBranch: gittest.DefaultBranch,
			})
			if err == nil {
				t.Fatalf("EnsureWorktree produced a worktree at %s, want a refusal", result.Worktree.Path())
			}
			if !errors.Is(err, git.ErrWorktreeOccupied) {
				t.Fatalf("error = %v, want ErrWorktreeOccupied", err)
			}

			var conflict *git.WorktreeConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("error %v is not a *git.WorktreeConflictError", err)
			}
			if conflict.Path != dir {
				t.Errorf("WorktreeConflictError.Path = %q, want %q", conflict.Path, dir)
			}
			if conflict.WantBranch != branch {
				t.Errorf("WorktreeConflictError.WantBranch = %q, want %q", conflict.WantBranch, branch)
			}
			if conflict.Found == "" {
				t.Error("WorktreeConflictError.Found is empty; the message cannot say what is in the way")
			}

			content, err := os.ReadFile(survivor)
			if err != nil {
				t.Fatalf("what was at the path is gone: %v", err)
			}
			if string(content) != precious {
				t.Errorf("what was at the path = %q, want it untouched at %q", content, precious)
			}
		})
	}
}

// TestEnsureWorktreeRefusesAStaleRegistration covers the case a user creates by
// hand: they deleted the worktree directory with rm -rf, leaving git's
// administrative entry behind. Adding over it fails, and pruning is not in the
// S-6 allowlist, so the refusal has to say what will fix it.
func TestEnsureWorktreeRefusesAStaleRegistration(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	branch, dir := task(t, worktreeRoot(repo), 42, "Deleted by hand")

	opts := git.EnsureWorktreeOptions{Dir: dir, Branch: branch, BaseBranch: gittest.DefaultBranch}
	if _, err := git.EnsureWorktree(t.Context(), clone, opts); err != nil {
		t.Fatalf("first EnsureWorktree: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove the worktree directory: %v", err)
	}

	_, err := git.EnsureWorktree(t.Context(), clone, opts)
	if err == nil {
		t.Fatal("EnsureWorktree succeeded over a stale registration")
	}
	if !errors.Is(err, git.ErrWorktreeOccupied) {
		t.Fatalf("error = %v, want ErrWorktreeOccupied", err)
	}
	if !strings.Contains(err.Error(), "task clean") {
		t.Errorf("error %q does not say how to recover", err)
	}
}

// TestEnsureWorktreeRefusesPathsOverlappingTheClone is the central guarantee,
// checked on the second constructor. Clone.WorktreeAdd refuses these paths; so
// must the composite operation built on top of it, and it must refuse before
// running any git command rather than after creating something.
func TestEnsureWorktreeRefusesPathsOverlappingTheClone(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()
	clone := openFixture(t, repo)

	for _, dir := range []string{
		repo.Dir,
		filepath.Join(repo.Dir, "task-42"),
		filepath.Join(repo.Dir, ".git", "task-42"),
		filepath.Dir(repo.Dir),
	} {
		gittest.AssertUnchanged(t, repo.Dir, func() {
			result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
				Dir:        dir,
				Branch:     "lf/42-nope",
				BaseBranch: gittest.DefaultBranch,
			})
			if err == nil {
				t.Fatalf("EnsureWorktree(%s) produced a worktree at %s, want a refusal",
					dir, result.Worktree.Path())
			}
			if !errors.Is(err, git.ErrWorktreeOverlapsClone) {
				t.Fatalf("EnsureWorktree(%s) error = %v, want ErrWorktreeOverlapsClone", dir, err)
			}
		})
	}
}

// TestCommitWritesTheProvenanceTrailers is the shape of AC-3.21: the summary
// above, the three trailers below, authored as the human.
func TestCommitWritesTheProvenanceTrailers(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Add the trailers")

	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	result, err := worktree.Commit(t.Context(), commitOptions("Added a retry to the webhook handler.", 3))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !result.Committed {
		t.Fatal("Commit reported nothing to commit, but the run wrote a file")
	}
	if result.SHA == "" || result.SHA != result.Head {
		t.Errorf("SHA = %q, Head = %q, want both set to the new commit", result.SHA, result.Head)
	}

	message := repo.GitIn(dir, "log", "-1", "--format=%B")
	summary, trailers, ok := strings.Cut(message, "\n\n")
	if !ok {
		t.Fatalf("commit message has no blank line between summary and trailers:\n%s", message)
	}
	if summary != "Added a retry to the webhook handler." {
		t.Errorf("summary = %q, want the run's own text", summary)
	}

	wantTrailers := []string{
		"Litefactory-Task: github:cloudvane/litefactory#42",
		"Litefactory-Mode: spike",
		"Litefactory-Run: 3",
	}
	got := strings.Split(strings.TrimRight(trailers, "\n"), "\n")
	if !slices.Equal(got, wantTrailers) {
		t.Errorf("trailers = %q, want %q", got, wantTrailers)
	}

	// Authored as the human, not as a bot: branch protection and CI key off
	// this, and a synthetic identity behaves differently from a hand-made
	// commit.
	if name := repo.GitIn(dir, "log", "-1", "--format=%an"); name != testAuthor.Name {
		t.Errorf("author name = %q, want %q", name, testAuthor.Name)
	}
	if email := repo.GitIn(dir, "log", "-1", "--format=%ae"); email != testAuthor.Email {
		t.Errorf("author email = %q, want %q", email, testAuthor.Email)
	}
	if result.Message != message+"\n" && result.Message != message {
		t.Errorf("CommitResult.Message = %q, want the message git recorded %q", result.Message, message)
	}
}

// TestCommitStripsForgedTrailersFromTheSummary is the security half of AC-3.21.
//
// The summary is written by Claude and is therefore untrusted, while the
// trailers are what a later reader trusts to say which task and which run
// produced a commit. A summary containing its own Litefactory-Task line would
// forge exactly that, so every line in the namespace is removed before the real
// trailers are appended.
func TestCommitStripsForgedTrailersFromTheSummary(t *testing.T) {
	t.Parallel()

	const forged = "I fixed the bug.\n" +
		"Litefactory-Task: github:attacker/evil#1\n" +
		"  litefactory-run: 9999\n" +
		"LITEFACTORY-Mode: production-deploy\n" +
		"That is all."

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Forge some trailers")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	if _, err := worktree.Commit(t.Context(), commitOptions(forged, 3)); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	message := repo.GitIn(dir, "log", "-1", "--format=%B")
	for _, ghost := range []string{"attacker/evil", "9999", "production-deploy"} {
		if strings.Contains(message, ghost) {
			t.Errorf("the forged value %q survived into the commit message:\n%s", ghost, message)
		}
	}
	for _, prefix := range []string{"Litefactory-Task", "Litefactory-Mode", "Litefactory-Run"} {
		if n := strings.Count(message, prefix); n != 1 {
			t.Errorf("%s appears %d times in the message, want exactly 1:\n%s", prefix, n, message)
		}
	}
	if !strings.HasPrefix(message, "I fixed the bug.\nThat is all.\n\n") {
		t.Errorf("the summary's ordinary lines did not survive intact:\n%s", message)
	}

	// git's own trailer parser must agree: it is what a report or a script
	// would use, and it reads trailers more loosely than a strict prefix match.
	parsed := repo.GitIn(dir, "log", "-1", "--format=%(trailers:only,unfold)")
	if strings.Count(parsed, "Litefactory-Task:") != 1 {
		t.Errorf("git parses %d Litefactory-Task trailers, want 1:\n%s",
			strings.Count(parsed, "Litefactory-Task:"), parsed)
	}
	if !strings.Contains(parsed, "Litefactory-Task: github:cloudvane/litefactory#42") {
		t.Errorf("git's parsed trailers do not carry the real task ref:\n%s", parsed)
	}
}

// TestCommitWithAnEmptySummaryStillCommits covers the degenerate input: a
// summary that is empty, whitespace, or nothing but forged trailers leaves no
// subject line at all, and a commit with an empty message is not something git
// will make.
func TestCommitWithAnEmptySummaryStillCommits(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Empty summary")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	if _, err := worktree.Commit(t.Context(), commitOptions("  \n Litefactory-Task: x \n", 1)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if subject := repo.GitIn(dir, "log", "-1", "--format=%s"); subject == "" {
		t.Error("the commit has an empty subject")
	}
}

// TestCommitWithNoChangesMakesNoCommit is AC-3.23. It is a distinguishable
// outcome and not an error: a run that changed nothing succeeded.
func TestCommitWithNoChangesMakesNoCommit(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Change nothing")

	before := repo.GitIn(dir, "rev-parse", "HEAD")
	count := repo.GitIn(dir, "rev-list", "--count", "HEAD")

	result, err := worktree.Commit(t.Context(), commitOptions("I looked around and changed nothing.", 1))
	if err != nil {
		t.Fatalf("Commit with no changes returned an error: %v", err)
	}
	if result.Committed {
		t.Error("Commit reported Committed on an unchanged worktree")
	}
	if result.SHA != "" {
		t.Errorf("SHA = %q, want empty when nothing was committed", result.SHA)
	}
	if result.Head != before {
		t.Errorf("Head = %q, want the unmoved HEAD %q", result.Head, before)
	}
	if len(result.Files) != 0 {
		t.Errorf("Files = %v, want none", result.Files)
	}
	if got := repo.GitIn(dir, "rev-list", "--count", "HEAD"); got != count {
		t.Errorf("the branch has %s commits, want the unchanged %s", got, count)
	}
}

// TestStatusReportsUntrackedButNotIgnoredFiles is AC-3.21 and AC-4.4's
// files-touched list. The untracked half is what litefactory sees most — a run
// creating new files — and the ignored half is what must never reach a commit
// or a report.
func TestStatusReportsUntrackedButNotIgnoredFiles(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.WriteFile(".gitignore", "build/\nsecrets.env\n")
	repo.WriteFile("tracked.txt", "before\n")
	repo.WriteFile("doomed.txt", "delete me\n")
	repo.Commit("set the stage")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	worktree, dir := newTaskWorktree(t, repo, 42, "Touch some files")
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	write("tracked.txt", "after\n")
	write("internal/new/thing.go", "package thing\n")
	write("build/artifact.bin", "ignored\n")
	write("secrets.env", "TOKEN=ghp_notreal\n")
	if err := os.Remove(filepath.Join(dir, "doomed.txt")); err != nil {
		t.Fatalf("remove doomed.txt: %v", err)
	}

	changes, err := worktree.Status(t.Context())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.String())
	}
	slices.Sort(got)

	want := []string{
		"deleted doomed.txt",
		"modified tracked.txt",
		// --untracked-files=all lists the file inside the new directory, not
		// the directory: a report has to name what the run actually wrote.
		"untracked internal/new/thing.go",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Status = %q, want %q", got, want)
	}

	// The ignored files must not reach the commit either, not just the list.
	if _, err := worktree.Commit(t.Context(), commitOptions("Touched some files.", 1)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	committed := repo.GitIn(dir, "show", "--name-only", "--format=", "HEAD")
	for _, ignored := range []string{"build/artifact.bin", "secrets.env"} {
		if strings.Contains(committed, ignored) {
			t.Errorf("the ignored file %s was committed:\n%s", ignored, committed)
		}
	}
}

// TestPushPublishesTheBranch proves the mechanism against a local bare
// repository. Per SD-9 no test here reaches a real remote: SSH to GitHub is
// deferred, and a local origin exercises the same refspec and the same
// --set-upstream bookkeeping.
func TestPushPublishesTheBranch(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Push me")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	result, err := worktree.Commit(t.Context(), commitOptions("Wrote a file.", 1))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := worktree.Push(t.Context(), git.DefaultRemote); err != nil {
		t.Fatalf("Push: %v", err)
	}

	published := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+worktree.Branch())
	if published != result.SHA {
		t.Errorf("origin has %q on %s, want the new commit %q", published, worktree.Branch(), result.SHA)
	}

	// Pushing twice is how a second run behaves; it must be a no-op rather
	// than a rejection.
	if err := worktree.Push(t.Context(), git.DefaultRemote); err != nil {
		t.Fatalf("second Push: %v", err)
	}
}

// TestPushFailureLeavesTheCommitIntact is AC-3.24's recovery path. A push that
// does not reach the remote leaves a perfectly good commit on a local branch,
// and the difference between a recoverable situation and a lost afternoon is
// whether the error says where that branch is.
func TestPushFailureLeavesTheCommitIntact(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Fail to push")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	result, err := worktree.Commit(t.Context(), commitOptions("Wrote a file.", 1))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// A remote that cannot be reached. A local path fails immediately, so the
	// test does not spend the push deadline finding out.
	repo.SetOriginURL(filepath.Join(filepath.Dir(repo.Dir), "there-is-no-repository-here.git"))

	err = worktree.Push(t.Context(), git.DefaultRemote)
	if err == nil {
		t.Fatal("Push succeeded against a remote that does not exist")
	}

	var pushErr *git.PushError
	if !errors.As(err, &pushErr) {
		t.Fatalf("error %v is not a *git.PushError", err)
	}
	if pushErr.Worktree != dir {
		t.Errorf("PushError.Worktree = %q, want the local worktree %q", pushErr.Worktree, dir)
	}
	if pushErr.Branch != worktree.Branch() {
		t.Errorf("PushError.Branch = %q, want %q", pushErr.Branch, worktree.Branch())
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the worktree holding the commit", err)
	}

	// The commit is the point: it must still be there, on the branch, with its
	// content.
	if head := repo.GitIn(dir, "rev-parse", "HEAD"); head != result.SHA {
		t.Errorf("HEAD = %q after the failed push, want the commit %q", head, result.SHA)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("the run's file is gone after the failed push: %v", err)
	}
	if status := repo.GitIn(dir, "status", "--porcelain"); status != "" {
		t.Errorf("the worktree is dirty after the failed push, so the commit did not hold:\n%s", status)
	}
}

// TestWorktreeRemoveRefusesUncommittedChangesUnlessForced is AC-3.25, the
// capability "litefactory task clean <issue>" is built on.
//
// The unforced refusal is what makes losing work impossible by accident, so it
// is checked the same way the occupied-path refusal is: the error is named, and
// the files are still there afterwards.
func TestWorktreeRemoveRefusesUncommittedChangesUnlessForced(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	_, dir := newTaskWorktreeIn(t, repo, clone, 42, "Clean me")

	const unsaved = "not committed anywhere\n"
	if err := os.WriteFile(filepath.Join(dir, "unsaved.txt"), []byte(unsaved), 0o600); err != nil {
		t.Fatalf("write into the worktree: %v", err)
	}

	err := clone.WorktreeRemove(t.Context(), dir, git.WorktreeRemoveOptions{})
	if err == nil {
		t.Fatal("WorktreeRemove deleted a worktree holding uncommitted work")
	}
	if !errors.Is(err, git.ErrWorktreeUncommittedChanges) {
		t.Fatalf("error = %v, want ErrWorktreeUncommittedChanges", err)
	}

	var dirty *git.WorktreeDirtyError
	if !errors.As(err, &dirty) {
		t.Fatalf("error %v is not a *git.WorktreeDirtyError", err)
	}
	if dirty.Path != dir {
		t.Errorf("WorktreeDirtyError.Path = %q, want %q", dirty.Path, dir)
	}
	if len(dirty.Changes) == 0 {
		t.Error("WorktreeDirtyError carries no changes, so the CLI cannot say what would be lost")
	}

	content, err := os.ReadFile(filepath.Join(dir, "unsaved.txt"))
	if err != nil {
		t.Fatalf("the refused removal took the file anyway: %v", err)
	}
	if string(content) != unsaved {
		t.Errorf("file = %q, want it untouched at %q", content, unsaved)
	}

	if err := clone.WorktreeRemove(t.Context(), dir, git.WorktreeRemoveOptions{Force: true}); err != nil {
		t.Fatalf("forced WorktreeRemove: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the worktree survived a forced removal: %v", err)
	}
	list, err := clone.WorktreeList(t.Context())
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("WorktreeList returned %d entries after removal, want only the clone: %+v", len(list), list)
	}
}

// TestFullCycleLeavesTheRegisteredCloneUntouched is AC-3.10, and the headline
// assertion of this package.
//
// The whole mutating half runs inside the assertion — ensure, status, commit,
// push — against a deliberately dirtied clone. The dirt is the point: against a
// clean checkout this assertion is trivially satisfiable and would not notice a
// bug that reset the index or moved HEAD. The guarantee is that a user can be
// mid-rebase in their own repository while litefactory works, and find it
// exactly as they left it.
func TestFullCycleLeavesTheRegisteredCloneUntouched(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()
	if status := repo.Git("status", "--porcelain"); status == "" {
		t.Fatal("the fixture is clean, so this assertion would prove nothing")
	}

	clone := openFixture(t, repo)
	branch, dir := task(t, worktreeRoot(repo), 42, "Do the whole cycle")

	var result git.CommitResult
	gittest.AssertUnchanged(t, repo.Dir, func() {
		ensured, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
			Dir:        dir,
			Branch:     branch,
			BaseBranch: gittest.DefaultBranch,
		})
		if err != nil {
			t.Fatalf("EnsureWorktree: %v", err)
		}
		worktree := ensured.Worktree

		// sha_before is read here, before the work, exactly as a turn reads it
		// before starting the subprocess.
		before, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
		if err != nil {
			t.Fatalf("RevParse: %v", err)
		}

		if err := os.WriteFile(filepath.Join(dir, "produced.txt"), []byte("by the run\n"), 0o600); err != nil {
			t.Fatalf("write into the worktree: %v", err)
		}
		if _, err := worktree.Status(t.Context()); err != nil {
			t.Fatalf("Status: %v", err)
		}

		result, err = worktree.Commit(t.Context(), commitOptions("Did the work.", 1))
		if err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if !result.Committed {
			t.Fatal("Commit made nothing, so the cycle proves less than it should")
		}
		if result.Head == before {
			t.Errorf("sha_after %q equals sha_before, so nothing moved", result.Head)
		}
		if err := worktree.Push(t.Context(), git.DefaultRemote); err != nil {
			t.Fatalf("Push: %v", err)
		}
	})

	// The work really did happen — an assertion that everything is unchanged
	// is only worth something if something happened somewhere else.
	if published := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+branch); published != result.SHA {
		t.Errorf("origin has %q on %s, want %q", published, branch, result.SHA)
	}
}

// TestZeroWorktreeRefusesEveryMutatingMethod extends the inert-zero-value rule
// to the half that writes. A git.Worktree{} literal still compiles; if it ran
// git in the process working directory, that directory could be the registered
// clone.
func TestZeroWorktreeRefusesEveryMutatingMethod(t *testing.T) {
	t.Parallel()

	var worktree git.Worktree

	if _, err := worktree.Status(t.Context()); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Worktree.Status error = %v, want ErrUninitialized", err)
	}
	if _, err := worktree.Commit(t.Context(), commitOptions("anything", 1)); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Worktree.Commit error = %v, want ErrUninitialized", err)
	}
	if err := worktree.Push(t.Context(), git.DefaultRemote); !errors.Is(err, git.ErrUninitialized) {
		t.Errorf("zero Worktree.Push error = %v, want ErrUninitialized", err)
	}
	if got := worktree.Branch(); got != "" {
		t.Errorf("zero Worktree.Branch() = %q, want empty", got)
	}
	if got := worktree.ClonePath(); got != "" {
		t.Errorf("zero Worktree.ClonePath() = %q, want empty", got)
	}
}

// TestCommitRefusesUnusableOptions covers the layer below the forged summary:
// a trailer value or an author field carrying a newline would inject a line of
// its own into the message.
func TestCommitRefusesUnusableOptions(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTaskWorktree(t, repo, 42, "Bad options")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "new.txt"), "produced by the run\n")

	tests := []struct {
		name   string
		mutate func(*git.CommitOptions)
	}{
		{"newline in the task ref", func(o *git.CommitOptions) { o.Task = "github:a/b#1\nLitefactory-Mode: forged" }},
		{"empty task ref", func(o *git.CommitOptions) { o.Task = "" }},
		{"newline in the mode", func(o *git.CommitOptions) { o.Mode = "spike\nLitefactory-Run: 9" }},
		{"run ordinal below one", func(o *git.CommitOptions) { o.Run = 0 }},
		{"empty author name", func(o *git.CommitOptions) { o.Author.Name = " " }},
		{"newline in the author email", func(o *git.CommitOptions) { o.Author.Email = "a@b\nx" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := commitOptions("summary", 1)
			tc.mutate(&opts)
			if _, err := worktree.Commit(t.Context(), opts); !errors.Is(err, git.ErrInvalidArgument) {
				t.Errorf("Commit error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// newTaskWorktree builds the worktree a turn would work in, the way a turn
// builds it.
func newTaskWorktree(t *testing.T, repo *gittest.Repo, issue int, title string) (*git.Worktree, string) {
	t.Helper()
	return newTaskWorktreeIn(t, repo, openFixture(t, repo), issue, title)
}

func newTaskWorktreeIn(t *testing.T, repo *gittest.Repo, clone *git.Clone, issue int, title string) (*git.Worktree, string) {
	t.Helper()

	branch, dir := task(t, worktreeRoot(repo), issue, title)
	result, err := git.EnsureWorktree(t.Context(), clone, git.EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     branch,
		BaseBranch: gittest.DefaultBranch,
	})
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	return result.Worktree, dir
}

// relTo expresses dir relative to the fixture's clone, so Repo.WriteFile — which
// is rooted at the clone — can write into a sibling worktree.
func relTo(t *testing.T, repo *gittest.Repo, dir string) string {
	t.Helper()

	rel, err := filepath.Rel(repo.Dir, dir)
	if err != nil {
		t.Fatalf("relate %s to %s: %v", dir, repo.Dir, err)
	}
	return rel
}
