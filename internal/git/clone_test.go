package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

// resolveGit resolves the git binary once per test. Resolution is part of what
// is under test: a daemon started by launchd searches only
// /usr/bin:/bin:/usr/sbin:/sbin, so "is git actually there and does it run" has
// to be answered at startup rather than three hours into a run.
func resolveGit(t *testing.T) *git.Binary {
	t.Helper()
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !filepath.IsAbs(binary.Path()) {
		t.Fatalf("Resolve returned a relative path %q", binary.Path())
	}
	if !strings.Contains(binary.Version(), "git version") {
		t.Fatalf("Resolve reported version %q", binary.Version())
	}
	return binary
}

func openFixture(t *testing.T, repo *gittest.Repo) *git.Clone {
	t.Helper()
	clone, err := resolveGit(t).Open(t.Context(), repo.Dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", repo.Dir, err)
	}
	return clone
}

func TestResolveNamesThePathItSearched(t *testing.T) {
	// Setenv forbids t.Parallel.
	t.Setenv("PATH", t.TempDir())

	_, err := git.Resolve(t.Context())
	if err == nil {
		t.Fatal("Resolve succeeded with an empty PATH")
	}
	var notFound *git.BinaryNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error %v is not a *git.BinaryNotFoundError", err)
	}
	if !strings.Contains(err.Error(), os.Getenv("PATH")) {
		t.Errorf("error %q does not name the PATH that was searched", err)
	}
}

func TestOpenCanonicalisesThePath(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	binary := resolveGit(t)

	// A symlink to the repository, a subdirectory of it and the repository
	// itself must all resolve to the same canonical, absolute path — that is
	// what makes the unique path column in the repos table meaningful.
	link := filepath.Join(filepath.Dir(repo.Dir), "link-to-clone")
	if err := os.Symlink(repo.Dir, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	repo.WriteFile("sub/nested/file.txt", "hello\n")
	repo.Commit("add a subdirectory")

	for _, path := range []string{repo.Dir, link, filepath.Join(link, "sub"), filepath.Join(repo.Dir, "sub", "nested")} {
		clone, err := binary.Open(t.Context(), path)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		if clone.Path() != repo.Dir {
			t.Errorf("Open(%s).Path() = %q, want %q", path, clone.Path(), repo.Dir)
		}
	}
}

func TestOpenRejectsWhatIsNotAWorkingClone(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	binary := resolveGit(t)
	parent := filepath.Dir(repo.Dir)

	plain := filepath.Join(parent, "not-a-repo")
	if err := os.MkdirAll(plain, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	tests := []struct {
		name string
		path string
		want error
	}{
		{name: "plain directory", path: plain, want: git.ErrNotARepository},
		{name: "bare repository", path: repo.Origin, want: git.ErrBareRepository},
		{name: "missing path", path: filepath.Join(parent, "nowhere"), want: os.ErrNotExist},
		{name: "regular file", path: filepath.Join(repo.Dir, "README.md"), want: git.ErrNotDirectory},
		{name: "empty path", path: "", want: git.ErrInvalidArgument},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			clone, err := binary.Open(t.Context(), tc.path)
			if err == nil {
				t.Fatalf("Open(%s) = %v, want an error", tc.path, clone.Path())
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Open(%s) error = %v, want %v", tc.path, err, tc.want)
			}
		})
	}
}

// TestReadOnlyOperationsLeaveTheCloneUntouched is AC-1.7. The fixture is dirtied
// first on purpose: against a clean checkout the assertion passes trivially and
// would not notice a bug that reset the index.
//
// Fetch is included because it is the one operation here that writes to the
// repository at all — it moves remote-tracking refs. What it must not touch is
// the working tree, the index or HEAD, which is exactly what the snapshot
// covers.
func TestReadOnlyOperationsLeaveTheCloneUntouched(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()

	status := repo.Git("status", "--porcelain")
	if status == "" {
		t.Fatal("the fixture is clean, so this assertion would prove nothing")
	}

	clone := openFixture(t, repo)
	ctx := t.Context()

	gittest.AssertUnchanged(t, repo.Dir, func() {
		if err := clone.Fetch(ctx, "origin"); err != nil {
			t.Fatalf("Fetch: %v", err)
		}
		if _, err := clone.RevParse(ctx, "HEAD", git.RevParseOptions{}); err != nil {
			t.Fatalf("RevParse: %v", err)
		}
		if _, err := clone.RemoteURL(ctx, "origin"); err != nil {
			t.Fatalf("RemoteURL: %v", err)
		}
		if _, err := clone.ConfigGet(ctx, "remote.origin.url"); err != nil {
			t.Fatalf("ConfigGet: %v", err)
		}
		if _, err := clone.ShowRef(ctx, "refs/heads/"+gittest.DefaultBranch); err != nil {
			t.Fatalf("ShowRef: %v", err)
		}
		if _, err := clone.WorktreeList(ctx); err != nil {
			t.Fatalf("WorktreeList: %v", err)
		}
	})
}

// TestWorktreeAddLeavesTheCloneUntouched is the same assertion across the one
// operation that does create something. The worktree's administrative entry
// lives under .git by necessity; the clone's working tree, index and HEAD still
// must not move.
func TestWorktreeAddLeavesTheCloneUntouched(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.Dirty()
	clone := openFixture(t, repo)

	dir := filepath.Join(filepath.Dir(repo.Dir), "worktrees", "task-42")
	var worktree *git.Worktree

	gittest.AssertUnchanged(t, repo.Dir, func() {
		var err error
		worktree, err = clone.WorktreeAdd(t.Context(), dir, git.WorktreeAddOptions{
			Branch:     "lf/42-example",
			CreateFrom: "origin/" + gittest.DefaultBranch,
		})
		if err != nil {
			t.Fatalf("WorktreeAdd: %v", err)
		}
	})

	if worktree.Path() != dir {
		t.Errorf("Worktree.Path() = %q, want %q", worktree.Path(), dir)
	}
	if worktree.Branch() != "lf/42-example" {
		t.Errorf("Worktree.Branch() = %q, want %q", worktree.Branch(), "lf/42-example")
	}
	if worktree.ClonePath() != repo.Dir {
		t.Errorf("Worktree.ClonePath() = %q, want %q", worktree.ClonePath(), repo.Dir)
	}

	head, err := worktree.RevParse(t.Context(), "HEAD", git.RevParseOptions{AbbrevRef: true})
	if err != nil {
		t.Fatalf("Worktree.RevParse: %v", err)
	}
	if head != "lf/42-example" {
		t.Errorf("worktree HEAD is on %q, want %q", head, "lf/42-example")
	}

	// The listing reports data, never a Worktree: its first entry is the
	// registered clone itself.
	list, err := clone.WorktreeList(t.Context())
	if err != nil {
		t.Fatalf("WorktreeList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("WorktreeList returned %d entries, want 2: %+v", len(list), list)
	}
	if list[0].Path != repo.Dir {
		t.Errorf("first worktree = %q, want the registered clone %q", list[0].Path, repo.Dir)
	}
	if list[1].Path != dir || list[1].Branch != "refs/heads/lf/42-example" {
		t.Errorf("second worktree = %+v, want %s on refs/heads/lf/42-example", list[1], dir)
	}

	if err := clone.WorktreeRemove(t.Context(), dir, git.WorktreeRemoveOptions{}); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("worktree directory still exists after removal: %v", err)
	}
}

// TestWorktreeAddRefusesPathsOverlappingTheClone is the runtime half of the
// compile-time guarantee. A Worktree is the only handle that can write, so a
// Worktree pointing inside the registered clone would defeat the whole design —
// including via a symlink that hides where the path really leads.
func TestWorktreeAddRefusesPathsOverlappingTheClone(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	parent := filepath.Dir(repo.Dir)

	disguised := filepath.Join(parent, "innocent-looking")
	if err := os.Symlink(repo.Dir, disguised); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	tests := []struct {
		name string
		dir  string
	}{
		{name: "the clone itself", dir: repo.Dir},
		{name: "inside the clone", dir: filepath.Join(repo.Dir, "worktrees", "task-1")},
		{name: "inside the clone's git directory", dir: filepath.Join(repo.Dir, ".git", "task-1")},
		{name: "through a symlink to the clone", dir: filepath.Join(disguised, "task-1")},
		{name: "a parent of the clone", dir: parent},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			worktree, err := clone.WorktreeAdd(t.Context(), tc.dir, git.WorktreeAddOptions{
				Branch:     "lf/1-nope",
				CreateFrom: "HEAD",
			})
			if err == nil {
				t.Fatalf("WorktreeAdd(%s) produced a worktree at %s, want a refusal", tc.dir, worktree.Path())
			}
			if !errors.Is(err, git.ErrWorktreeOverlapsClone) {
				t.Fatalf("WorktreeAdd(%s) error = %v, want ErrWorktreeOverlapsClone", tc.dir, err)
			}
		})
	}
}

func TestMissingThingsAreNamedConditions(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	ctx := t.Context()

	if _, err := clone.RemoteURL(ctx, "upstream"); !errors.Is(err, git.ErrRemoteNotFound) {
		t.Errorf("RemoteURL(upstream) error = %v, want ErrRemoteNotFound", err)
	}
	if _, err := clone.ConfigGet(ctx, "litefactory.nothing"); !errors.Is(err, git.ErrConfigKeyNotFound) {
		t.Errorf("ConfigGet error = %v, want ErrConfigKeyNotFound", err)
	}
	if _, err := clone.ShowRef(ctx, "refs/heads/no-such-branch"); !errors.Is(err, git.ErrRefNotFound) {
		t.Errorf("ShowRef error = %v, want ErrRefNotFound", err)
	}
	if _, err := clone.RevParse(ctx, "no-such-revision", git.RevParseOptions{}); !errors.Is(err, git.ErrRevisionNotFound) {
		t.Errorf("RevParse error = %v, want ErrRevisionNotFound", err)
	}
}

// TestFailuresCarryTheCommandAndStderr covers what several acceptance criteria
// ask the CLI to print: the exact command that failed and what git said about
// it, as fields rather than as an already-formatted sentence.
func TestFailuresCarryTheCommandAndStderr(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)

	_, err := clone.ShowRef(t.Context(), "refs/heads/no-such-branch")
	if err == nil {
		t.Fatal("ShowRef succeeded for a missing ref")
	}

	var cmdErr *git.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error %v is not a *git.CommandError", err)
	}
	if want := "show-ref --verify -- refs/heads/no-such-branch"; !strings.HasSuffix(cmdErr.Command(), want) {
		t.Errorf("Command() = %q, want it to end with %q", cmdErr.Command(), want)
	}
	if !filepath.IsAbs(cmdErr.Binary) || !strings.HasPrefix(cmdErr.Command(), cmdErr.Binary+" ") {
		t.Errorf("Command() = %q, want it to start with the absolute git binary %q", cmdErr.Command(), cmdErr.Binary)
	}
	if cmdErr.Stderr == "" {
		t.Error("CommandError.Stderr is empty; git's diagnostics were dropped")
	}
	if cmdErr.ExitCode == 0 {
		t.Errorf("CommandError.ExitCode = %d, want non-zero", cmdErr.ExitCode)
	}
	if cmdErr.Dir != repo.Dir {
		t.Errorf("CommandError.Dir = %q, want %q", cmdErr.Dir, repo.Dir)
	}
	if !strings.Contains(err.Error(), cmdErr.Stderr) {
		t.Errorf("error %q does not include git's stderr %q", err, cmdErr.Stderr)
	}
}

// TestOptionLookalikesAreRefused checks that a caller-supplied value cannot
// reach git as an option. "--upload-pack=..." wearing the costume of a remote
// name is the classic way to turn a repository URL into command execution.
func TestOptionLookalikesAreRefused(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)
	ctx := t.Context()

	if err := clone.Fetch(ctx, "--upload-pack=touch /tmp/pwned"); !errors.Is(err, git.ErrInvalidArgument) {
		t.Errorf("Fetch error = %v, want ErrInvalidArgument", err)
	}
	if _, err := clone.RemoteURL(ctx, "-x"); !errors.Is(err, git.ErrInvalidArgument) {
		t.Errorf("RemoteURL error = %v, want ErrInvalidArgument", err)
	}
	if _, err := clone.RevParse(ctx, "--version", git.RevParseOptions{}); !errors.Is(err, git.ErrInvalidArgument) {
		t.Errorf("RevParse error = %v, want ErrInvalidArgument", err)
	}
	if _, err := clone.ConfigGet(ctx, ""); !errors.Is(err, git.ErrInvalidArgument) {
		t.Errorf("ConfigGet error = %v, want ErrInvalidArgument", err)
	}
}

// TestCancellationIsReported checks the failure attribution the daemon depends
// on: a cancelled context is a cancellation, not a mysterious git failure.
func TestCancellationIsReported(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	clone := openFixture(t, repo)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := clone.RevParse(ctx, "HEAD", git.RevParseOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("RevParse on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestRemoteURLFeedsTheParser closes the loop between the two halves of LF-1:
// what git reports is what the parser has to accept.
func TestRemoteURLFeedsTheParser(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	repo.SetOriginURL("git@github.com:cloudvane/litefactory.git")
	clone := openFixture(t, repo)

	url, err := clone.RemoteURL(t.Context(), "origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	ref, err := git.ParseRemoteURL(url)
	if err != nil {
		t.Fatalf("ParseRemoteURL(%q): %v", url, err)
	}
	if got := ref.String(); got != "github:cloudvane/litefactory" {
		t.Errorf("parsed %q, want %q", got, "github:cloudvane/litefactory")
	}
}
