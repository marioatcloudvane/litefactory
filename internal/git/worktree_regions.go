package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// validateRegionProposal reconstructs the canonical preview from fresh Git data.
// Legacy whole-file strings are never promoted into this format.
func (w *Worktree) validateRegionProposal(ctx context.Context, base, before string, files []ResolvedFile) ([]ConflictSnapshot, []ValidatedConflict, error) {
	var identityFaults []error
	for _, f := range files {
		if f.Format != 1 {
			identityFaults = append(identityFaults, refuse(f.Path, "unsupported or legacy proposal format; regenerate and confirm a new proposal"))
		}
	}
	if err := errors.Join(identityFaults...); err != nil {
		return nil, nil, err
	}
	baseSHA, err := revParse(ctx, w.runner, base, RevParseOptions{})
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		if f.BranchSHA != before || f.BaseSHA != baseSHA {
			identityFaults = append(identityFaults, refuse(f.Path, "stale proposal identities; regenerate"))
		}
	}
	if err := errors.Join(identityFaults...); err != nil {
		return nil, nil, err
	}
	preview, err := w.MergePreview(ctx, base)
	if err != nil {
		return nil, nil, err
	}
	conflicts := make(map[string]struct{}, len(preview.Conflicts))
	for _, p := range preview.Conflicts {
		conflicts[p] = struct{}{}
	}
	if err := conflictSetMismatch(conflicts, files); err != nil {
		return nil, nil, err
	}
	limit := files[0].ContentLimit
	if limit <= 0 {
		return nil, nil, refuse("", "missing conflict content budget")
	}
	snapshots, err := w.CollectConflictSnapshots(ctx, preview.TreeSHA, before, baseSHA, resolvedPaths(files), limit)
	if err != nil {
		return nil, nil, err
	}
	var candidates []ValidatedConflict
	var faults []error
	total := 0
	for i, s := range snapshots {
		f := files[i]
		for _, r := range f.Validated.Regions {
			total += len(r.Resolution)
		}
		if total > MaxConflictOriginalBytes {
			faults = append(faults, refuse(f.Path, "aggregate resolution exceeds 1 MiB"))
			continue
		}
		v, err := ValidateConflict(s, f.Validated.Regions)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if v.Mode != f.Validated.Mode || v.OriginalDigest != f.Validated.OriginalDigest || v.ResultDigest != f.Validated.ResultDigest || v.Metrics != f.Validated.Metrics || !reflect.DeepEqual(v.Regions, f.Validated.Regions) {
			faults = append(faults, refuse(f.Path, "stored content, digest, region order or statistics differ; regenerate"))
			continue
		}
		candidates = append(candidates, v)
	}
	if err := errors.Join(faults...); err != nil {
		return nil, nil, err
	}
	return snapshots, candidates, nil
}

func readActualConflict(root string, file ResolvedFile, files []ResolvedFile, limit int) (string, error) {
	path, err := containedResolvedPath(root, file.Path, files)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return "", refuse(file.Path, "nonregular or oversized working file")
	}
	mode := "100644"
	if info.Mode().Perm()&0111 != 0 {
		mode = "100755"
	}
	if file.Validated.Mode != "" && mode != file.Validated.Mode {
		return "", refuse(file.Path, "working file mode changed")
	}
	// #nosec G304 -- containedResolvedPath rechecks every component and Lstat rejects nonregular files.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", refuse(file.Path, "oversized working file")
	}
	return string(data), nil
}

func (w *Worktree) verifyResolvedBytes(ctx context.Context, files []ResolvedFile) error {
	for _, f := range files {
		if len(f.Resolution) > MaxConflictOriginalBytes+MaxConflictOutputBytes {
			return refuse(f.Path, "oversized validated result")
		}
		actual, err := readActualConflict(w.path, f, files, len(f.Resolution))
		if err != nil {
			return err
		}
		if f.Format != 1 {
			return refuse(f.Path, "unsupported proposal format; regenerate")
		}

		index, indexErr := w.runner.run(ctx, readTimeout, "ls-files", "--stage", "-z", "--", f.Path)
		if indexErr != nil {
			return indexErr
		}
		records := strings.Split(strings.TrimSuffix(index, "\x00"), "\x00")
		if len(records) != 1 {
			return refuse(f.Path, "ambiguous staged entry")
		}
		meta, path, ok := strings.Cut(records[0], "\t")
		fields := strings.Fields(meta)
		if !ok || path != f.Path || len(fields) != 3 || fields[2] != "0" || fields[0] != f.Validated.Mode {
			return refuse(f.Path, "staged mode or stage differs")
		}
		sizeText, sizeErr := w.runner.run(ctx, readTimeout, "cat-file", "-s", fields[1])
		if sizeErr != nil {
			return sizeErr
		}
		size, sizeErr := strconv.Atoi(strings.TrimSpace(sizeText))
		if sizeErr != nil || size != len(f.Resolution) {
			return refuse(f.Path, "staged result size differs")
		}
		staged, err := w.runner.runRaw(ctx, showBlobTimeout, "cat-file", "blob", fields[1])

		if err != nil {
			return fmt.Errorf("read staged resolution %s: %w", f.Path, err)
		}
		if actual != f.Resolution || staged != f.Resolution || ConflictDigest(staged) != f.Validated.ResultDigest {
			return refuse(f.Path, "written or staged bytes differ from validated result")
		}
	}
	return nil
}
