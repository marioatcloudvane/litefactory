package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrRetirementChanged means the exact resource changed after eligibility was
// established. A later pass must obtain fresh evidence before trying again.
var ErrRetirementChanged = errors.New("retirement resource changed")

// GuardedRetirement is the narrow S-6 task maintenance capability. The daemon
// owns canonical ownership/closure/retention; this capability independently
// rechecks Git safety and uses expected-head deletion. No clone checkout, index
// or HEAD mutation is permitted. kind is worktree, local_branch or remote_branch.
func GuardedRetirement(ctx context.Context, c *Clone, previous RetirementObservation, base, kind string) error {
	current, err := ObserveRetirement(ctx, c, previous.Path, previous.Branch, base)
	if err != nil {
		return err
	}
	if current.Protected || !current.WorktreeSafe || current.Dirty || (current.LocalSHA != "" && !current.LocalPreserved) {
		return fmt.Errorf("retirement safety no longer holds: %w", ErrRetirementChanged)
	}
	ref := "refs/heads/" + previous.Branch
	switch kind {
	case "worktree":
		if current.WorktreeSHA != previous.WorktreeSHA || current.LocalSHA != previous.LocalSHA || current.WorktreeExists != previous.WorktreeExists {
			return ErrRetirementChanged
		}
		if !current.WorktreeRegistered {
			return nil
		}
		// Also reconciles an exact stale registered path without global prune.
		return c.WorktreeRemove(ctx, current.Path, WorktreeRemoveOptions{})
	case "local_branch":
		// Observed absence (a successful probe that found no ref) is the goal
		// state; the caller confirms it independently.
		if current.LocalSHA == "" {
			return nil
		}
		if current.LocalSHA != previous.LocalSHA || current.LocalSHA == "" || !current.LocalMerged || current.BranchInUse {
			return ErrRetirementChanged
		}
		// update-ref compares the old object under the ref lock; unlike branch -d,
		// its preservation basis is the verified remote base, never clone HEAD.
		_, err = c.runner.run(ctx, readTimeout, "update-ref", "--no-deref", "-d", ref, previous.LocalSHA)
		return err
	case "remote_branch":
		if current.RemoteSHA == "" {
			return nil
		}
		if current.RemoteSHA != previous.RemoteSHA || current.RemoteSHA == "" || !current.RemoteMerged {
			return ErrRetirementChanged
		}
		// Explicit expected value is enforced by receive-pack on the remote server.
		_, err = c.runner.run(ctx, fetchTimeout, "push", "--force-with-lease="+ref+":"+previous.RemoteSHA, "--", "origin", ":"+ref)
		return err
	default:
		return fmt.Errorf("unknown retirement resource kind %q", kind)
	}
}

// RetirementAbsent confirms only the named resource. It never fetches or
// globally prunes other owners' registrations. Remote absence is read directly
// from the server rather than inferred from a tracking ref.
func RetirementAbsent(ctx context.Context, c *Clone, path, branch, kind string) (bool, error) {
	if c == nil {
		return false, ErrUninitialized
	}
	if err := checkName("branch", branch); err != nil {
		return false, err
	}
	switch kind {
	case "local_branch":
		sha, err := retirementRef(ctx, c, "refs/heads/"+branch)
		return sha == "", err
	case "remote_branch":
		out, err := c.runner.run(ctx, fetchTimeout, "ls-remote", "--refs", "--", "origin", "refs/heads/"+branch)
		return strings.TrimSpace(out) == "", err
	case "worktree":
		if path == "" {
			return false, fmt.Errorf("empty retirement path")
		}
		entries, err := c.WorktreeList(ctx)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.Path == path {
				return false, nil
			}
		}
		return retirementPathAbsent(path)
	default:
		return false, fmt.Errorf("unknown retirement resource kind %q", kind)
	}
}

func retirementPathAbsent(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}
