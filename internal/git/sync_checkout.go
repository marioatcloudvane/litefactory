package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckSyncCheckout checks retained work without changing HEAD, files or index.
// An empty reason permits a fast-forward to commit. The caller must hold its
// writer exclusion across this check and the subsequent publication or refresh.
func (w *Worktree) CheckSyncCheckout(ctx context.Context, working, commit string) (string, error) {
	if err := w.check(); err != nil {
		return "", err
	}
	if err := checkName("working branch", working); err != nil {
		return "", err
	}
	if err := checkName("commit", commit); err != nil {
		return "", err
	}
	branch, err := w.runner.run(ctx, readTimeout, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		var command *CommandError
		if errors.As(err, &command) && command.ExitCode == 1 {
			return "retained checkout has detached HEAD", nil
		}
		return "", fmt.Errorf("inspect retained checkout branch: %w", err)
	}
	if branch != "refs/heads/"+working || w.branch != working {
		return "retained checkout branch does not match working branch", nil
	}
	// These index flags suppress ordinary status checks. Refuse them rather
	// than claiming that hidden tracked bytes are safe to replace.
	index, err := w.runner.run(ctx, readTimeout, "ls-files", "-v", "-z")
	if err != nil {
		return "", fmt.Errorf("inspect retained index flags: %w", err)
	}
	for _, entry := range strings.Split(index, "\x00") {
		if entry == "" {
			continue
		}
		if len(entry) < 3 || entry[1] != ' ' {
			return "", fmt.Errorf("unexpected retained index flag entry: %w", ErrInvalidArgument)
		}
		if entry[0] == 'S' || entry[0] >= 'a' && entry[0] <= 'z' {
			return "retained checkout has unsupported index flags (assume-unchanged or skip-worktree)", nil
		}
	}
	paths := []string{".litefactory-plan-proposal.json", ".litefactory-plan-outcome.json", ".litefactory-story-events"}
	args := append(append([]string{}, statusArgs...), "--untracked-files=no", "--")
	reserved, err := w.runner.run(ctx, readTimeout, append(args, paths...)...)
	if err != nil {
		return "", fmt.Errorf("inspect retained reserved transport: %w", err)
	}
	if reserved != "" {
		return "retained reserved transport has staged or tracked changes", nil
	}
	// Verify the target exists before interpreting merge-base exit status 1.
	target, err := revParse(ctx, w.runner, commit+"^{commit}", RevParseOptions{})
	if err != nil {
		return "", fmt.Errorf("resolve sync commit: %w", err)
	}
	args = []string{"diff", "--name-only", "-z", "HEAD", target, "--"}
	reserved, err = w.runner.run(ctx, readTimeout, append(args, paths...)...)
	if err != nil {
		return "", fmt.Errorf("inspect incoming reserved transport: %w", err)
	}
	if reserved != "" {
		return "reserved factory transport differs from synchronization commit", nil
	}
	merge, err := w.runner.run(ctx, readTimeout, "rev-parse", "--quiet", "--verify", "MERGE_HEAD")
	if err != nil {
		var command *CommandError
		if !errors.As(err, &command) || command.ExitCode != 1 {
			return "", fmt.Errorf("inspect retained merge state: %w", err)
		}
	}
	if merge != "" {
		return "retained checkout has a merge in progress", nil
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("inspect retained work: %w", err)
	}
	if len(changes) != 0 {
		return "retained checkout has uncommitted or untracked changes", nil
	}
	// Git may overwrite ignored files when bringing tracked paths into the
	// checkout. Ordinary ignored artifacts are safe unless an incoming path
	// overlaps their bytes, including file/directory replacements.
	ignored, err := w.runner.run(ctx, readTimeout, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return "", fmt.Errorf("inspect ignored retained work: %w", err)
	}
	if ignored != "" {
		ignoreCase, configErr := w.runner.run(ctx, readTimeout, "config", "--bool", "--get", "core.ignorecase")
		if configErr != nil {
			var command *CommandError
			if !errors.As(configErr, &command) || command.ExitCode != 1 {
				return "", fmt.Errorf("inspect retained path case rules: %w", configErr)
			}
		}
		added, diffErr := w.runner.run(ctx, readTimeout, "diff", "--name-only", "--no-renames", "--diff-filter=A", "-z", "HEAD", target, "--")
		if diffErr != nil {
			return "", fmt.Errorf("inspect incoming tracked paths: %w", diffErr)
		}
		for _, local := range strings.Split(ignored, "\x00") {
			if local == "" {
				continue
			}
			for _, incoming := range strings.Split(added, "\x00") {
				if incoming == "" {
					continue
				}
				if syncPathsOverlap(local, incoming) {
					return "incoming tracked paths overlap ignored retained work", nil
				}
				if !syncPathsOverlap(strings.ToLower(local), strings.ToLower(incoming)) {
					continue
				}
				if ignoreCase == "true" {
					return "incoming tracked paths overlap ignored retained work", nil
				}
				// Configuration can disagree with the filesystem. Probe only
				// case-folded candidates, at their shared path depth, to detect
				// physical aliases without touching unrelated retained files.
				aliases, aliasErr := syncPhysicalAlias(w.path, local, incoming)
				if aliasErr != nil {
					return "", fmt.Errorf("inspect retained path aliases: %w", aliasErr)
				}
				if aliases {
					return "incoming tracked paths overlap ignored retained work", nil
				}
			}
		}
	}
	_, err = w.runner.run(ctx, readTimeout, "merge-base", "--is-ancestor", "HEAD", target)
	if err != nil {
		var command *CommandError
		if errors.As(err, &command) && command.ExitCode == 1 {
			return "retained local commits are not contained in synchronization commit", nil
		}
		return "", fmt.Errorf("inspect retained commit ancestry: %w", err)
	}
	return "", nil
}

func syncPathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func syncPhysicalAlias(root, local, incoming string) (bool, error) {
	localParts, incomingParts := strings.Split(local, "/"), strings.Split(incoming, "/")
	depth := min(len(localParts), len(incomingParts))
	localInfo, err := os.Lstat(filepath.Join(root, filepath.FromSlash(strings.Join(localParts[:depth], "/"))))
	if err != nil {
		return false, err
	}
	incomingInfo, err := os.Lstat(filepath.Join(root, filepath.FromSlash(strings.Join(incomingParts[:depth], "/"))))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(localInfo, incomingInfo), nil
}
