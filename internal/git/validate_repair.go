package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxRepairDiffRead bounds the diff read from git. A larger change is described
// by its statistics only.
const maxRepairDiffRead = 2 << 20

// DiffSince returns the textual change of the worktree against the exact commit
// base: committed, staged, unstaged and untracked files together. Untracked
// files are listed by path because git prints no body for them. A change too
// large to read is described by its statistics only. It changes nothing.
func (w *Worktree) DiffSince(ctx context.Context, base string) (string, error) {
	if err := w.check(); err != nil {
		return "", err
	}
	if !fullBugObject(base) {
		return "", errors.New("diff baseline must be a full commit object")
	}
	actual, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return "", err
	}
	if actual != base {
		return "", errors.New("diff baseline is not the exact commit")
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--unified=2", base, "--"}
	text, err := w.runner.runRawLimit(ctx, readTimeout, maxRepairDiffRead, args...)
	if err != nil {
		if ctx.Err() != nil {
			return "", err
		}
		stat, statErr := w.runner.runRaw(ctx, readTimeout, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--stat", base, "--")
		if statErr != nil {
			return "", errors.Join(err, statErr)
		}
		text = "diff too large to keep; statistics only:\n" + stat
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(text)
	for _, change := range changes {
		if change.Kind == ChangeUntracked {
			fmt.Fprintf(&b, "\nnew untracked file: %s\n", change.Path)
		}
	}
	return b.String(), nil
}

// ResetHard puts the worktree back on the exact commit and removes every
// untracked file outside the keep prefixes, so no half-made edit survives. The
// branch moves to the commit. keep names paths, relative to the worktree root,
// that hold the factory's own files (such as the session directory) and stay.
func (w *Worktree) ResetHard(ctx context.Context, commit string, keep ...string) error {
	if err := w.check(); err != nil {
		return err
	}
	if !fullBugObject(commit) {
		return errors.New("reset target must be a full commit object")
	}
	actual, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		return err
	}
	if actual != commit {
		return errors.New("reset target is not the exact commit")
	}
	if _, err := w.runner.run(ctx, commitTimeout, "reset", "--hard", "--quiet", commit); err != nil {
		return fmt.Errorf("reset to %s: %w", commit, err)
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return fmt.Errorf("read status after reset: %w", err)
	}
	for _, change := range changes {
		if change.Kind != ChangeUntracked || keptPath(change.Path, keep) || !fs.ValidPath(change.Path) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(w.path, filepath.FromSlash(change.Path))); err != nil {
			return fmt.Errorf("remove untracked %s: %w", change.Path, err)
		}
	}
	return nil
}

func keptPath(name string, keep []string) bool {
	for _, k := range keep {
		k = strings.TrimSuffix(k, "/")
		if k != "" && (name == k || strings.HasPrefix(name, k+"/")) {
			return true
		}
	}
	return false
}
