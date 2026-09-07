package git

import (
	"context"
	"fmt"
)

// Worktree is a linked git worktree: the one place litefactory is allowed to
// change a repository.
//
// # Why this type exists
//
// Every field is unexported and there is no exported constructor, so no code
// outside this package can build one. A composite literal such as
// git.Worktree{} still compiles, but it is inert: the runner inside it is the
// zero runner, and every method fails with [ErrUninitialized] rather than
// running git in whatever directory the process happens to be sitting in.
//
// Inside the package there is exactly one constructor, [Clone.newWorktree], and
// it refuses any path overlapping the registered clone. Both exported ways to
// obtain a Worktree — [Clone.WorktreeAdd] and [EnsureWorktree] — go through it
// before they run a single git command, and an internal test refuses a Worktree
// composite literal written anywhere else in the package. The two halves
// together are the guarantee: mutating operations exist only on this type, and
// this type can never point at the repository the user registered.
//
// # What it can do
//
// The read-only half records what happened: [Worktree.Path], [Worktree.Branch],
// [Worktree.ClonePath], [Worktree.RevParse], [Worktree.Status], [Worktree.Divergence]
// and [Worktree.MergePreview]. The mutating half is [Worktree.Commit],
// [Worktree.Push] and [Worktree.MergeBase] — the only three operations in
// litefactory that write to a repository or to a remote.
type Worktree struct {
	runner runner
	path   string
	branch string
	clone  string
}

// newWorktree builds a Worktree. It is the only place in this package that
// writes the composite literal, which is what makes the overlap check below
// impossible to route around: a future operation that needs a handle has to
// come through here and is refused the same way [Clone.WorktreeAdd] is.
//
// It runs no git command, so callers can build the handle first and let the
// refusal happen before anything touches the disk.
func (c *Clone) newWorktree(target, branch string) (*Worktree, error) {
	if c == nil || c.runner.binary == "" || c.path == "" {
		return nil, ErrUninitialized
	}
	if target == "" {
		return nil, fmt.Errorf("worktree path: %w: must not be empty", ErrInvalidArgument)
	}
	if err := checkName("branch", branch); err != nil {
		return nil, err
	}
	if overlaps(c.path, target) {
		return nil, fmt.Errorf("worktree path %s %w %s", target, ErrWorktreeOverlapsClone, c.path)
	}
	return &Worktree{
		runner: runner{binary: c.runner.binary, dir: target},
		path:   target,
		branch: branch,
		clone:  c.path,
	}, nil
}

// check refuses a Worktree this package did not build. Every method that runs
// git goes through it, so a git.Worktree{} literal is inert in the same way
// whether the method reads or writes.
func (w *Worktree) check() error {
	if w == nil || w.runner.binary == "" || w.runner.dir == "" || w.branch == "" || w.clone == "" {
		return ErrUninitialized
	}
	return nil
}

// Path returns the worktree's root directory: absolute, with the symlinks in
// its existing ancestors already resolved.
func (w *Worktree) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// Branch returns the branch checked out in this worktree.
func (w *Worktree) Branch() string {
	if w == nil {
		return ""
	}
	return w.branch
}

// ClonePath returns the registered clone this worktree was added to. Reports
// and cleanup need to name both paths, and keeping the link on the value means
// a caller never has to reconstruct it.
func (w *Worktree) ClonePath() string {
	if w == nil {
		return ""
	}
	return w.clone
}

// RevParse resolves rev inside the worktree, returning its object id or — with
// AbbrevRef — its symbolic name. This is how the before and after commit of a
// run are recorded.
func (w *Worktree) RevParse(ctx context.Context, rev string, opts RevParseOptions) (string, error) {
	if err := w.check(); err != nil {
		return "", err
	}
	return revParse(ctx, w.runner, rev, opts)
}
