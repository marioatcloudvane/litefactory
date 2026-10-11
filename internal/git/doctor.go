package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// DoctorObservation contains bounded local worktree facts with no file contents.
// BranchHead is the last local origin task head when the local branch has no
// commits absent from it, and the local Head otherwise.
type DoctorObservation struct{ Head, BranchHead, Status, Unpushed string }

// ObserveDoctor never fetches or writes the index. It verifies recorded branch
// identity and disables optional locks, fsmonitor and untracked-cache writes.
func (b *Binary) ObserveDoctor(ctx context.Context, path, branch string) (DoctorObservation, error) {
	var out DoctorObservation
	if b.Path() == "" {
		return out, ErrUninitialized
	}
	if err := checkName("branch", branch); err != nil {
		return out, err
	}
	resolved, err := resolveTarget(path)
	if err != nil {
		return out, err
	}
	if resolved != path || !filepath.IsAbs(path) {
		return out, errors.New("doctor worktree path is not canonical")
	}
	r := runner{binary: b.path, dir: path}
	run := func(args ...string) (string, error) {
		base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false"}
		return r.runRawLimit(ctx, readTimeout, 65536, append(base, args...)...)
	}
	top, err := run("rev-parse", "--show-toplevel")
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(top) != path {
		return out, errors.New("doctor worktree root mismatch")
	}
	actual, err := run("symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(actual) != "refs/heads/"+branch {
		return out, errors.New("doctor worktree branch mismatch")
	}
	head, err := run("rev-parse", "--verify", "HEAD")
	if err != nil {
		return out, err
	}
	out.Head = strings.TrimSpace(head)
	out.BranchHead = out.Head
	out.Status, err = run("status", "--porcelain", "-z", "--untracked-files=all", "--ignore-submodules=all")
	if err != nil {
		return out, err
	}
	remote, err := run("rev-parse", "--verify", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil {
		out.Unpushed = "Unknown: no usable local origin task reference; no fetch performed"
		return out, nil //nolint:nilerr // An absent optional local remote reference is an explicit coverage gap.
	}
	count, err := run("rev-list", "--count", strings.TrimSpace(remote)+".."+out.Head)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(count) == "0" {
		out.BranchHead = strings.TrimSpace(remote)
	}
	out.Unpushed = strings.TrimSpace(count) + " local commits absent from last local origin task reference; no fetch performed"
	return out, nil
}
