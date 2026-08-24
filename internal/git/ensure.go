package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultRemote is the remote litefactory works against. S-6 names origin
// specifically, and there is no configuration key for it: a repository whose
// upstream is called something else is out of scope rather than silently
// guessed at.
const DefaultRemote = "origin"

// worktreeDirMode is the mode for parent directories created on the way to a
// worktree. The daemon runs under umask 0o077 already; spelling the mode out
// here means a worktree root created by the CLI in an ordinary shell is not
// group-readable either.
const worktreeDirMode fs.FileMode = 0o700

// EnsureWorktreeOptions configures [EnsureWorktree].
type EnsureWorktreeOptions struct {
	// Dir is the worktree directory, as produced by naming.WorktreePath:
	// <worktree_root>/task-<issue>. Missing parents are created.
	Dir string
	// Branch is the task branch, as produced by naming.Branch:
	// <feature_prefix>/<issue>-<slug>. Required.
	Branch string
	// BaseBranch is the repository's base branch, for example "main". A branch
	// that does not exist yet is created from <Remote>/<BaseBranch>; it is
	// unused when the branch already exists. Required.
	BaseBranch string
	// Remote is the remote to resolve the base and any existing task branch
	// against. Empty means [DefaultRemote].
	Remote string
}

// EnsureResult reports what [EnsureWorktree] found and what it had to make.
//
// The two Created flags are what lets a report say "resumed run 3 on the
// existing branch" rather than implying every run starts from scratch.
type EnsureResult struct {
	// Worktree is the handle to work in. It is never nil on success.
	Worktree *Worktree
	// BranchCreated reports that the local branch did not exist and was made.
	// False means it was reused, which is the AC-3.8 requirement: a second run
	// on the same task continues the first run's branch.
	BranchCreated bool
	// BranchStart is the start point the branch was created from — either
	// "<remote>/<base>" for a new task or "<remote>/<branch>" when the branch
	// existed only on the remote. Empty when the branch was reused locally.
	BranchStart string
	// WorktreeCreated reports that the directory was not already a registered
	// worktree and was added. False means it was reused as it stood.
	WorktreeCreated bool
}

// EnsureWorktree returns a worktree for a task, creating as little as it can.
//
// It is the composite operation a turn actually needs, built entirely out of
// the S-6 allowlist — show-ref, worktree list, worktree add — which is why it is
// a function taking a [Clone] rather than a method on one. The registered
// clone's method set stays exactly the allowlist, and nothing here writes to
// the clone's working tree, index or HEAD.
//
// The sequence is reuse-first at both levels:
//
//   - If Dir is already this clone's worktree on Branch, it is reused
//     untouched. Registered on a different branch, or registered with its
//     directory gone, is a [*WorktreeConflictError].
//   - If Dir holds anything else — a file, or a non-empty directory that is not
//     a registered worktree — it is a [*WorktreeConflictError] and nothing is
//     deleted, moved or overwritten. Removing whatever a user left in the way
//     is not a decision this package gets to make. An existing empty directory
//     is not "anything else": it holds no work to lose, so git is allowed to
//     check out into it.
//   - The branch is reused if it exists locally. If it exists only on the
//     remote it is created from the remote-tracking ref, so a branch pushed by
//     an earlier run on another machine is continued rather than reset. Only a
//     branch that exists nowhere is created from <remote>/<base> (AC-3.8).
//
// It runs no network command. The caller fetches first — AC-3.5 puts "git fetch
// --prune" against the registered clone at the top of the turn — so the
// remote-tracking refs read here are as fresh as that fetch made them.
func EnsureWorktree(ctx context.Context, clone *Clone, opts EnsureWorktreeOptions) (EnsureResult, error) {
	remote := opts.Remote
	if remote == "" {
		remote = DefaultRemote
	}
	if err := checkName("remote", remote); err != nil {
		return EnsureResult{}, err
	}
	if err := checkName("base branch", opts.BaseBranch); err != nil {
		return EnsureResult{}, err
	}

	target, err := resolveTarget(opts.Dir)
	if err != nil {
		return EnsureResult{}, err
	}
	// The handle is built before anything else runs, so a path overlapping the
	// registered clone is refused without a single git command being issued —
	// and without this function ever holding a Worktree it should not have.
	worktree, err := clone.newWorktree(target, opts.Branch)
	if err != nil {
		return EnsureResult{}, err
	}

	registered, err := clone.WorktreeList(ctx)
	if err != nil {
		return EnsureResult{}, fmt.Errorf("list the worktrees of %s: %w", clone.Path(), err)
	}
	if entry, ok := findWorktree(registered, target); ok {
		if err := checkReusable(entry, target, opts.Branch); err != nil {
			return EnsureResult{}, err
		}
		return EnsureResult{Worktree: worktree}, nil
	}

	if err := checkVacant(target, opts.Branch); err != nil {
		return EnsureResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target), worktreeDirMode); err != nil {
		return EnsureResult{}, fmt.Errorf("create the parent directory of %s: %w", target, err)
	}

	start, created, err := resolveBranchStart(ctx, clone, remote, opts.Branch, opts.BaseBranch)
	if err != nil {
		return EnsureResult{}, err
	}
	if _, err := clone.WorktreeAdd(ctx, target, WorktreeAddOptions{Branch: opts.Branch, CreateFrom: start}); err != nil {
		return EnsureResult{}, err
	}
	return EnsureResult{
		Worktree:        worktree,
		BranchCreated:   created,
		BranchStart:     start,
		WorktreeCreated: true,
	}, nil
}

// resolveBranchStart decides what the worktree should be checked out from.
//
// An empty start point means "the branch already exists locally, just check it
// out". The remote-tracking branch is preferred over the base branch because a
// task branch that exists on the remote is a previous run's work: starting it
// again from the base would silently orphan those commits.
func resolveBranchStart(ctx context.Context, clone *Clone, remote, branch, base string) (start string, created bool, err error) {
	switch _, err := clone.ShowRef(ctx, "refs/heads/"+branch); {
	case err == nil:
		return "", false, nil
	case !errors.Is(err, ErrRefNotFound):
		return "", false, fmt.Errorf("look up local branch %s: %w", branch, err)
	}

	remoteBranch := remote + "/" + branch
	switch _, err := clone.ShowRef(ctx, "refs/remotes/"+remoteBranch); {
	case err == nil:
		return remoteBranch, true, nil
	case !errors.Is(err, ErrRefNotFound):
		return "", false, fmt.Errorf("look up remote branch %s: %w", remoteBranch, err)
	}

	// Neither: this is the task's first run, so the branch starts at the base.
	// The base is verified here rather than left to "git worktree add" so the
	// failure names the branch that is missing instead of relaying git's prose
	// about an invalid reference.
	remoteBase := remote + "/" + base
	if _, err := clone.ShowRef(ctx, "refs/remotes/"+remoteBase); err != nil {
		return "", false, fmt.Errorf("base branch %s: %w", remoteBase, err)
	}
	return remoteBase, true, nil
}

// findWorktree locates the entry for target in a "git worktree list" result.
//
// Paths are compared after resolution because the recorded path came from
// whatever spelling was used at add time, while target has been through
// resolveTarget — on macOS that is the difference between /var and /private/var
// for every worktree under a temporary directory.
func findWorktree(entries []WorktreeInfo, target string) (WorktreeInfo, bool) {
	for _, entry := range entries {
		if entry.Path == target || filepath.Clean(entry.Path) == target {
			return entry, true
		}
		if resolved, err := resolveTarget(entry.Path); err == nil && resolved == target {
			return entry, true
		}
	}
	return WorktreeInfo{}, false
}

// checkReusable decides whether an already-registered worktree is the one this
// task wants. Everything it refuses, it refuses without touching the directory.
func checkReusable(entry WorktreeInfo, target, branch string) error {
	conflict := func(found string) error {
		return &WorktreeConflictError{Path: target, WantBranch: branch, Found: found}
	}
	switch {
	case entry.Prunable:
		// git has the worktree on its books but the directory is gone.
		// Re-adding would fail on the stale administrative entry, and pruning
		// it is not in the S-6 allowlist — "task clean" is the way out, and
		// saying so is more use than a fatal from git.
		return conflict("a registered worktree whose directory is missing; run \"litefactory task clean\" first")
	case entry.Detached:
		return conflict("a registered worktree with a detached HEAD")
	case entry.Branch != "refs/heads/"+branch:
		return conflict(fmt.Sprintf("a registered worktree on %s", entry.Branch))
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("inspect the registered worktree at %s: %w", target, err)
	}
	if !info.IsDir() {
		return conflict("not a directory")
	}
	return nil
}

// checkVacant refuses a path that holds something git would have to displace.
//
// git refuses a non-empty directory too, but only with "fatal: '<path>' already
// exists". Naming the condition here is what lets the CLI tell the user that
// their directory is intact and in the way, rather than relaying a message that
// reads like a bug.
func checkVacant(target, branch string) error {
	info, err := os.Stat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %s: %w", target, err)
	}

	conflict := func(found string) error {
		return &WorktreeConflictError{Path: target, WantBranch: branch, Found: found}
	}
	if !info.IsDir() {
		return conflict("a file")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return fmt.Errorf("read %s: %w", target, err)
	}
	if len(entries) > 0 {
		return conflict(fmt.Sprintf("a non-empty directory (%d entries) that is not a registered worktree", len(entries)))
	}
	return nil
}

// WorktreeConflictError reports a worktree path that already holds something
// else. Nothing was deleted, moved or overwritten.
//
// It exists as a named type because this is the one failure in the worktree
// lifecycle where the safe action and the convenient action differ. Deleting a
// user's directory because it was in the way is not recoverable; failing the
// turn is. The fields carry what a person needs to decide what to do — where,
// what litefactory wanted, and what is actually there.
type WorktreeConflictError struct {
	// Path is the worktree directory that was left exactly as it was.
	Path string
	// WantBranch is the branch litefactory needed checked out there.
	WantBranch string
	// Found describes what occupies the path instead.
	Found string
}

func (e *WorktreeConflictError) Error() string {
	return fmt.Sprintf("worktree path %s holds %s, not a worktree on %s; nothing was removed",
		e.Path, e.Found, e.WantBranch)
}

func (e *WorktreeConflictError) Unwrap() error { return ErrWorktreeOccupied }
