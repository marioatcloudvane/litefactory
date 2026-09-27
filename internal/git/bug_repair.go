package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// BugRepairSnapshot is an immutable, bounded observation of the actual repair.
// Diff includes changes already present in the reservation's source commit only
// when they changed again; no worktree reset or cleanup is performed.
type BugRepairSnapshot struct {
	Commit, Tree, BaseCommit, BaseTree, DiffDigest string
	Paths                                          []string
	Diff                                           string
}

// ObserveBugRepair observes a clean persisted source and compares it with the
// reserved immutable input. Rename detection is disabled so both path boundaries
// are visible. Unsupported file kinds and excessive diffs fail closed.
func (w *Worktree) ObserveBugRepair(ctx context.Context, base, baseTree, commit string) (BugRepairSnapshot, error) {
	var out BugRepairSnapshot
	if err := w.check(); err != nil {
		return out, err
	}
	if !fullBugObject(base) || !fullBugObject(baseTree) || !fullBugObject(commit) {
		return out, errors.New("bug repair requires exact source objects")
	}
	actualBaseTree, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", base+"^{tree}")
	if err != nil {
		return out, err
	}
	if actualBaseTree != baseTree {
		return out, errors.New("bug repair reserved source tree mismatch")
	}
	tree, err := w.runner.run(ctx, readTimeout, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return out, err
	}
	if err = w.ObserveBugCheckSource(ctx, commit, tree); err != nil {
		return out, err
	}
	names, err := w.runner.runRaw(ctx, readTimeout, "diff", "--no-ext-diff", "--no-renames", "--name-only", "-z", base, commit, "--")
	if err != nil {
		return out, err
	}
	paths := strings.Split(strings.TrimSuffix(names, "\x00"), "\x00")
	if names == "" {
		paths = nil
	}
	if len(paths) > 256 {
		return out, errors.New("bug repair exceeds 256 changed paths")
	}
	// Bound each immutable blob before requesting a patch; no unbounded file
	// content is read merely to discover that it exceeds the evidence limit.
	total := 0
	for _, rev := range []string{base, commit} {
		for _, path := range paths {
			entries, readErr := w.runner.run(ctx, readTimeout, "ls-tree", rev, "--", path)
			if readErr != nil {
				return out, readErr
			}
			if entries == "" {
				continue
			}
			fields := strings.SplitN(entries, "\t", 2)
			if len(fields) != 2 {
				return out, errors.New("bug repair malformed tree entry")
			}
			mode := strings.Fields(fields[0])
			if len(mode) != 3 || (mode[0] != "100644" && mode[0] != "100755") || mode[1] != "blob" {
				return out, errors.New("bug repair scope cannot certify symlink or submodule changes")
			}
			sizeText, readErr := w.runner.run(ctx, readTimeout, "cat-file", "-s", mode[2])
			if readErr != nil {
				return out, readErr
			}
			size, readErr := parseBugRepairSize(sizeText)
			if readErr != nil {
				return out, readErr
			}
			total += size
			if total > 1<<20 {
				return out, errors.New("bug repair diff inputs exceed 1 MiB")
			}
		}
	}
	diff, err := w.runner.runRaw(ctx, readTimeout, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--binary", base, commit, "--")
	if err != nil {
		return out, err
	}
	if len(diff) > 2<<20 {
		return out, errors.New("bug repair diff exceeds evidence bound")
	}
	if err = w.ObserveBugCheckSource(ctx, commit, tree); err != nil {
		return out, err
	}
	sum := sha256.Sum256([]byte(diff))
	return BugRepairSnapshot{Commit: commit, Tree: tree, BaseCommit: base, BaseTree: baseTree, DiffDigest: hex.EncodeToString(sum[:]), Paths: paths, Diff: diff}, nil
}

func parseBugRepairSize(raw string) (int, error) {
	size, err := strconv.Atoi(raw)
	if err != nil || size < 0 || size > 1<<20 {
		return 0, errors.New("bug repair blob exceeds evidence bound")
	}
	return size, nil
}
