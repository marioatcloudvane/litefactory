package git

import (
	"context"
	"fmt"
)

// Push publishes the worktree's branch to remote.
//
// # Credentials
//
// The push authenticates with the ambient SSH credential and nothing else. The
// bot token is never handed to git: it does not appear in the child environment
// (see childEnv), in the argument list, or in a credential helper. A repository
// whose origin is an HTTPS URL therefore cannot be pushed to by the daemon, and
// that is the intended behaviour rather than a gap — pushing as the bot would
// produce commits attributed to a machine account, which is precisely what
// [Author] exists to avoid.
//
// # Deadline
//
// The command carries the 120 second push deadline. Without it a
// passphrase-protected key or a black-holed host produces a hung tick instead
// of an error, and a hung daemon looks alive — a far worse failure than a loud
// one. ssh runs in BatchMode, so it refuses to prompt rather than waiting for
// an answer no one can give.
//
// # On failure
//
// The commit is already made and is not lost. The returned [*PushError] names
// the worktree holding it so the report can tell the user where the work is
// (AC-3.24).
func (w *Worktree) Push(ctx context.Context, remote string) error {
	if err := w.check(); err != nil {
		return err
	}
	if err := checkName("remote", remote); err != nil {
		return err
	}

	// The fully qualified refspec leaves git nothing to guess: it cannot be
	// read as an option, it cannot be resolved to a tag of the same name, and
	// it does not depend on push.default. --set-upstream is idempotent and is
	// what makes a subsequent "git status" in the worktree say how far ahead
	// the branch is.
	ref := "refs/heads/" + w.branch
	if _, err := w.runner.run(ctx, pushTimeout, "push", "--set-upstream", "--", remote, ref+":"+ref); err != nil {
		return &PushError{Remote: remote, Branch: w.branch, Worktree: w.path, Err: err}
	}
	return nil
}

// PushError reports a push that did not reach the remote.
//
// It exists to carry Worktree. A failed push leaves a perfectly good commit on
// a local branch, and the difference between a recoverable situation and a lost
// afternoon is whether the report says where that branch is — so the path is a
// field, not a sentence a caller has to reconstruct.
type PushError struct {
	// Remote is the remote that was pushed to.
	Remote string
	// Branch is the branch that was pushed.
	Branch string
	// Worktree is the local directory still holding the commit.
	Worktree string
	// Err is the underlying failure, usually a [*CommandError].
	Err error
}

func (e *PushError) Error() string {
	return fmt.Sprintf("push %s to %s failed, the commit is intact in the worktree at %s: %v",
		e.Branch, e.Remote, e.Worktree, e.Err)
}

func (e *PushError) Unwrap() error { return e.Err }
