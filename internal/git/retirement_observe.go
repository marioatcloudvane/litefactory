package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RetirementObservation contains only preservation facts, never file contents.
// It is advisory: callers must observe again under coordination before removal.
type RetirementObservation struct {
	Path                    string
	Branch                  string
	BaseSHA                 string
	LocalSHA                string
	RemoteSHA               string
	WorktreeSHA             string
	WorktreeExists          bool
	WorktreeRegistered      bool
	WorktreeSafe            bool
	WorktreeReason          string
	Dirty                   bool
	LocalPreserved          bool
	LocalMerged             bool
	RemoteMerged            bool
	BranchInUse             bool
	OtherWorktreeUsesBranch bool
	Protected               bool
}

// ObserveRetirement performs fresh fetch/prune and narrowly scoped read-only
// probes. It never changes the registered clone checkout, raw index or HEAD.
func ObserveRetirement(ctx context.Context, c *Clone, path, branch, base string) (RetirementObservation, error) {
	o := RetirementObservation{Path: path, Branch: branch}
	if c == nil {
		return o, ErrUninitialized
	}
	if err := checkName("branch", branch); err != nil {
		return o, err
	}
	if err := checkName("base", base); err != nil {
		return o, err
	}
	if err := c.Fetch(ctx, "origin"); err != nil {
		return o, err
	}
	var err error
	o.BaseSHA, err = c.RevParse(ctx, "refs/remotes/origin/"+base+"^{commit}", RevParseOptions{})
	if err != nil {
		return o, err
	}
	defaultOutput, err := c.runner.run(ctx, fetchTimeout, "ls-remote", "--symref", "--", "origin", "HEAD")
	if err != nil {
		return o, err
	}
	var defaultRef string
	for _, line := range strings.Split(defaultOutput, "\n") {
		if strings.HasPrefix(line, "ref: refs/heads/") && strings.HasSuffix(line, "\tHEAD") {
			defaultRef = strings.TrimSuffix(strings.TrimPrefix(line, "ref: refs/heads/"), "\tHEAD")
		}
	}
	// Unknown default identity is a conservative refusal, never permission.
	o.Protected = branch == base || defaultRef == branch || defaultRef == ""
	o.LocalSHA, err = retirementRef(ctx, c, "refs/heads/"+branch)
	if err != nil {
		return o, err
	}
	o.RemoteSHA, err = retirementRef(ctx, c, "refs/remotes/origin/"+branch)
	if err != nil {
		return o, err
	}
	if o.LocalSHA != "" {
		o.LocalMerged, err = retirementAncestor(ctx, c, o.LocalSHA, o.BaseSHA)
		if err != nil {
			return o, err
		}
		o.LocalPreserved = o.LocalMerged
		if !o.LocalPreserved && o.RemoteSHA != "" {
			o.LocalPreserved, err = retirementAncestor(ctx, c, o.LocalSHA, o.RemoteSHA)
			if err != nil {
				return o, err
			}
		}
	}
	if o.RemoteSHA != "" {
		o.RemoteMerged, err = retirementAncestor(ctx, c, o.RemoteSHA, o.BaseSHA)
		if err != nil {
			return o, err
		}
	}
	entries, err := c.WorktreeList(ctx)
	if err != nil {
		return o, err
	}
	o.WorktreeSafe = path == ""
	for _, e := range entries {
		if e.Branch == "refs/heads/"+branch {
			o.BranchInUse = true
			if e.Path != path {
				o.OtherWorktreeUsesBranch = true
			}
		}
		if path == "" || e.Path != path {
			continue
		}
		o.WorktreeRegistered = true
		o.WorktreeSHA = e.Head
		if e.Bare || e.Detached || e.Locked || e.Branch != "refs/heads/"+branch || overlaps(c.Path(), path) {
			o.WorktreeReason = "unsafe_worktree_identity"
			continue
		}
		target, resolveErr := resolveTarget(path)
		if resolveErr != nil {
			return o, resolveErr
		}
		if target != path || !filepath.IsAbs(path) {
			o.WorktreeReason = "noncanonical_worktree_path"
			continue
		}
		_, statErr := os.Lstat(path)
		if errors.Is(statErr, os.ErrNotExist) {
			o.WorktreeSafe = true
			o.WorktreeReason = "already_absent"
			continue
		}
		if statErr != nil {
			return o, statErr
		}
		o.WorktreeExists = true
		r := runner{binary: c.runner.binary, dir: path}
		top, probeErr := r.run(ctx, readTimeout, "rev-parse", "--show-toplevel")
		if probeErr != nil {
			return o, probeErr
		}
		if top != path {
			o.WorktreeReason = "wrong_worktree_root"
			continue
		}
		out, probeErr := r.run(ctx, readTimeout, "--no-optional-locks", "status", "--porcelain", "-z", "--untracked-files=all", "--ignored=matching", "--ignore-submodules=none")
		if probeErr != nil {
			return o, probeErr
		}
		// Status cannot prove absence of ignored files inside a submodule.
		// Refuse that uncertain checkout rather than discard nested local data.
		index, probeErr := r.run(ctx, readTimeout, "ls-files", "--stage", "-z")
		if probeErr != nil {
			return o, probeErr
		}
		hasSubmodule := false
		for _, entry := range strings.Split(index, "\x00") {
			if strings.HasPrefix(entry, "160000 ") {
				hasSubmodule = true
				break
			}
		}
		if hasSubmodule {
			o.WorktreeReason = "submodule_content_unverified"
			continue
		}
		o.Dirty = out != ""
		o.WorktreeSafe = true
		if o.Dirty {
			o.WorktreeReason = "local_files"
		}
		if e.Head != o.LocalSHA {
			o.WorktreeSafe = false
			o.WorktreeReason = "worktree_head_changed"
		}
	}
	if path != "" && !o.WorktreeRegistered {
		_, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			o.WorktreeSafe = true
			o.WorktreeReason = "already_absent"
		case err != nil:
			return o, err
		default:
			o.WorktreeExists = true
			o.WorktreeReason = "unregistered_path"
		}
	}
	return o, nil
}

func retirementRef(ctx context.Context, c *Clone, ref string) (string, error) {
	sha, err := c.ShowRef(ctx, ref)
	if errors.Is(err, ErrRefNotFound) {
		return "", nil
	}
	return sha, err
}
func retirementAncestor(ctx context.Context, c *Clone, tip, base string) (bool, error) {
	if strings.TrimSpace(tip) == "" || strings.TrimSpace(base) == "" {
		return false, fmt.Errorf("missing ancestry tip")
	}
	_, err := c.runner.run(ctx, readTimeout, "merge-base", "--is-ancestor", tip, base)
	if err == nil {
		return true, nil
	}
	var command *CommandError
	if errors.As(err, &command) && command.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
