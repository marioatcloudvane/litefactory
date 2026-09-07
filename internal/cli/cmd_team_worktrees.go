package cli

// This file is US-15's whole addition to `team update`: live discovery of a
// repository's other worktrees, and the --all-worktrees orchestration that
// applies teamUpdateWorktree (internal/cli/cmd_team.go, US-11) to each one
// independently. Nothing here persists a repo-to-worktree relationship —
// discoverSiblingWorktrees asks git itself, every time, via internal/git's
// existing Clone.WorktreeList, and treats its answer as the sole ground
// truth for which paths are real, current worktrees of this repository.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/git"
)

// discoverSiblingWorktrees returns every other worktree git confirms belongs
// to the same repository as repoPath (T-41), sorted for deterministic
// output.
//
// "git worktree list" is the only git verb this whole feature is allowed to
// invoke, and it is already the complete, authoritative answer: it reports
// every worktree linked to the repository — the main one and every linked
// one alike — regardless of how or when it was created, purely from git's
// own on-disk registry under .git/worktrees. That is why no other signal
// (recorded session history, a collision-set from repo registration) is
// consulted here to expand the result: nothing outside git's own registry
// can name a worktree this call would otherwise miss, so treating such a
// signal as a candidate to filter through this call would only ever narrow
// what AC-1 requires — every worktree git confirms — never legitimately
// widen it.
//
// Two categories of entry are excluded, silently rather than as an error
// (AC-2): a bare entry, which is the repository's own bare backing store and
// never a checkout `team update` can act on, and a prunable one, which
// git's own registry still names but whose directory is gone from disk —
// exactly the stale/removed candidate case this story must exclude without
// reporting a failure.
func discoverSiblingWorktrees(ctx context.Context, repoPath string) ([]string, error) {
	binary, err := git.Resolve(ctx)
	if err != nil {
		return nil, Precondition(err)
	}
	clone, err := binary.Open(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	list, err := clone.WorktreeList(ctx)
	if err != nil {
		return nil, err
	}

	self := clone.Path()

	var siblings []string
	for _, wt := range list {
		if wt.Bare || wt.Prunable {
			continue
		}
		abs, err := filepath.Abs(wt.Path)
		if err != nil {
			// A path git itself printed that this process cannot even make
			// absolute is not a worktree this run can safely act on; exclude
			// it rather than fail the whole discovery over one entry.
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if abs == self {
			continue
		}
		siblings = append(siblings, abs)
	}
	sort.Strings(siblings)
	return siblings, nil
}

// teamUpdateWorktreeJSON is one worktree's outcome inside `team update
// --all-worktrees`'s per-worktree report (AC-4): which worktree, and what
// [teamUpdateWorktree]'s US-11 algorithm did there, independent of every
// other worktree in the run.
type teamUpdateWorktreeJSON struct {
	Path    string                 `json:"path"`
	Updated int                    `json:"updated"`
	Skipped int                    `json:"skipped"`
	Refused int                    `json:"refused"`
	Records []teamUpdateRecordJSON `json:"records,omitempty"`
	// Error explains why this worktree did not fully succeed: a lockfile or
	// team-index resolution failure (Records is empty in that case — nothing
	// was classified), a refused record, or a record-level classify/render/
	// write error. Empty when everything for this worktree went cleanly.
	Error string `json:"error,omitempty"`
}

// teamUpdateAllJSON is `team update --all-worktrees --json`'s top-level
// shape: the worktree the command was invoked against plus every
// git-confirmed sibling (T-41/AC-1), each classified and updated
// independently — one worktree's failure never removes another's entry from
// this list (AC-4).
type teamUpdateAllJSON struct {
	Repo      string                   `json:"repo"`
	Force     bool                     `json:"force"`
	Worktrees []teamUpdateWorktreeJSON `json:"worktrees"`
}

// runTeamUpdateAllWorktrees is `team update --all-worktrees`'s
// orchestration: discover every worktree git confirms belongs to repoPath's
// repository, then run [teamUpdateWorktree] against repoPath and each
// sibling independently, in the same process, aggregating one report.
//
// A discovery failure (git missing, repoPath not a real repository) aborts
// before anything is touched — there is no partial worktree list to act on.
// Once discovery succeeds, every worktree's own [teamUpdateWorktree] call is
// isolated: its error, if any, is recorded against that worktree's own
// report entry and folded into the run's combined error via errors.Join,
// but it never stops the loop from reaching the remaining worktrees (AC-4).
func runTeamUpdateAllWorktrees(ctx context.Context, env *Env, rt *teamRuntime, repoPath string, force, asJSON bool) error {
	siblings, err := discoverSiblingWorktrees(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("discover sibling worktrees of %s: %w", repoPath, err)
	}

	paths := make([]string, 0, len(siblings)+1)
	paths = append(paths, repoPath)
	paths = append(paths, siblings...)

	worktrees := make([]teamUpdateWorktreeJSON, 0, len(paths))
	var errs []error
	for _, path := range paths {
		out, updateErr := teamUpdateWorktree(ctx, rt, path, force)
		entry := teamUpdateWorktreeJSON{Path: path}
		if out != nil {
			entry.Updated = out.Updated
			entry.Skipped = out.Skipped
			entry.Refused = out.Refused
			entry.Records = out.Records
		}
		if updateErr != nil {
			entry.Error = updateErr.Error()
			errs = append(errs, fmt.Errorf("worktree %s: %w", path, updateErr))
		}
		worktrees = append(worktrees, entry)
	}

	report := teamUpdateAllJSON{
		Repo:      repoPath,
		Force:     force,
		Worktrees: worktrees,
	}

	if asJSON {
		if err := writeTeamUpdateAllJSON(env.Stdout, report); err != nil {
			return err
		}
	} else {
		writeTeamUpdateAllTable(env.Stdout, report)
	}

	return errors.Join(errs...)
}

// writeTeamUpdateAllJSON encodes v to out as indented JSON.
func writeTeamUpdateAllJSON(out io.Writer, v teamUpdateAllJSON) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode team update json: %w", err)
	}
	return nil
}

// writeTeamUpdateAllTable prints v as the human-readable form of `team
// update --all-worktrees`: one section per worktree, each naming its own
// records exactly as the single-worktree table does, so a reader can see at
// a glance which worktree owns which outcome (AC-4).
func writeTeamUpdateAllTable(out io.Writer, v teamUpdateAllJSON) {
	fmt.Fprintf(out, "repo: %s (--all-worktrees)\n\n", v.Repo)

	for _, wt := range v.Worktrees {
		fmt.Fprintf(out, "worktree: %s\n", wt.Path)

		if len(wt.Records) == 0 {
			if wt.Error != "" {
				fmt.Fprintf(out, "  error: %s\n\n", wt.Error)
			} else {
				fmt.Fprintln(out, "  no files tracked by this worktree's team lockfile")
				fmt.Fprintln(out)
			}
			continue
		}

		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "  ACTION\tSTATUS\tAGENT\tHARNESS\tDEST\tNOTE\n")
		for _, r := range wt.Records {
			note := r.Note
			if note == "" {
				note = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n", r.Action, r.Status, r.AgentKey, r.Harness, r.DestPath, note)
		}
		// A failed write of already-reported output is not actionable.
		_ = tw.Flush()
		fmt.Fprintln(out)
	}
}
