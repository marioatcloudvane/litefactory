package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
)

// mergeBugTarget merges the target commit into the task worktree with a
// factory-authored merge commit unless it is already an ancestor of HEAD. A
// conflicting merge is never started: the read-only preview names the
// conflicting paths, which are returned as the blocking reason with the
// worktree untouched. MergeBase itself refuses a dirty worktree and aborts
// any merge it cannot complete.
func mergeBugTarget(ctx context.Context, w *git.Worktree, target string, author git.Author) (string, error) {
	d, err := w.Divergence(ctx, target)
	if err != nil {
		return "", err
	}
	if d.Behind == 0 {
		return "", nil
	}
	preview, err := w.MergePreview(ctx, target)
	if err != nil {
		return "", err
	}
	if !preview.Clean {
		return fmt.Sprintf("merging main %s into the repair branch conflicts in %s; resolve the conflict on the branch before another repair attempt", target, strings.Join(preview.Conflicts, ", ")), nil
	}
	_, err = w.MergeBase(ctx, target, git.MergeOptions{Author: author})
	if errors.Is(err, git.ErrMergeConflict) {
		return fmt.Sprintf("merging main %s into the repair branch conflicts; the merge was aborted and the branch is unchanged", target), nil
	}
	return "", err
}

// syncBugTarget brings current main into the task worktree at the start of a
// repair attempt, before its source commit is read, so the fixer works on
// current code and a retry picks up fixes that landed on main meanwhile. It
// leaves a dirty worktree or one other work owns exactly as it is.
func (r *BugRuntime) syncBugTarget(ctx context.Context, session int64, tree *git.Worktree) (string, error) {
	idle, e := r.opts.DB.BugWorkIdle(ctx, session)
	if e != nil || !idle {
		return "", e
	}
	changes, e := tree.Status(ctx)
	if e != nil || len(changes) > 0 {
		return "", e
	}
	if e = r.fetchTarget(ctx, session); e != nil {
		return "", e
	}
	s, e := r.opts.DB.GetSession(ctx, session)
	if e != nil {
		return "", e
	}
	repo, e := r.opts.DB.GetRepo(ctx, s.RepoID)
	if e != nil {
		return "", e
	}
	release, e := r.opts.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, repo.Path)
	if e != nil {
		return "", e
	}
	target, e := clone.ShowRef(ctx, "refs/remotes/"+r.opts.remote()+"/main")
	if e != nil {
		return "", e
	}
	author, e := resolveAuthor(ctx, r.opts, clone)
	if e != nil {
		return "", e
	}
	return mergeBugTarget(ctx, tree, target, author)
}

// bugBaseWorktree checks the target commit out in a temporary detached
// worktree of the task's clone for check rerun attribution. The returned
// cleanup removes the worktree and its directory.
func (r *BugRuntime) bugBaseWorktree(session int64) func(context.Context, string) (*git.Worktree, func(context.Context) error, error) {
	return func(ctx context.Context, commit string) (*git.Worktree, func(context.Context) error, error) {
		s, e := r.opts.DB.GetSession(ctx, session)
		if e != nil {
			return nil, nil, e
		}
		repo, e := r.opts.DB.GetRepo(ctx, s.RepoID)
		if e != nil {
			return nil, nil, e
		}
		dir, e := os.MkdirTemp("", "lf-bug-target-")
		if e != nil {
			return nil, nil, e
		}
		tree := filepath.Join(dir, "tree")
		w, e := r.addBugTarget(ctx, repo.Path, tree, commit)
		if e != nil {
			return nil, nil, errors.Join(e, os.RemoveAll(dir))
		}
		return w, func(ctx context.Context) error {
			return errors.Join(r.removeBugTarget(ctx, repo.Path, tree), os.RemoveAll(dir))
		}, nil
	}
}

// addBugTarget adds the detached target worktree under the clone gate.
func (r *BugRuntime) addBugTarget(ctx context.Context, clonePath, tree, commit string) (*git.Worktree, error) {
	release, e := r.opts.CloneGate.Acquire(ctx, clonePath)
	if e != nil {
		return nil, e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, clonePath)
	if e != nil {
		return nil, e
	}
	return clone.WorktreeAdd(ctx, tree, git.WorktreeAddOptions{Branch: commit, Detach: true})
}

// removeBugTarget removes the target worktree under the clone gate, forced
// because the rerun may leave build or test files behind.
func (r *BugRuntime) removeBugTarget(ctx context.Context, clonePath, tree string) error {
	release, e := r.opts.CloneGate.Acquire(ctx, clonePath)
	if e != nil {
		return e
	}
	defer release()
	clone, e := r.opts.Git.Open(ctx, clonePath)
	if e != nil {
		return e
	}
	return clone.WorktreeRemove(ctx, tree, git.WorktreeRemoveOptions{Force: true})
}
