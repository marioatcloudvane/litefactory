package git

import (
	"context"
	"errors"
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
// # The upstream is verified, never inferred from the exit code
//
// git exiting 0 is not evidence that --set-upstream wrote anything. Measured at
// parallelism 4 on git 2.50.1, 240 concurrent pushes from one clone produced
// **0 non-zero exits** and every one of the 240 branches reached the remote —
// and yet 89 of those runs printed
//
//	error: could not lock config file .git/config: File exists
//
// and 60 of the 240 branches ended with no branch.<name>.remote entry at all.
// git loses the race for the config lock, prints the error, skips the write and
// still exits 0. Checking only the exit status would have litefactory report a
// clean push for a branch that has no upstream, and what this daemon signs its
// name to has to be true — so the recorded upstream is read back afterwards,
// whatever git exited with, and a missing or wrong entry is a failure
// (AC-PAR-3.4). See confirmUpstream for what "the recorded upstream" means and
// why it is two config keys rather than one.
//
// # On failure
//
// The commit is already made and is not lost. The returned [*PushError] names
// the worktree holding it so the report can tell the user where the work is
// (AC-3.24). When only the upstream is at fault the error also says the branch
// did reach the remote, because measurement says it did.
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
	if err := w.confirmUpstream(ctx, remote); err != nil {
		return &PushError{
			Remote:    remote,
			Branch:    w.branch,
			Worktree:  w.path,
			Published: true,
			Err:       err,
		}
	}
	return nil
}

// confirmUpstream reads back the upstream that --set-upstream was supposed to
// write, and is the whole of the exit-code-independent check described above.
//
// Both halves of the upstream are checked, not only branch.<name>.remote, and
// the second one is what gives the check teeth. Measured on git 2.50.1, a
// "worktree add -b <branch> origin/<base>" already leaves
//
//	branch.<name>.remote = origin
//	branch.<name>.merge  = refs/heads/<base>
//
// behind it, because branch.autoSetupMerge is on by default. The push's job is
// to move merge to refs/heads/<name>; remote is usually already correct before
// the push runs. So a push whose config write was lost passes a remote-only
// check while leaving the branch tracking the *base* — "git pull" in that
// worktree would pull main into the task branch — and litefactory would report
// the push as clean. Checking merge as well is what makes the branch's recorded
// upstream, rather than one field of it, the thing that has to be true.
//
// These are separate git commands with their own deadlines rather than a flag
// on the push, because that is how every command in this package is bounded:
// the push's 120 seconds belong to the push, and a verification sharing them
// would have no budget left on exactly the slow push where it matters most
// (AC-PAR-3.6). Both are local config reads.
//
// The values are read the way git itself resolves them — no --local — because
// the question being answered is what the next "git push" or "git status" in
// this worktree will do, not which file the answer came from.
func (w *Worktree) confirmUpstream(ctx context.Context, remote string) error {
	for _, want := range []struct{ key, value string }{
		{key: "branch." + w.branch + ".remote", value: remote},
		{key: "branch." + w.branch + ".merge", value: "refs/heads/" + w.branch},
	} {
		got, err := w.runner.run(ctx, readTimeout, "config", "--get", "--", want.key)
		if err != nil {
			if classified := classify(err, ErrConfigKeyNotFound, 1); errors.Is(classified, ErrConfigKeyNotFound) {
				return fmt.Errorf("%w: %s is not set", ErrUpstreamNotRecorded, want.key)
			}
			// git could not answer at all. That is not the measured failure,
			// but it is still not a confirmed upstream, and reporting it as one
			// would be the same lie in a rarer costume.
			return fmt.Errorf("read back %s: %w", want.key, err)
		}
		if got != want.value {
			return fmt.Errorf("%w: %s is %q, want %q", ErrUpstreamNotRecorded, want.key, got, want.value)
		}
	}
	return nil
}

// PushError reports a push that did not fully succeed.
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
	// Published reports that git's own push succeeded, so the objects did
	// reach the remote and only the upstream bookkeeping is in question. It
	// keeps the message honest: the failure mode this flag exists for was
	// measured to deliver every single branch (see [Worktree.Push]), and a
	// report claiming the work never left the machine would send the user
	// looking for something that is already there.
	Published bool
	// Err is the underlying failure: a [*CommandError] from the push itself,
	// or [ErrUpstreamNotRecorded] when the push landed and the upstream did
	// not.
	Err error
}

func (e *PushError) Error() string {
	if e.Published {
		return fmt.Sprintf("push %s to %s reported success but its upstream could not be confirmed; "+
			"the branch did reach %s and the commit is intact in the worktree at %s: %v",
			e.Branch, e.Remote, e.Remote, e.Worktree, e.Err)
	}
	return fmt.Sprintf("push %s to %s failed, the commit is intact in the worktree at %s: %v",
		e.Branch, e.Remote, e.Worktree, e.Err)
}

func (e *PushError) Unwrap() error { return e.Err }
