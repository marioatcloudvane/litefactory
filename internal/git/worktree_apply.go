package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolvedFile is one conflicting path and the confirmed, fully resolved
// replacement content for it (US-12).
//
// It is this package's own shape for what an owner confirmed, deliberately
// distinct from internal/agent's own conflict-resolution types: this package
// takes no dependency on internal/agent, and the caller (internal/turn) is
// what maps a confirmed conflict-resolution proposal's stored files onto
// this type at the one call site that needs both.
type ResolvedFile struct {
	// Path is the conflicting file's path, relative to the worktree root,
	// exactly as the proposal recorded it.
	Path string
	// Resolution is the confirmed replacement content for Path.
	Resolution         string
	Format             int
	BranchSHA, BaseSHA string
	ContentLimit       int
	Validated          ValidatedConflict
	beforeContent      *string
}

// ErrConflictMismatch reports that the paths git actually finds conflicted do
// not exactly match the confirmed file list a caller is applying (S-11,
// watch-out #7 of US-12): either a listed path never conflicted, or a real
// conflict exists outside the confirmed list. Either way, applying only the
// listed files could not finish the merge honestly, so nothing is written and
// the in-progress merge is aborted before this is ever returned.
var ErrConflictMismatch = errors.New("confirmed files do not match the merge's conflicted paths")

// ErrPathNotContained reports a resolved file path this package refuses to
// write: absolute, escaping the worktree, carrying a symlink component, or
// simply not one of the confirmed paths (S-13). Containment is enforced here,
// inside *Worktree, and never by string-prefix comparison — see
// containedResolvedPath.
var ErrPathNotContained = errors.New("path is not contained in the worktree or the confirmed file list")

// WriteResolvedFiles starts the merge a confirmed conflict-resolution
// proposal (US-12) resolves, and writes its confirmed content over exactly
// the conflicting paths it names.
//
// It runs "git merge --no-commit --no-ff --end-of-options base" first — the
// same merge [Worktree.MergeBase] performs, except it never commits on its
// own, because this call's whole job is to fix the paths a human confirmed a
// resolution for before anything is committed. base and the worktree's
// branch are screened by [checkName], the same way [Worktree.MergeBase]
// screens them; base is confirmed to resolve before the merge is attempted;
// a dirty worktree is refused before either happens (S-5), the same
// precondition [Worktree.MergeBase] enforces.
//
// A clean merge — base already fully covered by branch, nothing left to
// resolve — is not silently accepted: it means the confirmed resolution no
// longer matches reality (the branch or base moved since the proposal was
// computed), so it is undone and reported as [ErrConflictMismatch] rather
// than committed with content nobody actually confirmed (AC-12.4).
//
// Once the merge conflicts, [Worktree.Status]'s own [ChangeUnmerged]
// classification is read back and compared against files: the two path sets
// must be exactly equal (watch-out #7 of US-12). A conflicted path files does
// not cover, or a listed path that did not actually conflict, both abort the
// merge and report [ErrConflictMismatch] — applying only the listed files
// must never leave a real conflict behind, and must never write over a path
// the confirmation never named.
//
// Every path in files is then screened by containedResolvedPath before a
// single byte is written (S-13): an absolute path, a ".." component, a
// symlinked ancestor, or a path files does not itself list aborts the merge
// and refuses the whole call — never a partial write. On any failure past the
// point the merge actually started, the in-progress merge is aborted and the
// branch tip is confirmed back at the value it held before this call (S-5); a
// mismatch there is its own reported failure, never silently accepted.
func (w *Worktree) WriteResolvedFiles(ctx context.Context, base string, files []ResolvedFile) error {
	if err := w.check(); err != nil {
		return err
	}
	if err := checkName("base", base); err != nil {
		return err
	}
	if err := checkName("branch", w.branch); err != nil {
		return err
	}
	if err := checkResolvedFiles(files); err != nil {
		return err
	}

	changes, err := w.Status(ctx)
	if err != nil {
		return fmt.Errorf("check worktree status before applying a confirmed resolution to %s: %w", w.branch, err)
	}
	if len(changes) > 0 {
		return fmt.Errorf("apply confirmed resolution to %s: %w", w.branch, ErrWorktreeUncommittedChanges)
	}
	if _, err := revParse(ctx, w.runner, base, RevParseOptions{}); err != nil {
		return fmt.Errorf("resolve base %s before applying a confirmed resolution to %s: %w", base, w.branch, err)
	}
	before, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return fmt.Errorf("resolve HEAD on branch %s before applying a confirmed resolution: %w", w.branch, err)
	}

	snapshots, candidates, validationErr := w.validateRegionProposal(ctx, base, before, files)
	if validationErr != nil {
		return validationErr
	}

	var snapshotFaults []error
	for i, f := range files {
		content, readErr := readActualConflict(w.path, f, files, MaxConflictOriginalBytes)
		if readErr != nil {
			snapshotFaults = append(snapshotFaults, readErr)
			continue
		}
		files[i].beforeContent = &content
	}

	if err := errors.Join(snapshotFaults...); err != nil {
		return err
	}
	_, exitCode, err := w.runner.runAllowingExit(ctx, mergeTimeout, []int{0, 1},
		"merge", "--no-commit", "--no-ff", "--end-of-options", base)
	if err != nil {
		if _, headErr := revParse(ctx, w.runner, "MERGE_HEAD", RevParseOptions{}); headErr == nil {
			return w.abortResolvedMerge(ctx, before, files, err)
		}
		return classify(err, ErrRevisionNotFound, 128)
	}
	if exitCode == 0 {
		return w.abortResolvedMerge(ctx, before, files, fmt.Errorf("%w: merge of %s into %s produced no conflicts", ErrConflictMismatch, base, w.branch))
	}

	unmerged, statusErr := w.unmergedPaths(ctx)
	if statusErr != nil {
		return w.abortResolvedMerge(ctx, before, files,
			fmt.Errorf("read conflicted paths after merging %s into %s: %w", base, w.branch, statusErr))
	}
	if mismatchErr := conflictSetMismatch(unmerged, files); mismatchErr != nil {
		return w.abortResolvedMerge(ctx, before, files, mismatchErr)
	}

	// Validate every actual merge file before the first replacement write.
	var validationFaults []error
	for i, snapshot := range snapshots {
		actual, readErr := readActualConflict(w.path, files[i], files, MaxConflictOriginalBytes)
		if readErr != nil {
			validationFaults = append(validationFaults, readErr)
			continue
		}
		if matchErr := EquivalentConflictOriginals(snapshot.Path, snapshot.Original, actual); matchErr != nil {
			validationFaults = append(validationFaults, matchErr)
			continue
		}
		regions, parseErr := ParseConflictRegions(snapshot.Path, actual)
		if parseErr != nil {
			validationFaults = append(validationFaults, parseErr)
			continue
		}
		resolutions := make([]RegionResolution, len(regions))
		for j, r := range regions {
			resolutions[j] = RegionResolution{ID: r.ID, Resolution: candidates[i].Regions[j].Resolution}
		}
		snapshot.Original = actual
		output, outputErr := ValidateConflict(snapshot, resolutions)
		if outputErr != nil {
			validationFaults = append(validationFaults, outputErr)
			continue
		}
		if output.ResultDigest != candidates[i].ResultDigest {
			validationFaults = append(validationFaults, refuse(snapshot.Path, "actual reconstruction differs; regenerate"))
			continue
		}
		files[i].Resolution = output.Result
	}

	if err := errors.Join(validationFaults...); err != nil {
		return w.abortResolvedMerge(ctx, before, files, err)
	}

	for _, f := range files {
		target, containErr := containedResolvedPath(w.path, f.Path, files)
		if containErr != nil {
			return w.abortResolvedMerge(ctx, before, files, containErr)
		}
		// #nosec G306 -- SEC-10 umask 077 narrows newly created files; existing worktree modes are preserved.
		if err := os.WriteFile(target, []byte(f.Resolution), 0o644); err != nil {
			return w.abortResolvedMerge(ctx, before, files, fmt.Errorf("write resolved content for %s: %w", f.Path, err))
		}
	}
	return nil
}

// StageResolvedFiles stages exactly the confirmed paths files names — "git
// add --" one path list rather than "git add --all" the way [Worktree.Commit]
// stages a run's own changes — because staging here must never pick up
// anything this call did not itself write (S-13): a confirmed resolution
// stages the confirmed files, and nothing else the worktree happens to be
// holding.
//
// Every path is re-screened by containedResolvedPath independently of
// [Worktree.WriteResolvedFiles] (S-13): this package never trusts a path
// that reached it once already, and enforces containment again at every
// mutating call rather than relying on an earlier check's memory of having
// done so. On any failure the in-progress merge is aborted and the branch tip
// is confirmed back where it was before [Worktree.WriteResolvedFiles] started
// it (S-5); "before" is re-derived from the same "git rev-parse HEAD"
// [Worktree.WriteResolvedFiles] itself reads, since HEAD does not move while
// a merge is only staged and not yet committed.
func (w *Worktree) StageResolvedFiles(ctx context.Context, files []ResolvedFile) error {
	if err := w.check(); err != nil {
		return err
	}
	if err := checkResolvedFiles(files); err != nil {
		return err
	}
	before, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return fmt.Errorf("resolve HEAD on branch %s before staging a confirmed resolution: %w", w.branch, err)
	}
	for _, f := range files {
		if _, containErr := containedResolvedPath(w.path, f.Path, files); containErr != nil {
			return w.abortResolvedMerge(ctx, before, files, containErr)
		}
	}
	var formatFaults []error
	for _, f := range files {
		if f.Format != 1 || f.Validated.ResultDigest == "" {
			formatFaults = append(formatFaults, refuse(f.Path, "missing supported region evidence; regenerate"))
		}
	}
	if err := errors.Join(formatFaults...); err != nil {
		return w.abortResolvedMerge(ctx, before, files, err)
	}

	args := append([]string{"add", "--"}, resolvedPaths(files)...)
	if _, err := w.runner.run(ctx, commitTimeout, args...); err != nil {
		return w.abortResolvedMerge(ctx, before, files, fmt.Errorf("stage confirmed resolution in %s: %w", w.path, err))
	}
	if err := w.verifyResolvedBytes(ctx, files); err != nil {
		return w.abortResolvedMerge(ctx, before, files, err)
	}
	return nil
}

// CompleteMergeOptions carries what [Worktree.CompleteMerge] needs to finish
// an in-progress merge [Worktree.WriteResolvedFiles] started and
// [Worktree.StageResolvedFiles] resolved in the index (US-12).
type CompleteMergeOptions struct {
	// Task and Mode carry the ordinary TrailerTask/TrailerMode trailers
	// every other litefactory commit carries. There is no per-task run
	// ordinal for this agentless turn — it never allocates a runs row — so
	// there is deliberately no TrailerRun here.
	Task, Mode string
	// Author is the identity the merge commit is authored and committed as,
	// exactly as [MergeOptions.Author] is for [Worktree.MergeBase].
	Author Author
	// ProposalID, ConfirmedBy and Model are litefactory-controlled values —
	// never model output — that become this story's three new trailers
	// (S-16): TrailerConflictProposal, TrailerConfirmedBy and TrailerModel.
	Files       []ResolvedFile
	ProposalID  int64
	ConfirmedBy string
	Model       string
}

func (o CompleteMergeOptions) validate() error {
	if err := checkTrailerValue(TrailerTask, o.Task); err != nil {
		return err
	}
	if err := checkTrailerValue(TrailerMode, o.Mode); err != nil {
		return err
	}
	if err := checkConfigValue("author name", o.Author.Name); err != nil {
		return err
	}
	if err := checkConfigValue("author email", o.Author.Email); err != nil {
		return err
	}
	if o.ProposalID < 1 {
		return fmt.Errorf("proposal id %d: %w: must be positive", o.ProposalID, ErrInvalidArgument)
	}
	if err := checkTrailerValue(TrailerConfirmedBy, o.ConfirmedBy); err != nil {
		return err
	}
	return checkTrailerValue(TrailerModel, o.Model)
}

// CompleteMerge finishes the merge [Worktree.WriteResolvedFiles] started and
// [Worktree.StageResolvedFiles] staged (US-12).
//
// Every conflicted path must already be resolved and staged, or this refuses
// rather than letting "git commit" do whatever it does with a still-
// conflicted index (watch-out #7 of US-12): applying a proposal that excluded
// some files must never complete a merge while real conflicts remain.
//
// "git commit" is what actually finishes it: MERGE_HEAD is already present
// from the merge [Worktree.WriteResolvedFiles] started, so git makes the
// ordinary two-parent merge commit on its own — this reuses the same
// identity-prelude commit mechanism [Worktree.Commit] and [Worktree.MergeBase]
// already carry ("-c user.name=... -c user.email=... -c
// commit.gpgsign=false"), not a second one.
//
// On any failure the in-progress merge is aborted and the branch tip is
// confirmed back where it was before the merge started (S-5), the same
// abort-then-verify discipline [Worktree.MergeBase] applies to its own
// failures.
func (w *Worktree) CompleteMerge(ctx context.Context, opts CompleteMergeOptions) (MergeResult, error) {
	if err := w.check(); err != nil {
		return MergeResult{}, err
	}
	before, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve HEAD on branch %s before completing a confirmed merge: %w", w.branch, err)
	}
	if err := opts.validate(); err != nil {
		return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files, err)
	}

	unmerged, err := w.unmergedPaths(ctx)
	if err != nil {
		return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files,
			fmt.Errorf("read conflicted paths before completing a confirmed merge on %s: %w", w.branch, err))
	}
	if len(unmerged) > 0 {
		paths := sortedKeys(unmerged)
		return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files,
			fmt.Errorf("%w: %v still conflicted", ErrConflictMismatch, paths))
	}

	if len(opts.Files) == 0 {
		return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files, refuse("", "missing validated region files"))
	}
	if len(opts.Files) > 0 {
		if err := w.verifyResolvedBytes(ctx, opts.Files); err != nil {
			return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files, err)
		}
	}
	message := completeMergeMessage(opts)
	args := []string{
		"-c", "user.name=" + opts.Author.Name,
		"-c", "user.email=" + opts.Author.Email,
		"-c", "commit.gpgsign=false",
		"commit", "--quiet", "--message", message,
	}
	if _, err := w.runner.run(ctx, commitTimeout, args...); err != nil {
		return MergeResult{}, w.abortResolvedMerge(ctx, before, opts.Files, fmt.Errorf("complete confirmed merge on %s: %w", w.branch, err))
	}

	head, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve HEAD on branch %s after completing a confirmed merge: %w", w.branch, err)
	}
	return MergeResult{Head: head, Merged: head != before}, nil
}

// completeMergeMessage assembles the merge commit's message: a fixed,
// litefactory-authored subject — never model text, since this turn is
// agentless and has no summary to sanitise — a blank line, and the ordinary
// trailers plus this story's three new ones.
func completeMergeMessage(opts CompleteMergeOptions) string {
	var b strings.Builder
	b.WriteString("Apply confirmed conflict-resolution proposal")
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%s: %s\n", TrailerTask, opts.Task)
	fmt.Fprintf(&b, "%s: %s\n", TrailerMode, opts.Mode)
	fmt.Fprintf(&b, "%s: %d\n", TrailerConflictProposal, opts.ProposalID)
	fmt.Fprintf(&b, "%s: %s\n", TrailerConfirmedBy, opts.ConfirmedBy)
	fmt.Fprintf(&b, "%s: %s\n", TrailerModel, opts.Model)
	return b.String()
}

// abortInProgressMerge aborts a merge this file's own methods started via
// "git merge --no-commit --no-ff", then confirms the branch tip is back at
// before — the same abort-then-verify discipline [Worktree.MergeBase]'s own
// recoverFromFailedMerge applies (US-10, S-5): a caller here never has to
// wonder whether a failed application left MERGE_HEAD, index.lock or a
// partially written file behind, because "git merge --abort" discards the
// index and working-tree changes both the merge and this file's own writes
// made, restoring both to before. cause is returned unwrapped-further on
// success, so the caller sees exactly what actually went wrong; a failed
// abort or a branch tip that did not come back to before are reported as
// their own, more urgent failure instead.
func (w *Worktree) abortInProgressMerge(ctx context.Context, before string, cause error, snapshots ...[]ResolvedFile) error {
	if _, abortErr := w.runner.run(ctx, mergeTimeout, "merge", "--abort"); abortErr != nil {
		return fmt.Errorf("%w (aborting the in-progress merge also failed: %w)", cause, abortErr)
	}
	after, revErr := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if revErr != nil {
		return fmt.Errorf("resolve HEAD on branch %s after aborting a failed application: %w", w.branch, revErr)
	}
	if after != before {
		return fmt.Errorf("apply confirmed resolution to %s: branch tip moved from %s to %s while aborting (original error: %w)",
			w.branch, before, after, cause)
	}
	var restorationFaults []error
	for _, files := range snapshots {
		for _, f := range files {
			if f.beforeContent == nil {
				continue
			}
			restored, err := readActualConflict(w.path, f, files, len(*f.beforeContent))
			if err != nil || restored != *f.beforeContent {
				restorationFaults = append(restorationFaults, fmt.Errorf("tracked bytes for %q differ from before the merge", f.Path))
			}
		}
	}
	if err := errors.Join(restorationFaults...); err != nil {
		return fmt.Errorf("%w (restoration failed: %w)", cause, err)
	}
	return cause
}

// unmergedPaths reads the worktree's current status and returns the set of
// paths [classifyStatus] marks [ChangeUnmerged].
func (w *Worktree) unmergedPaths(ctx context.Context) (map[string]struct{}, error) {
	changes, err := w.Status(ctx)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]struct{}, len(changes))
	for _, c := range changes {
		if c.Kind == ChangeUnmerged {
			paths[c.Path] = struct{}{}
		}
	}
	return paths, nil
}

// conflictSetMismatch reports whether the paths git actually finds conflicted
// exactly equal the confirmed file list, naming both differences when they do
// not (watch-out #7 of US-12).
func conflictSetMismatch(unmerged map[string]struct{}, files []ResolvedFile) error {
	listed := make(map[string]struct{}, len(files))
	for _, f := range files {
		listed[f.Path] = struct{}{}
	}
	var extra, missing []string
	for p := range unmerged {
		if _, ok := listed[p]; !ok {
			extra = append(extra, p)
		}
	}
	for p := range listed {
		if _, ok := unmerged[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(extra) == 0 && len(missing) == 0 {
		return nil
	}
	sort.Strings(extra)
	sort.Strings(missing)
	return fmt.Errorf("%w: conflicted but unconfirmed %v, confirmed but not conflicted %v",
		ErrConflictMismatch, extra, missing)
}

// checkResolvedFiles screens the argument shape of a resolved-file list
// before any of it reaches git or the filesystem: at least one entry, and
// every path passes [checkName] the same way every other caller-supplied
// name in this package does.
//
// Every entry is checked, and every problem found is reported: one malformed
// path among several says nothing about the others, so this collects one
// error per bad entry with errors.Join rather than stopping at the first —
// the same aggregate-don't-fail-fast shape [repos.ValidateWorktreeRoots]
// already applies to its own independent, per-item checks. A caller fixing a
// confirmed file list one comment at a time would otherwise see only the
// first problem and have to resubmit to find the second.
func checkResolvedFiles(files []ResolvedFile) error {
	if len(files) == 0 {
		return fmt.Errorf("resolved files: %w: at least one file is required", ErrInvalidArgument)
	}
	var errs []error
	for i, f := range files {
		if err := checkName(fmt.Sprintf("resolved file %d path", i), f.Path); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// resolvedPaths projects files down to their paths, in order, for building an
// argv.
func resolvedPaths(files []ResolvedFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

// sortedKeys returns the keys of a string set in sorted order, for a
// deterministic error message.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// containedResolvedPath is the whole of S-13's enforcement: it never trusts a
// path from a caller, only what it can verify itself against the worktree's
// own filesystem and the confirmed file list. path must appear, byte for
// byte, in allowed; its cleaned form must be a relative path that does not
// climb outside the worktree with "..", and every existing ancestor
// directory it resolves through inside the worktree must not be a symlink —
// only then is the joined absolute path returned for writing.
func containedResolvedPath(root, path string, allowed []ResolvedFile) (string, error) {
	found := false
	for _, f := range allowed {
		if f.Path == path {
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("%w: %q is not one of the confirmed files", ErrPathNotContained, path)
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q is absolute", ErrPathNotContained, path)
	}

	cleaned := filepath.Clean(path)
	if cleaned != path || cleaned == "." || cleaned == ".." {
		return "", fmt.Errorf("%w: %q is not a clean, contained relative path", ErrPathNotContained, path)
	}
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("%w: %q traverses outside the worktree", ErrPathNotContained, path)
		}
	}

	target := filepath.Join(root, cleaned)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes the worktree", ErrPathNotContained, path)
	}

	// Refuse a symlink anywhere along an existing ancestor: a symlinked
	// directory could otherwise redirect this write outside the worktree
	// even though the joined path looks contained.
	dir := root
	for _, part := range strings.Split(filepath.Dir(cleaned), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		dir = filepath.Join(dir, part)
		info, statErr := os.Lstat(dir)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				break
			}
			return "", fmt.Errorf("inspect %s before writing a confirmed resolution: %w", dir, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %q has a symlinked ancestor", ErrPathNotContained, path)
		}
	}
	if info, statErr := os.Lstat(target); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %q is itself a symlink", ErrPathNotContained, path)
	}

	return target, nil
}

// abortResolvedMerge also verifies the raw pre-merge candidate bytes; Git
// filters can make merge --abort succeed while checkout changes those bytes.
func (w *Worktree) abortResolvedMerge(ctx context.Context, before string, files []ResolvedFile, cause error) error {
	return w.abortInProgressMerge(ctx, before, cause, files)
}
