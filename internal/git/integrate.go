package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrIntegrationHeadMoved reports that the remote integration branch is no
// longer at the head a merge was built on. Nothing was published; the merge can
// be rebuilt on the new head.
var ErrIntegrationHeadMoved = errors.New("integration branch moved since the merge was built")

func remoteRefHead(ctx context.Context, r runner, remote, ref string) (string, error) {
	out, err := r.run(ctx, fetchTimeout, "ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return "", fmt.Errorf("observe remote %s: %w", ref, err)
	}
	if out == "" {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != ref || !fullBugObject(fields[0]) {
		return "", fmt.Errorf("unexpected remote observation for %s", ref)
	}
	return fields[0], nil
}

// PushHeadTo publishes the worktree's HEAD as the remote branch, without force,
// and only while the remote branch is still at expectedHead. HEAD must descend
// from expectedHead, so the push is always a fast-forward: a branch that moved
// meanwhile makes the server refuse it and nothing is overwritten. A moved head
// is reported as [ErrIntegrationHeadMoved], whether it is seen before the push
// or only after the server refused it.
func (w *Worktree) PushHeadTo(ctx context.Context, remote, branch, expectedHead string) error {
	if err := w.check(); err != nil {
		return err
	}
	if err := checkName("remote", remote); err != nil {
		return err
	}
	if err := checkName("branch", branch); err != nil {
		return err
	}
	if !fullBugObject(expectedHead) {
		return fmt.Errorf("expected head %q: %w: must be a full commit id", expectedHead, ErrInvalidArgument)
	}
	head, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return fmt.Errorf("resolve HEAD before publishing: %w", err)
	}
	if _, err := w.runner.run(ctx, readTimeout, "merge-base", "--is-ancestor", expectedHead, head); err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) && cmdErr.ExitCode == 1 && !cmdErr.TimedOut {
			return fmt.Errorf("publish to %s: %w: the merge does not build on the expected head", branch, ErrInvalidArgument)
		}
		return fmt.Errorf("check ancestry before publishing: %w", err)
	}
	ref := "refs/heads/" + branch
	current, err := remoteRefHead(ctx, w.runner, remote, ref)
	if err != nil {
		return err
	}
	if current != expectedHead {
		return fmt.Errorf("publish to %s: %w", branch, ErrIntegrationHeadMoved)
	}
	if _, pushErr := w.runner.run(ctx, pushTimeout, "push", "--", remote, head+":"+ref); pushErr != nil {
		if after, obsErr := remoteRefHead(ctx, w.runner, remote, ref); obsErr == nil && after != expectedHead {
			return fmt.Errorf("publish to %s: %w", branch, ErrIntegrationHeadMoved)
		}
		return &PushError{Remote: remote, Branch: branch, Worktree: w.path, Err: pushErr}
	}
	return nil
}
