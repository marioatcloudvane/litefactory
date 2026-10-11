package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Divergence reports how far a worktree's branch has drifted from a base ref,
// measured fresh at the moment [Worktree.Divergence] is called.
//
// Behind is the number of commits Base has that Branch does not — what a
// merge or rebase would need to bring in. Ahead is the number of commits
// Branch has that Base does not. Base and Branch are the exact ref names the
// count was taken against, so a caller can report them without having to
// remember what it passed in.
type Divergence struct {
	// Ahead is how many commits Branch has that Base does not.
	Ahead int
	// Behind is how many commits Base has that Branch does not.
	Behind int
	// Base is the ref the comparison was measured against, exactly as passed
	// in — typically "origin/<base branch>".
	Base string
	// Branch is the worktree's own branch, the same value [Worktree.Branch]
	// returns.
	Branch string
}

// Divergence measures how far w's branch has drifted from base, using
// "git rev-list --left-right --count base...branch". It is read-only: it
// walks history in the local object store and touches neither the working
// tree nor any ref.
//
// base is almost always a remote-tracking ref such as "origin/main" — this
// method does not fetch, so the caller is responsible for updating base
// before measuring against it. base and the worktree's branch are screened by
// [checkName] before either reaches the range expression this method builds;
// a base that no longer resolves — a deleted branch, a renamed default branch
// — is reported as [ErrRevisionNotFound] rather than a raw exit code or a
// panic.
func (w *Worktree) Divergence(ctx context.Context, base string) (Divergence, error) {
	if err := w.check(); err != nil {
		return Divergence{}, err
	}
	if err := checkName("base", base); err != nil {
		return Divergence{}, err
	}
	if err := checkName("branch", w.branch); err != nil {
		return Divergence{}, err
	}

	// The range is assembled here, from the two names just screened, rather
	// than accepted pre-joined from a caller: that is what keeps a
	// caller-supplied range from ever reaching git unchecked.
	//
	// "base...branch" makes base the left side and branch the right side of
	// "--left-right --count", whose output order is "<left> <right>": the
	// left count is commits only base can reach (Behind), the right count is
	// commits only branch can reach (Ahead). Swapping the two sides of "..."
	// silently swaps Ahead and Behind, which is why this order is pinned by a
	// regression test rather than left to be re-derived from memory.
	rangeSpec := base + "..." + w.branch

	// --end-of-options is a second lock on the door checkName already closed:
	// it stops git from ever reading rangeSpec as an option, exactly as
	// RevParse does for a single revision.
	out, err := w.runner.run(ctx, divergenceTimeout, "rev-list", "--left-right", "--count", "--end-of-options", rangeSpec)
	if err != nil {
		return Divergence{}, classify(err, ErrRevisionNotFound, 128)
	}

	behind, ahead, err := parseDivergenceCounts(out)
	if err != nil {
		return Divergence{}, fmt.Errorf("parse divergence for %s: %w", rangeSpec, err)
	}
	return Divergence{Ahead: ahead, Behind: behind, Base: base, Branch: w.branch}, nil
}

// parseDivergenceCounts decodes "git rev-list --left-right --count" output:
// two whitespace-separated non-negative integers, left then right.
func parseDivergenceCounts(out string) (behind, ahead int, err error) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("%q: %w: expected \"<left> <right>\"", out, ErrInvalidArgument)
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("left count %q: %w: %w", fields[0], ErrInvalidArgument, err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("right count %q: %w: %w", fields[1], ErrInvalidArgument, err)
	}
	return behind, ahead, nil
}

// MergePreview reports whether merging base into w's branch would succeed
// cleanly.
type MergePreview struct {
	// Clean reports whether the merge would complete without conflicts.
	Clean bool
	// Conflicts lists the paths that would conflict. It is empty when Clean
	// is true, and capped at [MaxMergeConflictPaths] regardless of how many
	// git actually reported.
	Conflicts []string
	// TreeSHA is the object id of the tree merge-tree computed — the result
	// of the merge whether or not it is clean. It is not attached to any ref.
	TreeSHA string
}

// MergePreview reports whether merging base into w's branch would succeed
// cleanly, using "git merge-tree --write-tree --name-only". It touches
// neither the working tree, the index nor any ref — the branch is left
// exactly as it was, whichever way the merge would go — which is what makes
// it safe to call before deciding whether [Worktree.MergeBase] should run at
// all.
//
// It is not fully side-effect free at the repository level: --write-tree
// always writes the resulting tree object into the shared object database,
// and on a conflicted merge it also writes a blob per conflicted file so
// that git has something for the reported tree to point at. Those are loose
// objects like any other git writes; they do not move any ref and do not
// touch the working tree, and an eventual "git gc" reclaims them once
// nothing else references them.
//
// base and the worktree's branch are screened by [checkName] before being
// used to build the command, exactly as [Worktree.Divergence] does.
//
// "git merge-tree --write-tree" is a git ≥2.38 feature. A resolved git older
// than that is refused here, before any command runs, as
// [ErrUnsupportedGitVersion] — never a raw [*CommandError] carrying whatever
// "unknown option" text that git version happens to print, and never a boot
// failure: the caller decides how to report it turn by turn.
func (w *Worktree) MergePreview(ctx context.Context, base string) (MergePreview, error) {
	if err := w.check(); err != nil {
		return MergePreview{}, err
	}
	if err := checkName("base", base); err != nil {
		return MergePreview{}, err
	}
	if err := checkName("branch", w.branch); err != nil {
		return MergePreview{}, err
	}
	if !w.runner.mergeTreeSupported {
		// Refused before exec rather than after: an older git rejects
		// --write-tree with a plain "unknown option" usage error, which would
		// otherwise surface as an unclassified *CommandError instead of this
		// named, operator-readable condition.
		return MergePreview{}, fmt.Errorf("%w", ErrUnsupportedGitVersion)
	}

	// merge-tree exits 0 for a clean merge and 1 for a conflicted one, and
	// either way the payload we need — the tree id, and on conflict the
	// paths — is on stdout rather than stderr. runAllowingExit is what lets
	// both of those be read as a reported outcome instead of an error.
	//
	// -z is not optional: without it a conflicting path carrying a
	// non-ASCII byte, a tab, a backslash or a double quote comes back
	// C-quoted and escaped (core.quotePath) rather than as the literal path
	// — exactly the hazard [statusArgs] already carries a -z for on "git
	// status". parseMergePreview is written against -z's NUL-terminated,
	// unquoted shape; it does not know how to read the quoted, newline
	// form, so this flag and that parser move together.
	out, exitCode, err := w.runner.runAllowingExit(ctx, mergePreviewTimeout, []int{0, 1},
		"merge-tree", "--write-tree", "--name-only", "-z", "--end-of-options", w.branch, base)
	if err != nil {
		return MergePreview{}, classify(err, ErrRevisionNotFound, 128)
	}
	preview, err := parseMergePreview(out, exitCode == 0)
	if err != nil {
		return MergePreview{}, fmt.Errorf("merge-tree %s %s: %w", w.branch, base, err)
	}
	return preview, nil
}

// ConflictBlob reads the content merge-tree wrote for path within treeSHA —
// the tree object [Worktree.MergePreview] returned for a conflicted merge.
// For a textual conflict that content is the same shape a real working-tree
// merge would have left: the non-conflicting parts resolved, and each
// conflicting region wrapped in the ordinary "<<<<<<<"/"======="/">>>>>>>"
// markers. It is read-only: "git show" touches neither the working tree,
// the index nor any ref, so this is safe to call for every conflicting path
// [git.MergePreview.Conflicts] reported, in any order, without disturbing
// the branch [Worktree.MergePreview] promised to leave untouched.
//
// Content is returned exactly as git wrote it, with no interpretation:
// binary detection, non-UTF-8 handling, capping and hunk extraction are the
// caller's policy (internal/turn), not this package's. treeSHA and path are
// screened by [checkName] before either reaches git, the same way every
// other caller-supplied value in this package is.
func (w *Worktree) ConflictBlob(ctx context.Context, treeSHA, path string) (string, error) {
	if err := w.check(); err != nil {
		return "", err
	}
	if err := checkName("tree", treeSHA); err != nil {
		return "", err
	}
	if err := checkName("path", path); err != nil {
		return "", err
	}
	out, err := w.runner.run(ctx, showBlobTimeout, "show", "--end-of-options", treeSHA+":"+path)
	if err != nil {
		return "", classify(err, ErrRevisionNotFound, 128)
	}
	return out, nil
}

// MaxMergeConflictPaths caps how many conflicted paths [Worktree.MergePreview]
// reports. The cap is applied here, at parse time, rather than only where a
// caller later renders the list (S-9): a merge with thousands of conflicting
// paths must not be free to grow an unbounded slice this deep in the call
// stack just because some future renderer happens to truncate its own output.
const MaxMergeConflictPaths = 500

// parseMergePreview decodes "git merge-tree --write-tree --name-only -z"
// output.
//
// Per "git help merge-tree"'s OUTPUT section, -z NUL-terminates every field
// instead of newline-separating them, and unquotes every path instead of
// C-quoting it (empirically confirmed against a real conflicted merge with a
// non-ASCII filename, rather than assumed from the docs alone — see
// TestWorktreeMergePreviewConflictNonASCIIPath). A clean merge is just the
// tree id, NUL-terminated. A conflicted merge is the tree id, NUL-terminated,
// followed by one NUL-terminated conflicted path per file --name-only is
// reporting on; the doc for -z says the free-form "informational messages"
// section that follows (things like "Auto-merging <path>" and
// "CONFLICT (...): ...", which this package has no use for) begins with a NUL
// character rather than a blank line. Splitting the whole output on NUL turns
// that into an empty field sitting right after the last conflicted path —
// the parser stops there rather than needing to know anything about the
// messages section's own internal shape.
func parseMergePreview(out string, clean bool) (MergePreview, error) {
	if out == "" {
		return MergePreview{}, fmt.Errorf("%w: missing tree id", ErrInvalidArgument)
	}
	fields := strings.Split(out, "\x00")

	if clean {
		return MergePreview{Clean: true, TreeSHA: fields[0]}, nil
	}

	if len(fields) < 2 {
		return MergePreview{}, fmt.Errorf("%q: %w: missing conflicted file list", out, ErrInvalidArgument)
	}
	preview := MergePreview{TreeSHA: fields[0]}

	for _, path := range fields[1:] {
		if path == "" {
			// The NUL that begins the informational messages section: the
			// conflicted file list is done, and nothing past here is ours to
			// read.
			break
		}
		if len(preview.Conflicts) >= MaxMergeConflictPaths {
			continue
		}
		preview.Conflicts = append(preview.Conflicts, path)
	}
	if len(preview.Conflicts) == 0 {
		return MergePreview{}, fmt.Errorf("%q: %w: conflicted but no paths listed", out, ErrInvalidArgument)
	}
	return preview, nil
}

// MergeOptions carries the identity a merge commit is authored as.
// [Worktree.MergeBase] defaults to --no-ff --no-edit; FastForwardOnly
// selects --ff-only for a task branch that has no unpublished local commits.
type MergeOptions struct {
	// FastForwardOnly requires an ancestry-preserving fast-forward, retaining all merge guards.
	FastForwardOnly bool
	// Author is the identity recorded as the merge commit's author and
	// committer, exactly as [CommitOptions.Author] is for a plain commit.
	Author Author
}

func (o MergeOptions) validate() error {
	if err := checkConfigValue("author name", o.Author.Name); err != nil {
		return err
	}
	return checkConfigValue("author email", o.Author.Email)
}

// MergeResult reports the outcome of [Worktree.MergeBase].
type MergeResult struct {
	// Head is the worktree branch's HEAD once the call returns, whether or
	// not a merge commit was actually created.
	Head string
	// Merged reports whether a merge commit was created. It is false when
	// base was already an ancestor of the branch ("Already up to date."): the
	// call is not an error, there is simply nothing to merge. This mirrors
	// [CommitResult.Committed] — the caller reads the bool rather than being
	// handed a "nothing to merge" sentinel error.
	Merged bool
}

// MergeBase merges base into w's branch with "git merge --no-ff --no-edit",
// carrying the same identity prelude as [Worktree.Commit]
// ("-c user.name=... -c user.email=... -c commit.gpgsign=false"): omitting
// commit.gpgsign=false risks a hang against GIT_TERMINAL_PROMPT=0 if the
// caller's own git config asks for a signature.
//
// It never rebases and never force-pushes — either would rewrite the trailers
// [Worktree.Commit] already wrote onto commits this branch may already carry
// — so a clean merge is always a new merge commit on top of the branch, never
// a rewrite of it. Pushing the result is the caller's job, later, through the
// ordinary [Worktree.Push].
//
// MergeBase refuses a dirty worktree before touching anything: uncommitted
// changes make it unsafe to tell "the merge left this behind" apart from
// "this was already here". On any failure once the merge itself has started —
// a conflict, a per-command timeout, or any other non-zero exit — it runs
// "git merge --abort" and re-reads HEAD to confirm the branch tip is exactly
// where it was before the call; a mismatch here is reported as a failure in
// its own right rather than silently accepted, because it would mean the
// rollback did not actually restore the branch. base and the worktree's
// branch are screened by [checkName], and base is confirmed to resolve
// before any merge is attempted, so an unmerge-able ref is reported as
// [ErrRevisionNotFound] rather than being folded into [ErrMergeConflict].
//
// A per-command timeout firing during the merge itself is recovered the same
// way a conflict is (US-10, S-8): the abort runs against the same ctx the
// merge itself used, which hands it its own fresh per-command deadline rather
// than inheriting the one that just fired, so it is no more likely to hang
// than the abort after a plain conflict — the risk of the merge having left
// MERGE_HEAD or index.lock behind is exactly why the abort step exists at
// all. Once the branch tip is reverified back at its pre-merge SHA, the
// caller sees [ErrMergeTimedOut] rather than [ErrMergeConflict] so a
// disclosure can tell the two apart (AC-10.2); if the abort itself fails, or
// HEAD does not land back where it started, that is reported as its own
// failure, exactly as for a conflict.
func (w *Worktree) MergeBase(ctx context.Context, base string, opts MergeOptions) (MergeResult, error) {
	if err := w.check(); err != nil {
		return MergeResult{}, err
	}
	if err := checkName("base", base); err != nil {
		return MergeResult{}, err
	}
	if err := checkName("branch", w.branch); err != nil {
		return MergeResult{}, err
	}
	if err := opts.validate(); err != nil {
		return MergeResult{}, err
	}

	changes, err := w.Status(ctx)
	if err != nil {
		return MergeResult{}, fmt.Errorf("check worktree status before merging %s into %s: %w", base, w.branch, err)
	}
	if len(changes) > 0 {
		return MergeResult{}, fmt.Errorf("merge %s into %s: %w", base, w.branch, ErrWorktreeUncommittedChanges)
	}

	if _, err := revParse(ctx, w.runner, base, RevParseOptions{}); err != nil {
		return MergeResult{}, fmt.Errorf("resolve base %s before merging into %s: %w", base, w.branch, err)
	}

	before, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve HEAD on branch %s before merge: %w", w.branch, err)
	}

	// Status intentionally hides factory transport from ordinary task work.
	// Raw Git still inspects those paths and can refresh the index even when
	// it refuses a merge. Refuse tracked/staged transport changes read-only,
	// while allowing the usual untracked proposal/event files (AC-171-7).
	reservedArgs := append(append([]string{}, statusArgs...), "--untracked-files=no", "--", ".litefactory-plan-proposal.json", ".litefactory-plan-outcome.json", ".litefactory-story-events")
	reserved, err := w.runner.run(ctx, readTimeout, reservedArgs...)
	if err != nil {
		return MergeResult{}, fmt.Errorf("inspect reserved transport before merging %s: %w", base, err)
	}
	if reserved != "" {
		return MergeResult{}, fmt.Errorf("merge %s into %s: reserved factory transport has staged or tracked changes: %w", base, w.branch, ErrWorktreeUncommittedChanges)
	}
	// A reserved path from the base must not overwrite retained transport or
	// enter the task's synchronization commit. Identical legacy tree entries
	// are harmless; changing them requires explicit operator reconciliation.
	reserved, err = w.runner.run(ctx, readTimeout, "diff", "--name-only", "-z", before, base, "--", ".litefactory-plan-proposal.json", ".litefactory-plan-outcome.json", ".litefactory-story-events")
	if err != nil {
		return MergeResult{}, fmt.Errorf("inspect incoming reserved transport from %s: %w", base, err)
	}
	if reserved != "" {
		return MergeResult{}, fmt.Errorf("merge %s into %s: reserved factory transport differs between branch and base; retained content was preserved: %w", base, w.branch, ErrInvalidArgument)
	}

	strategy := "--no-ff"
	if opts.FastForwardOnly {
		strategy = "--ff-only"
	}
	args := []string{
		"-c", "user.name=" + opts.Author.Name,
		"-c", "user.email=" + opts.Author.Email,
		"-c", "commit.gpgsign=false",
		"merge", strategy, "--no-edit", "--end-of-options", base,
	}
	if _, err := w.runner.run(ctx, mergeTimeout, args...); err != nil {
		return w.recoverFromFailedMerge(ctx, base, before, err)
	}

	head, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return MergeResult{}, fmt.Errorf("resolve HEAD on branch %s after merge: %w", w.branch, err)
	}
	return MergeResult{Head: head, Merged: head != before}, nil
}

// recoverFromFailedMerge aborts a merge that MergeBase started and could not
// complete — whether git rejected it outright or the per-command timeout
// fired partway through — then confirms the branch tip is back at before
// (S-5). It is the rollback half of MergeBase's guarantee: whatever mergeErr
// turns out to classify as, the caller never has to wonder whether the
// branch was left half-merged, or left with MERGE_HEAD/index.lock behind
// from a merge that timed out (S-8, US-10).
//
// The abort-then-verify path is identical for a conflict and a timeout — the
// abort runs against ctx, the same context the merge itself used, which
// gives it its own fresh mergeTimeout deadline rather than inheriting the one
// that just fired, so it is no more likely to hang than an abort after a
// plain conflict. Only the sentinel reported once the branch is confirmed
// clean differs: [ErrMergeTimedOut] for a timeout, [ErrMergeConflict]
// otherwise (AC-10.2). There is no Reset method on this type and this
// recovery path is not an exception: "git merge --abort" followed by a
// re-verify is the only accepted self-heal, never "git reset" (S-5).
func (w *Worktree) recoverFromFailedMerge(ctx context.Context, base, before string, mergeErr error) (MergeResult, error) {
	var cmdErr *CommandError
	timedOut := errors.As(mergeErr, &cmdErr) && cmdErr.TimedOut

	if _, abortErr := w.runner.run(ctx, mergeTimeout, "merge", "--abort"); abortErr != nil {
		return MergeResult{}, fmt.Errorf("merge %s into %s: %w (abort also failed: %w)", base, w.branch, mergeErr, abortErr)
	}

	after, revErr := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if revErr != nil {
		return MergeResult{}, fmt.Errorf("resolve HEAD on branch %s after aborting merge: %w", w.branch, revErr)
	}
	if after != before {
		return MergeResult{}, fmt.Errorf("merge %s into %s: branch tip moved from %s to %s while aborting a failed merge (original error: %w)", base, w.branch, before, after, mergeErr)
	}

	if timedOut {
		// classify() itself refuses to reclassify an already-timed-out
		// CommandError (a timeout must never be folded into an unrelated
		// condition such as ErrRevisionNotFound), so the same substitution it
		// performs for a plain exit code is done here by hand: keep every
		// other field — Binary, Dir, Args, ExitCode, Stderr, TimedOut,
		// Timeout — and swap only Err, so the caller keeps the full
		// diagnostics alongside the named sentinel.
		classified := *cmdErr
		classified.Err = ErrMergeTimedOut
		return MergeResult{}, &classified
	}
	return MergeResult{}, classify(mergeErr, ErrMergeConflict, 1)
}

// StartMerge begins merging ref into the worktree's branch without committing
// and leaves any conflict in place for an agent to resolve. It returns the
// paths git left unmerged, sorted. A merge already in progress (MERGE_HEAD
// resolves) is not restarted: its still-unmerged paths are returned as they
// are. Otherwise the worktree must be clean. It never aborts; concluding the
// merge is left to a later [Worktree.Commit].
func (w *Worktree) StartMerge(ctx context.Context, ref string) ([]string, error) {
	if err := w.check(); err != nil {
		return nil, err
	}
	if err := checkName("base", ref); err != nil {
		return nil, err
	}
	if _, err := revParse(ctx, w.runner, "MERGE_HEAD", RevParseOptions{}); err == nil {
		unmerged, err := w.unmergedPaths(ctx)
		if err != nil {
			return nil, fmt.Errorf("read conflicted paths of the merge in progress on %s: %w", w.branch, err)
		}
		return sortedKeys(unmerged), nil
	}
	changes, err := w.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("check worktree status before merging %s into %s: %w", ref, w.branch, err)
	}
	if len(changes) > 0 {
		return nil, fmt.Errorf("merge %s into %s: %w", ref, w.branch, ErrWorktreeUncommittedChanges)
	}
	if _, _, err := w.runner.runAllowingExit(ctx, mergeTimeout, []int{0, 1},
		"merge", "--no-commit", "--no-ff", "--end-of-options", ref); err != nil {
		return nil, classify(err, ErrRevisionNotFound, 128)
	}
	if _, err := revParse(ctx, w.runner, "MERGE_HEAD", RevParseOptions{}); err != nil {
		return nil, fmt.Errorf("merge %s into %s left no merge in progress: %w", ref, w.branch, err)
	}
	unmerged, err := w.unmergedPaths(ctx)
	if err != nil {
		return nil, fmt.Errorf("read conflicted paths after merging %s into %s: %w", ref, w.branch, err)
	}
	return sortedKeys(unmerged), nil
}
