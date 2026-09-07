package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
)

// timeoutTestAuthor is the identity every merge attempt in this file is
// authored as. It mirrors internal/git/merge_test.go's own testAuthor, kept
// as a separate value because that one lives in the external test package
// (git_test) and is not visible from here.
var timeoutTestAuthor = Author{Name: "Mario Meir-Huber", Email: "mario@example.invalid"}

// newTimeoutTestWorktree builds the worktree a turn would work in, the way a
// turn builds it, using only exported constructors — [Resolve], [Binary.Open]
// and [EnsureWorktree] — the same three internal/git/worktree_test.go's own
// newTaskWorktree composes. It is redeclared here, rather than reused, only
// because that helper lives in package git_test and this file must be
// package git to reach [Worktree.recoverFromFailedMerge] and its own
// unexported runner field.
func newTimeoutTestWorktree(t *testing.T, repo *gittest.Repo, issue int, title string) (*Worktree, string) {
	t.Helper()

	binary, err := Resolve(t.Context())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
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
		t.Fatalf("naming.Branch(%d, %q): %v", issue, slug, err)
	}
	root := filepath.Join(filepath.Dir(repo.Dir), ".lf-worktrees", filepath.Base(repo.Dir))
	dir, err := naming.WorktreePath(root, issue)
	if err != nil {
		t.Fatalf("naming.WorktreePath(%q, %d): %v", root, issue, err)
	}

	result, err := EnsureWorktree(t.Context(), clone, EnsureWorktreeOptions{
		Dir:        dir,
		Branch:     branch,
		BaseBranch: gittest.DefaultBranch,
	})
	if err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	return result.Worktree, dir
}

// conflictBaseAndBranchOnREADME makes base and branch each edit README.md
// differently — base pushed to origin, branch committed directly into dir —
// so that a real "git merge" between them cannot resolve on its own and
// leaves MERGE_HEAD behind, exactly the condition [recoverFromFailedMerge]
// exists to recover from.
func conflictBaseAndBranchOnREADME(t *testing.T, repo *gittest.Repo, dir string) {
	t.Helper()

	repo.WriteFile("README.md", "# fixture\nbase change\n")
	repo.Commit("base edits README.md")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# fixture\nbranch change\n"), 0o600); err != nil {
		t.Fatalf("write branch README.md: %v", err)
	}
	repo.GitIn(dir, "add", "--all")
	repo.GitIn(dir, "commit", "--quiet", "--message", "branch edits README.md")
}

// runRealConflictingMerge drives a real "git merge --no-ff --no-edit" between
// base and worktree's branch directly through the worktree's own runner,
// bypassing [Worktree.MergeBase] entirely so its own recovery never runs. The
// merge is expected to fail because base and branch conflict on the same
// file, which is what leaves a real MERGE_HEAD in dir for the rest of the
// test to recover from — the same state a per-command timeout partway
// through a real merge would also leave behind (US-10, S-8).
func runRealConflictingMerge(t *testing.T, worktree *Worktree, base string) error {
	t.Helper()

	args := []string{
		"-c", "user.name=" + timeoutTestAuthor.Name,
		"-c", "user.email=" + timeoutTestAuthor.Email,
		"-c", "commit.gpgsign=false",
		"merge", "--no-ff", "--no-edit", "--end-of-options", base,
	}
	_, err := worktree.runner.run(t.Context(), mergeTimeout, args...)
	if err == nil {
		t.Fatal("the real merge succeeded; base and branch were supposed to conflict on README.md")
	}
	return err
}

// TestRecoverFromFailedMergeTimeout is T-10.3's core case: a merge that fails
// because the per-command deadline fired, not because git rejected it
// outright, is recovered exactly the way a conflict is — abort, then
// re-verify HEAD landed back at the SHA captured before the merge started
// (AC-10.1) — but is reported through [ErrMergeTimedOut] rather than
// [ErrMergeConflict] so a disclosure can tell the two apart (AC-10.2), and
// the CommandError's own diagnostic fields survive the substitution
// untouched.
//
// [recoverFromFailedMerge] is exercised directly against a worktree left in a
// real, unresolved conflict — MERGE_HEAD present, produced by a real "git
// merge" this test runs itself via [runRealConflictingMerge] — rather than
// waiting out the real 120s mergeTimeout to reach a timeout through
// [Worktree.MergeBase].
func TestRecoverFromFailedMergeTimeout(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTimeoutTestWorktree(t, repo, 42, "Timed out merge rolls back")
	base := "origin/" + gittest.DefaultBranch
	conflictBaseAndBranchOnREADME(t, repo, dir)

	before, err := worktree.RevParse(t.Context(), "HEAD", RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}

	// Base only moved on origin so far; the worktree's remote-tracking ref
	// has to be refreshed before "base" resolves to it.
	repo.GitIn(dir, "fetch", "--quiet", "origin")

	realMergeErr := runRealConflictingMerge(t, worktree, base)
	var realCmdErr *CommandError
	if !errors.As(realMergeErr, &realCmdErr) {
		t.Fatalf("the real conflicting merge error %v is not a *CommandError", realMergeErr)
	}

	// A synthetic timeout, built the same shape [classifyRunError] gives a
	// real one — TimedOut true, Err is context.DeadlineExceeded, everything
	// else naming the command that (really) failed — standing in for the
	// per-command deadline this test does not wait out.
	synthetic := &CommandError{
		Binary:   realCmdErr.Binary,
		Dir:      realCmdErr.Dir,
		Args:     realCmdErr.Args,
		ExitCode: -1,
		Stderr:   realCmdErr.Stderr,
		TimedOut: true,
		Timeout:  mergeTimeout,
		Err:      context.DeadlineExceeded,
	}

	result, err := worktree.recoverFromFailedMerge(t.Context(), base, before, synthetic)

	if err == nil {
		t.Fatal("recoverFromFailedMerge succeeded, want an error reporting the timeout")
	}
	if !errors.Is(err, ErrMergeTimedOut) {
		t.Errorf("error = %v, want ErrMergeTimedOut", err)
	}
	if errors.Is(err, ErrMergeConflict) {
		t.Errorf("error = %v, wrongly classified as ErrMergeConflict instead of ErrMergeTimedOut", err)
	}
	if result != (MergeResult{}) {
		t.Errorf("result = %+v, want the zero value on failure", result)
	}

	var classified *CommandError
	if !errors.As(err, &classified) {
		t.Fatalf("error %v is not a *CommandError, so the diagnostic fields were not preserved", err)
	}
	if classified.Binary != synthetic.Binary {
		t.Errorf("Binary = %q, want %q", classified.Binary, synthetic.Binary)
	}
	if classified.Dir != synthetic.Dir {
		t.Errorf("Dir = %q, want %q", classified.Dir, synthetic.Dir)
	}
	if len(classified.Args) != len(synthetic.Args) {
		t.Errorf("Args = %v, want %v", classified.Args, synthetic.Args)
	}
	if classified.ExitCode != synthetic.ExitCode {
		t.Errorf("ExitCode = %d, want %d", classified.ExitCode, synthetic.ExitCode)
	}
	if classified.Stderr != synthetic.Stderr {
		t.Errorf("Stderr = %q, want %q", classified.Stderr, synthetic.Stderr)
	}
	if !classified.TimedOut {
		t.Error("TimedOut = false, want true: preserved from the synthetic error")
	}
	if classified.Timeout != synthetic.Timeout {
		t.Errorf("Timeout = %v, want %v", classified.Timeout, synthetic.Timeout)
	}

	// AC-10.1: the branch tip is byte-identical to before the attempt began,
	// verified by explicit comparison against the SHA captured earlier in
	// this test, not inferred from the absence of an error.
	after, err := worktree.RevParse(t.Context(), "HEAD", RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD after recovery: %v", err)
	}
	if after != before {
		t.Errorf("branch tip moved from %s to %s despite the rollback", before, after)
	}

	// The abort is expected to have cleaned up MERGE_HEAD and the conflicted
	// index state the real merge left behind, leaving nothing uncommitted.
	changes, err := worktree.Status(t.Context())
	if err != nil {
		t.Fatalf("Status after recovery: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("Status after recovery = %v, want no changes: the abort should have left a clean worktree", changes)
	}
}

// TestRecoverFromFailedMergeTimeoutAbortFailurePreservesOriginalError pins
// what happens if the abort step itself cannot run — CR-A-004's documented,
// today-unreachable risk that recoverFromFailedMerge's abort call reuses the
// caller's ctx rather than a fresh one. An already-cancelled ambient ctx
// makes the abort fail before it can do anything, and the guarantee this
// pins is that the failure is reported as its own condition rather than
// being misreported as the timeout having been handled: the caller never
// sees [ErrMergeTimedOut] for a recovery that did not actually complete.
//
// This is optional, cheap-to-write documentation of current (safe) behaviour
// per CR-A-004, not a required acceptance criterion — the condition is not
// reachable from production code today because nothing in this tree cancels
// the ambient context independently of the per-command timeout.
func TestRecoverFromFailedMergeTimeoutAbortFailurePreservesOriginalError(t *testing.T) {
	t.Parallel()

	repo := gittest.New(t)
	worktree, dir := newTimeoutTestWorktree(t, repo, 43, "Ambient cancellation during abort")
	base := "origin/" + gittest.DefaultBranch
	conflictBaseAndBranchOnREADME(t, repo, dir)

	before, err := worktree.RevParse(t.Context(), "HEAD", RevParseOptions{})
	if err != nil {
		t.Fatalf("RevParse HEAD before merge: %v", err)
	}
	repo.GitIn(dir, "fetch", "--quiet", "origin")
	realMergeErr := runRealConflictingMerge(t, worktree, base)
	var realCmdErr *CommandError
	if !errors.As(realMergeErr, &realCmdErr) {
		t.Fatalf("the real conflicting merge error %v is not a *CommandError", realMergeErr)
	}

	synthetic := &CommandError{
		Binary:   realCmdErr.Binary,
		Dir:      realCmdErr.Dir,
		Args:     realCmdErr.Args,
		ExitCode: -1,
		Stderr:   realCmdErr.Stderr,
		TimedOut: true,
		Timeout:  mergeTimeout,
		Err:      context.DeadlineExceeded,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := worktree.recoverFromFailedMerge(ctx, base, before, synthetic)

	if err == nil {
		t.Fatal("recoverFromFailedMerge succeeded against an already-cancelled ctx, want the abort to fail")
	}
	if errors.Is(err, ErrMergeTimedOut) {
		t.Errorf("error = %v, wrongly reports ErrMergeTimedOut for a recovery whose own abort could not run", err)
	}
	if result != (MergeResult{}) {
		t.Errorf("result = %+v, want the zero value on failure", result)
	}

	// The original merge error must still be discoverable: an abort failure
	// is additional information, not a replacement for what actually failed.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want it to still wrap the original merge error (context.DeadlineExceeded)", err)
	}

	// Cleanup: this ambient ctx is cancelled and unusable for further git
	// calls in this test, so the leftover MERGE_HEAD is cleared out with a
	// fresh context rather than left for the temp-dir removal to trip over.
	if _, abortErr := worktree.runner.run(t.Context(), mergeTimeout, "merge", "--abort"); abortErr != nil {
		t.Logf("cleanup abort after cancelled-ctx test: %v", abortErr)
	}
}
