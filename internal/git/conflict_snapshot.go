package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// boundedConflictBlob resolves an immutable blob and checks size before capturing its bytes.
func (w *Worktree) boundedConflictBlob(ctx context.Context, revision, path string) (string, string, error) {
	if err := w.check(); err != nil {
		return "", "", err
	}
	if err := checkName("revision", revision); err != nil {
		return "", "", err
	}
	if err := checkName("path", path); err != nil {
		return "", "", err
	}
	out, err := w.runner.run(ctx, readTimeout, "ls-tree", "-z", revision, "--", path)
	if err != nil {
		return "", "", err
	}
	records := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(records) != 1 {
		return "", "", refuse(path, "missing or ambiguous regular file")
	}
	meta, name, ok := strings.Cut(records[0], "\t")
	fields := strings.Fields(meta)
	if !ok || name != path || len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755") {
		return "", "", refuse(path, "missing baseline, symlink or nonregular mode")
	}
	sizeText, err := w.runner.run(ctx, readTimeout, "cat-file", "-s", fields[2])
	if err != nil {
		return "", "", err
	}
	size, err := strconv.Atoi(strings.TrimSpace(sizeText))
	if err != nil || size < 0 || size > MaxConflictOriginalBytes {
		return "", "", refuse(path, "original exceeds 1 MiB")
	}
	content, err := w.runner.runRaw(ctx, showBlobTimeout, "cat-file", "blob", fields[2])
	if err != nil {
		return "", "", err
	}
	if len(content) != size {
		return "", "", refuse(path, "blob size changed")
	}
	return content, fields[0], nil
}

// CollectConflictSnapshots refuses the whole set on any excluded sibling.
// It runs under turn's clone gate, before invoking an advisor.
func (w *Worktree) CollectConflictSnapshots(ctx context.Context, treeSHA, branchSHA, baseSHA string, paths []string, totalMax int) ([]ConflictSnapshot, error) {
	if len(paths) == 0 || len(paths) > MaxConflictProposalFiles {
		return nil, refuse("", "conflict file count must be between 1 and 100")
	}
	for _, revision := range []string{treeSHA, branchSHA, baseSHA} {
		if err := checkName("revision", revision); err != nil {
			return nil, err
		}
	}
	ancestor, err := w.runner.run(ctx, readTimeout, "merge-base", branchSHA, baseSHA)
	if err != nil {
		return nil, fmt.Errorf("find conflict ancestor: %w", err)
	}
	ancestor = strings.TrimSpace(ancestor)
	var snapshots []ConflictSnapshot
	var faults []error
	total := 0
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			faults = append(faults, refuse(path, "duplicate conflict path"))
			continue
		}
		seen[path] = true
		original, mode, e := w.boundedConflictBlob(ctx, treeSHA, path)
		if e != nil {
			faults = append(faults, fmt.Errorf("%q: read original: %w; resolve manually", path, e))
			continue
		}
		baseline, bmode, e := w.boundedConflictBlob(ctx, branchSHA, path)
		if e != nil {
			faults = append(faults, fmt.Errorf("%q: read branch baseline: %w; resolve manually", path, e))
			continue
		}
		other, otherMode, e := w.boundedConflictBlob(ctx, baseSHA, path)
		if e != nil {
			faults = append(faults, e)
			continue
		}
		ancestorContent, ancestorMode, e := w.boundedConflictBlob(ctx, ancestor, path)
		if e != nil {
			faults = append(faults, refuse(path, "rename/delete/add conflict requires manual resolution"))
			continue
		}
		if mode != bmode || mode != otherMode || mode != ancestorMode {
			faults = append(faults, refuse(path, "mode conflict requires manual resolution"))
			continue
		}
		regions, e := ParseConflictRegions(path, original)
		if e != nil {
			faults = append(faults, e)
			continue
		}
		size := 0
		for _, r := range regions {
			size += len(r.Content)
		}
		total += size
		snapshots = append(snapshots, ConflictSnapshot{Path: path, Original: original, Baseline: baseline, Mode: mode, BranchSHA: branchSHA, Regions: regions, ancestor: ancestorContent, other: other, hasAncestor: true})
	}
	if total > totalMax {
		reason := fmt.Sprintf("complete conflict content requires %d bytes; settings.conflict_content_max_bytes is %d", total, totalMax)
		switch {
		case len(faults) != 0:
			reason += "; raising the setting does not resolve the independent safety refusals"
		case total <= MaxConflictRegionBytes:
			reason += fmt.Sprintf("; set settings.conflict_content_max_bytes to at least %d", max(total, 256))
		default:
			reason += fmt.Sprintf("; no supported setting suffices (maximum %d)", MaxConflictRegionBytes)
		}
		faults = append(faults, refuse("", reason))
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	return snapshots, nil
}
