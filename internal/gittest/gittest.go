// Package gittest builds throwaway git repositories for tests and asserts that
// a repository was left untouched.
//
// # Test support only
//
// This package imports testing and must never be imported by non-test code; a
// test in this package enforces that by walking the module. It is also the one
// documented exception to "internal/git is the only package that execs git" —
// fixtures have to commit, and internal/git deliberately cannot. That exception
// is safe precisely because nothing shipped in the binary can reach this code.
//
// # Why fixtures rather than mocks
//
// The guarantees this project cares about are git's behaviour, not our model of
// it: whether "git worktree add" refuses an occupied path, whether an operation
// leaves the index alone. A mock would only ever confirm what we already
// believed. Every fixture here is a real repository built by real git.
//
// Fixtures are hermetic. The developer's global and system git configuration is
// switched off, HOME points into the temporary directory, identity and dates
// come from the environment, and the initial branch is always main — so a
// contributor with commit.gpgsign or a different init.defaultBranch gets the
// same results as CI.
package gittest

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultBranch is the branch every fixture starts on, independent of whatever
// init.defaultBranch the developer has configured.
const DefaultBranch = "main"

// Fixture commit identity and timestamp. Fixing the date keeps object ids
// reproducible from one run to the next.
const (
	authorName  = "Litefactory Fixture"
	authorEmail = "fixture@litefactory.invalid"
	commitDate  = "2026-01-01T00:00:00+00:00"
)

// commandTimeout bounds a fixture git command, so a hung git fails the test
// with a clear message instead of waiting for the package timeout.
const commandTimeout = 60 * time.Second

// Repo is a temporary git repository: a working clone with one commit on
// [DefaultBranch] and a bare origin it has already pushed to. Everything lives
// under the test's temporary directory and is removed when the test ends.
type Repo struct {
	// Dir is the working clone. It is absolute and symlink-resolved, so it
	// compares equal to the path git reports for the same repository.
	Dir string
	// Origin is the bare repository that Dir's "origin" remote points at. It
	// is a local path, so pushing to it makes no network call.
	Origin string

	tb   testing.TB
	home string
}

type templateFile struct {
	path string
	mode fs.FileMode
	data []byte
}

var fixtureTemplate struct {
	sync.Once
	files  []templateFile
	origin string
}

// New creates a fixture repository and returns it. The clone has one commit
// (README.md) on [DefaultBranch], an "origin" remote pointing at a local bare
// repository, an upstream tracking branch, and origin/HEAD set — everything a
// caller needs to exercise fetch, base-branch resolution and worktrees.
func New(tb testing.TB) *Repo {
	tb.Helper()
	fixtureTemplate.Do(func() {
		r := buildTemplate(tb)
		root := filepath.Dir(r.Dir)
		fixtureTemplate.origin = r.Origin
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == root {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			f := templateFile{path: rel, mode: info.Mode()}
			if !entry.IsDir() {
				// #nosec G304 G122 -- path comes from walking this test's freshly created git fixture; no untrusted writers.
				f.data, err = os.ReadFile(path)
				if err != nil {
					return err
				}
			}
			fixtureTemplate.files = append(fixtureTemplate.files, f)
			return nil
		})
		if err != nil {
			tb.Fatalf("gittest: capture template: %v", err)
		}
	})
	// Keep bytes, not paths into the first test's temporary directory. Each
	// fixture receives independent files, including its Git objects and index.
	root, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatalf("gittest: resolve temporary directory: %v", err)
	}
	r := &Repo{Dir: filepath.Join(root, "clone"), Origin: filepath.Join(root, "origin.git"), tb: tb, home: filepath.Join(root, "home")}
	for _, f := range fixtureTemplate.files {
		path := filepath.Join(root, f.path)
		if f.mode.IsDir() {
			err = os.MkdirAll(path, f.mode.Perm())
		} else {
			data := f.data
			if f.path == filepath.Join("clone", ".git", "config") {
				data = []byte(strings.ReplaceAll(string(data), fixtureTemplate.origin, r.Origin))
			}
			err = os.WriteFile(path, data, f.mode.Perm())
		}
		if err != nil {
			tb.Fatalf("gittest: restore %s: %v", f.path, err)
		}
	}
	return r
}

func buildTemplate(tb testing.TB) *Repo {
	tb.Helper()

	// t.TempDir hands back a path under /var, which on macOS is a symlink to
	// /private/var. Resolving it here is what lets a test compare Repo.Dir
	// with the path git reports.
	root, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatalf("gittest: resolve temporary directory: %v", err)
	}

	r := &Repo{
		Dir:    filepath.Join(root, "clone"),
		Origin: filepath.Join(root, "origin.git"),
		tb:     tb,
		home:   filepath.Join(root, "home"),
	}
	if err := os.MkdirAll(r.home, 0o700); err != nil {
		tb.Fatalf("gittest: create fixture home: %v", err)
	}

	r.GitIn(root, "init", "--quiet", "--bare", "--initial-branch="+DefaultBranch, r.Origin)
	r.GitIn(root, "init", "--quiet", "--initial-branch="+DefaultBranch, r.Dir)
	r.Git("remote", "add", "origin", r.Origin)
	r.WriteFile("README.md", "# fixture\n")
	r.Commit("initial commit")
	r.Git("push", "--quiet", "--set-upstream", "origin", DefaultBranch)
	r.Git("remote", "set-head", "origin", "--auto")
	return r
}

// Git runs a git command in the working clone and returns its trimmed standard
// output. A non-zero exit fails the test with the command and its stderr.
func (r *Repo) Git(args ...string) string {
	r.tb.Helper()
	return r.GitIn(r.Dir, args...)
}

// GitIn runs a git command in dir. It is the escape hatch for the commands that
// operate on the origin or on a worktree rather than on the clone.
func (r *Repo) GitIn(dir string, args ...string) string {
	r.tb.Helper()
	return run(r.tb, r.home, dir, args...)
}

// WriteFile writes content to a path relative to the working clone, creating
// parent directories as needed.
func (r *Repo) WriteFile(rel, content string) {
	r.tb.Helper()
	full := filepath.Join(r.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		r.tb.Fatalf("gittest: create directory for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.tb.Fatalf("gittest: write %s: %v", rel, err)
	}
}

// Commit stages everything in the working clone and commits it, returning the
// new object id.
func (r *Repo) Commit(message string) string {
	r.tb.Helper()
	r.Git("add", "--all")
	r.Git("commit", "--quiet", "--message", message)
	return r.Head()
}

// Head returns the object id HEAD points at.
func (r *Repo) Head() string {
	r.tb.Helper()
	return r.Git("rev-parse", "HEAD")
}

// Branch returns the checked-out branch name.
func (r *Repo) Branch() string {
	r.tb.Helper()
	return r.Git("rev-parse", "--abbrev-ref", "HEAD")
}

// SetOriginURL rewrites the origin remote's URL. Tests that need a GitHub-shaped
// remote — to exercise URL parsing or registration — use this after the fixture
// has finished pushing to its local origin.
func (r *Repo) SetOriginURL(url string) {
	r.tb.Helper()
	r.Git("remote", "set-url", "origin", url)
}

// RemoveOrigin deletes the origin remote, for the "repository without an origin"
// case.
func (r *Repo) RemoveOrigin() {
	r.tb.Helper()
	r.Git("remote", "remove", "origin")
}

// Dirty leaves the working clone in a deliberately messy state: a staged
// addition, an unstaged modification and an untracked file, so that status,
// index and working tree all have something to lose.
//
// This matters. An untouched-clone assertion against a clean repository passes
// trivially and proves nothing — a bug that wiped the index would still look
// like success. Dirty the fixture first and the assertion has something to
// detect.
func (r *Repo) Dirty() {
	r.tb.Helper()
	r.WriteFile("staged.txt", "staged addition\n")
	r.Git("add", "staged.txt")
	r.WriteFile("README.md", "# fixture\nunstaged modification\n")
	r.WriteFile("untracked.txt", "untracked\n")
}

// run executes git in dir with a hermetic environment and fails the test on any
// error.
func run(tb testing.TB, home, dir string, args ...string) string {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), commandTimeout)
	defer cancel()

	// #nosec G204 -- Test support only, and deliberately the one place that execs
	// git directly: fixtures have to commit, which internal/git's Clone has no
	// way to do. TestOnlyTestsImportGittest enforces that no shipped file can
	// reach this package, and args are fixture literals written by the tests.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env(home)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		tb.Fatalf("gittest: git %s (in %s): %v\n%s",
			strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	// Only the trailing newline goes: "git status --porcelain" encodes the
	// index and working-tree state in the first two columns, so a leading
	// space is data, not padding.
	return strings.TrimRight(stdout.String(), "\r\n")
}

// env builds the hermetic fixture environment. The developer's global and
// system configuration is switched off deliberately: an ambient commit.gpgsign,
// core.hooksPath or init.defaultBranch would otherwise change what these
// fixtures do from one machine to the next.
func env(home string) []string {
	path := os.Getenv("PATH")
	return []string{
		"PATH=" + path,
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=" + authorName,
		"GIT_AUTHOR_EMAIL=" + authorEmail,
		"GIT_AUTHOR_DATE=" + commitDate,
		"GIT_COMMITTER_NAME=" + authorName,
		"GIT_COMMITTER_EMAIL=" + authorEmail,
		"GIT_COMMITTER_DATE=" + commitDate,
	}
}
