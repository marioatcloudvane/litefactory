package git

import "context"

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
// That leaves exactly one source of a usable Worktree — [Clone.WorktreeAdd] —
// and that constructor refuses any path overlapping the registered clone. The
// two halves together are the guarantee: mutating operations exist only on this
// type, and this type can never point at the repository the user registered.
//
// The mutating half of the API (stage, commit with trailers, push) lands with
// the turn implementation; what is here now is the type, its construction path
// and the read-only accessors a caller needs to record what it did.
type Worktree struct {
	runner runner
	path   string
	branch string
	clone  string
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
	if w == nil {
		return "", ErrUninitialized
	}
	return revParse(ctx, w.runner, rev, opts)
}
