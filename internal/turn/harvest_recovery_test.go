package turn

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/harness"
)

func commitCount(t *testing.T, f *fixture, branch string) int {
	t.Helper()
	out := f.repo.GitIn(f.repo.Origin, "rev-list", "--count", "refs/heads/"+branch)
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("parse commit count %q: %v", out, err)
	}
	return n
}

func originFile(t *testing.T, f *fixture, branch, path string) string {
	t.Helper()
	return f.repo.GitIn(f.repo.Origin, "show", branch+":"+path)
}

func req2(f *fixture, body string) Request {
	req := f.request()
	req.Trigger.CommentID = testComment + 1
	req.Trigger.Body = body
	req.Command = ParseCommand(body)
	return req
}

func lockIndex(t *testing.T, worktree string) {
	t.Helper()

	gitPath := filepath.Join(worktree, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat %s: %v", gitPath, err)
	}

	gitDir := gitPath
	if !info.IsDir() {
		contents, err := os.ReadFile(gitPath) //nolint:gosec // a test-owned path
		if err != nil {
			t.Fatalf("read %s: %v", gitPath, err)
		}
		pointer, ok := strings.CutPrefix(strings.TrimSpace(string(contents)), "gitdir:")
		if !ok {
			t.Fatalf("%s is a file but does not name a gitdir: %q", gitPath, contents)
		}
		gitDir = strings.TrimSpace(pointer)
	}

	lock := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatalf("take the index lock at %s: %v", lock, err)
	}
	t.Cleanup(func() { _ = os.Remove(lock) })
}

// A failed run's work is committed and pushed like any other (#488: git is the
// record), and the next run builds on it.
func TestCommitAndPushKeepsFailedRunWorkAndTheNextRunBuildsOnIt(t *testing.T) {
	t.Parallel()

	var f *fixture
	runner := newStubRunner(func(_ context.Context, req harness.Request) (*harness.RunResult, error) {
		if req.RunOrdinal == 1 {
			writeInWorktree(t, req, "leftover.go", "package api // left by the failed run\n")
			result := succeeded(req, f.paths.Home())
			result.Outcome = harness.OutcomeFailed
			result.ExitCode = 2
			result.Reason = "claude exited with status 2"
			return result, nil
		}
		writeInWorktree(t, req, "finished.go", "package api // written by the succeeding run\n")
		return succeeded(req, f.paths.Home()), nil
	})
	f = newFixture(t, runner)

	first := f.execute(f.request())
	if first.Succeeded() {
		t.Fatalf("the first run was reported as a success: %+v", first)
	}
	if !first.Committed || !first.Pushed {
		t.Fatalf("committed=%t pushed=%t after the failed run, want both", first.Committed, first.Pushed)
	}
	if content := originFile(t, f, testBranch, "leftover.go"); strings.TrimSpace(content) == "" {
		t.Fatalf("origin's %s is missing the failed run's leftover.go", testBranch)
	}

	second := f.execute(req2(f, "/spike finish the job"))
	if !second.Succeeded() {
		t.Fatalf("the second run did not succeed: %+v\nlogs:\n%s", second, f.logs)
	}
	if !second.Committed || !second.Pushed {
		t.Errorf("committed=%t pushed=%t, want both on the run that finally succeeded", second.Committed, second.Pushed)
	}

	for _, name := range []string{"leftover.go", "finished.go"} {
		content := originFile(t, f, testBranch, name)
		if strings.TrimSpace(content) == "" {
			t.Errorf("origin's %s is missing %s", testBranch, name)
		}
	}
	if got := commitCount(t, f, testBranch); got != 3 {
		t.Errorf("branch carries %d commits, want 3 (the base commit, the failed run's and the succeeding run's)", got)
	}
}
