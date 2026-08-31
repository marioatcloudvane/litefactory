package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/report"
)

// The sub-issue phase closes the gap CR-B-001 named: Story C1 built
// github.CreateSubIssues and report.RenderSubIssue, but shipped with no
// caller of either outside their own package's tests, so a real Plan run
// never got a working sub-issue link for a single Story Index row.
//
// This file is that caller. It runs once per turn's harvest, after [save]'s
// commit and push have both succeeded and before [finish] runs — see
// [fileStoryIssues] for why that ordering is not negotiable and
// [runAndHarvest] for where it is wired in. It is not gated on this turn
// having been a Plan run: there is no reliable way to know that (see the
// architecture note this file was built from,
// .aiteam/lf-7-headless-mode-should-ask-via-a-comment/2026-08-26/
// headless-mode-should-ask-via-a-comment/ARCHITECT_SUBISSUE_WIRING.md, Q3),
// so the phase's own "is there a plan_brief.md with a Story Index to read"
// check is its mode check. Every other turn pays for one failed [os.Stat] and
// moves on.

// SubIssueCreator files a plan's sub-issues against a parent issue.
// *github.Client satisfies it.
//
// It is declared here, at the point of consumption — the same reason
// [Poster] and [Renderer] are declared in report.go rather than in
// internal/github or internal/report. Narrowing it to the one method this
// phase calls is also what keeps internal/github free of an interface of its
// own: TestNoForgeAbstraction (internal/github/surface_test.go) holds that
// package to zero, and this phase adds none there.
type SubIssueCreator interface {
	CreateSubIssues(ctx context.Context, repo github.Repo, parent int, specs []github.SubIssueSpec) ([]github.SubIssueResult, error)
}

// SubIssueRenderer renders one user story into a sub-issue body.
// *report.Reporter satisfies it.
type SubIssueRenderer interface {
	RenderSubIssue(ctx context.Context, story report.Story) (report.SubIssue, error)
}

// subIssueCommitSummary is commit2's message: litefactory's own, not
// claude's, so it needs no pass through the summary sanitiser [save] gives
// claude's text (Q2 of the architecture note — this run's report describes
// only what claude did, and this commit is deliberately not folded into it).
const subIssueCommitSummary = "litefactory: record this plan's sub-issue links in the Story Index"

// fileStoryIssues locates this task's plan_brief.md, if there is one waiting
// with a Story Index to resolve, files the missing sub-issues and rewrites
// the Story Index with a working link for each one it filed or already found.
//
// # Ordering — why this runs where it runs
//
// Strictly after [save]'s commit and push have both succeeded, never before.
// A sub-issue's technical-detail link is only good when it is pinned to a
// commit GitHub can actually serve (report.Story.DetailSHA's own doc comment:
// "a permalink into a commit that exists only in a local worktree is a
// guaranteed 404"), and the per-story file that link points at was written by
// claude in this same run — so it is not a real, pushable commit until save's
// commit1/push1 land. This is a second, independent commit and push, not
// folded into commit1: commit1 is claude's and this run's own report reads
// only commit1's fields, so a litefactory-authored edit folded into it would
// make the report claim something claude never did.
//
// # What this must never touch
//
// result.Changes, result.SHAAfter, result.CommitSHA, result.Committed,
// result.Pushed, result.Err, result.Outcome and result.Reason all stay
// commit1's. [finish] reads result.CommitSHA to record the run's own commit;
// recording commit2's SHA there instead would be recording the wrong commit
// against this run. This function reads result but returns it unchanged on
// every path — there is nothing yet worth surfacing on it (see the
// architecture note's Deferred section on a persisted signal for this
// phase's own outcome).
//
// # Failure containment
//
// Every failure here is a log line and an early return, never a mutation of
// result and never a returned error: a run that already succeeded, and was
// already about to be reported as having succeeded, must not be turned into
// a failure by something that happens after its own work is done. A batch
// that stops partway, a write-back that cannot be committed, a push that
// cannot reach the remote — all of them leave real, already-filed GitHub
// issues in place and an unresolved Story Index row for the next turn on this
// task to pick up, whatever that next turn turns out to be.
func fileStoryIssues(
	ctx context.Context,
	opts Options,
	prep *preparation,
	result Result,
	runResult *harness.RunResult,
	logger *slog.Logger,
) Result {
	if opts.SubIssueCreator == nil || opts.SubIssueRenderer == nil {
		return result
	}
	if runResult == nil || runResult.Outcome == harness.OutcomeAbandoned {
		// Mirrors save's own guard (harvest.go): the daemon may not have had
		// time to commit the worktree at all, and there is nothing here to
		// build on top of.
		return result
	}
	if result.Err != nil {
		// [save] failed, and every way it can fail leaves this phase with
		// nothing it may safely build on. The two are worth naming apart.
		//
		// A push failure leaves HEAD unconfirmed on the remote, so a Detail
		// link pinned to it is exactly the guaranteed-404 permalink
		// report.Story.DetailSHA's doc comment warns against.
		//
		// A commit failure is worse and does not set Changed, which is why
		// testing Changed here was not enough: the per-story files are then
		// not in git at all, and this phase would file real sub-issues from
		// content no commit holds. Its own write-back commit stages with
		// git add --all, so it would also sweep claude's still-uncommitted
		// work into a commit no runs row names.
		//
		// Either way the work is not lost. The next turn to reach this
		// task's worktree — a retry, or an unrelated later turn — carries it
		// forward, and this phase runs again then.
		return result
	}

	log := logger.With(slog.String("phase", "sub_issues"))

	dir, ok := locateSessionDir(prep, log)
	if !ok {
		return result
	}

	planPath := filepath.Join(prep.worktree.Path(), filepath.FromSlash(dir), planbrief.FileName)
	data, err := os.ReadFile(planPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Error("read plan_brief.md", slog.String("path", planPath), slog.Any("error", err))
		}
		return result
	}

	rows, err := planbrief.ParseStoryIndex(data)
	if err != nil {
		log.Error("parse plan_brief.md's Story Index", slog.String("path", planPath), slog.Any("error", err))
		return result
	}
	if len(rows) == 0 {
		log.Debug("plan_brief.md has an empty Story Index; nothing to file",
			slog.String("path", planPath))
		return result
	}

	specs := buildSubIssueSpecs(ctx, opts.SubIssueRenderer, prep, dir, result.SHAAfter, rows, log)
	if len(specs) == 0 {
		log.Warn("no story in plan_brief.md had a usable sub-issue body; nothing to file",
			slog.String("path", planPath), slog.Int("stories", len(rows)))
		return result
	}

	results, err := opts.SubIssueCreator.CreateSubIssues(ctx, github.RepoFromRef(prep.ref), prep.ref.Issue, specs)
	if err != nil {
		log.Error("file the plan's sub-issues", slog.Any("error", err))
		return result
	}

	rewritten, changed := rewriteStoryIndex(data, results, log)
	if !changed {
		log.Debug("every filed story already carried its link; nothing to write back")
		return result
	}

	if err := os.WriteFile(planPath, rewritten, planbriefFileMode); err != nil {
		log.Error("write plan_brief.md", slog.String("path", planPath), slog.Any("error", err))
		return result
	}

	commit, err := prep.worktree.Commit(ctx, git.CommitOptions{
		Summary: subIssueCommitSummary,
		Task:    prep.ref.String(),
		Mode:    prep.run.Mode,
		Run:     prep.run.Ordinal,
		Author:  prep.author,
	})
	if err != nil {
		log.Error("commit the Story Index rewrite",
			slog.String("worktree", prep.worktree.Path()), slog.Any("error", err))
		return result
	}
	if !commit.Committed {
		// Worktree.Commit stages everything and finds no diff — a retry that
		// found nothing left to do, the same AC-3.23 shape [save] itself
		// relies on. Nothing to push.
		return result
	}

	if err := prep.worktree.Push(ctx, opts.remote()); err != nil {
		log.Error("push the Story Index rewrite",
			slog.String("branch", prep.session.Branch),
			slog.String("worktree", prep.worktree.Path()),
			slog.Any("error", err))
		return result
	}

	log.Info("filed the plan's sub-issues",
		slog.Int("stories", len(specs)),
		slog.String("commit", commit.SHA))
	return result
}

// planbriefFileMode is the permission plan_brief.md is rewritten with — the
// same permissive-but-not-executable mode `git add` itself would leave a
// text file at, so a rewrite by litefactory cannot narrow what claude's own
// writes to the file left it at.
const planbriefFileMode = 0o644

// locateSessionDir finds this task's aiteam session directory: the
// repository-relative directory [naming.AITeamSessionDir] would have named
// when this task's branch was created (Q1 of the architecture note).
//
// It is derived, not discovered — the same principle [report.Run.briefLink]
// already follows for linking RESEARCH_BRIEF.md. The direct guess is the
// current run's own start day, which is exact and free for the ordinary case:
// the Plan run that just wrote plan_brief.md is, by construction, running on
// the same calendar day the session directory was named. Only when that
// guess misses — a retry days later, after an earlier partial failure — does
// it fall back to one bounded, one-level [os.ReadDir] of
// .aiteam/<branchDir>/, checking each date directory's plan_brief.md, newest
// first.
func locateSessionDir(prep *preparation, log *slog.Logger) (string, bool) {
	branch := prep.session.Branch
	slug, err := naming.SlugFromBranch(branch, prep.ref.Issue)
	if err != nil {
		log.Debug("branch was not built by this task's own tooling; nothing to file",
			slog.String("branch", branch), slog.Any("error", err))
		return "", false
	}

	if dir, ok := trySessionDay(prep, branch, slug, prep.run.StartedAt); ok {
		return dir, true
	}
	return fallbackSessionDir(prep, branch, slug, log)
}

// trySessionDay derives the session directory for one candidate day and
// reports whether plan_brief.md actually exists there — the "exact and free"
// half of [locateSessionDir]'s derivation, one [os.Stat] and nothing more.
func trySessionDay(prep *preparation, branch, slug string, day time.Time) (string, bool) {
	dir, err := naming.AITeamSessionDir(branch, day, slug)
	if err != nil {
		return "", false
	}
	planPath := filepath.Join(prep.worktree.Path(), filepath.FromSlash(dir), planbrief.FileName)
	if _, err := os.Stat(planPath); err != nil {
		return "", false
	}
	return dir, true
}

// fallbackSessionDir is the bounded, one-level scan [locateSessionDir] falls
// back to when the run's own start day is not where the session was named —
// checking every date directory under .aiteam/<branchDir>/ in turn, newest
// first, for one that actually holds plan_brief.md.
//
// It reads exactly one directory, never walks, and stops at the first date
// that matches — the same discipline internal/repos.DetectAiteam already
// holds its own directory scan to.
func fallbackSessionDir(prep *preparation, branch, slug string, log *slog.Logger) (string, bool) {
	branchDir, err := naming.AITeamBranchDir(branch)
	if err != nil {
		return "", false
	}

	root := filepath.Join(prep.worktree.Path(), filepath.FromSlash(branchDir))
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Debug("list the aiteam branch directory", slog.String("path", root), slog.Any("error", err))
		}
		return "", false
	}

	dates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dates = append(dates, entry.Name())
		}
	}
	// Lexicographic order is chronological order for YYYY-MM-DD, so this is
	// "newest first" with no date parsing needed to sort.
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	for _, date := range dates {
		day, err := time.ParseInLocation(naming.SessionDateLayout, date, time.Local)
		if err != nil {
			continue
		}
		if dir, ok := trySessionDay(prep, branch, slug, day); ok {
			return dir, true
		}
	}
	return "", false
}

// buildSubIssueSpecs turns every Story Index row into a [github.SubIssueSpec],
// skipping and logging any story whose per-story file is missing or will not
// parse into a story rather than letting one broken story block its siblings
// (the architecture note's step 4: checkSubIssueSpecs refuses the whole batch
// on the first bad spec, so nothing unrenderable may reach it).
//
// Every row is attempted regardless of what its own Sub-issue cell currently
// says — GitHub's own idempotency check inside CreateSubIssues is the
// authoritative answer to "does this story already have one", not the file.
func buildSubIssueSpecs(
	ctx context.Context,
	renderer SubIssueRenderer,
	prep *preparation,
	sessionDir, detailSHA string,
	rows []planbrief.StoryRow,
	log *slog.Logger,
) []github.SubIssueSpec {
	specs := make([]github.SubIssueSpec, 0, len(rows))
	for _, row := range rows {
		spec, err := buildSubIssueSpec(ctx, renderer, prep, sessionDir, detailSHA, row)
		if err != nil {
			log.Warn("skip a story with no usable sub-issue body",
				slog.String("story", row.ID), slog.Any("error", err))
			continue
		}
		specs = append(specs, spec)
	}
	return specs
}

// buildSubIssueSpec reads one story's per-story file, renders it and returns
// the spec [SubIssueCreator.CreateSubIssues] needs to file it.
func buildSubIssueSpec(
	ctx context.Context,
	renderer SubIssueRenderer,
	prep *preparation,
	sessionDir, detailSHA string,
	row planbrief.StoryRow,
) (github.SubIssueSpec, error) {
	// repoRelativePath is a git path (forward slashes); storyPath is where
	// that same path lives on this machine's filesystem.
	repoRelativePath := path.Join(sessionDir, row.ID+".md")
	storyPath := filepath.Join(prep.worktree.Path(), filepath.FromSlash(repoRelativePath))

	data, err := os.ReadFile(storyPath)
	if err != nil {
		return github.SubIssueSpec{}, fmt.Errorf("read the story file: %w", err)
	}
	content, err := planbrief.ParseStoryFile(data)
	if err != nil {
		return github.SubIssueSpec{}, fmt.Errorf("parse the story file: %w", err)
	}

	title := row.Title
	if title == "" {
		title = content.Title
	}

	story := report.Story{
		Ref:       prep.ref,
		ID:        row.ID,
		Title:     title,
		Summary:   content.Summary,
		Detail:    repoRelativePath,
		DetailSHA: detailSHA,
		Excerpt:   content.Excerpt,
	}

	rendered, err := renderer.RenderSubIssue(ctx, story)
	if err != nil {
		return github.SubIssueSpec{}, fmt.Errorf("render the sub-issue: %w", err)
	}

	return github.SubIssueSpec{
		StoryID: row.ID,
		Title:   rendered.Title,
		Body:    rendered.Body,
		Marker:  rendered.Marker,
	}, nil
}

// rewriteStoryIndex applies every result that named a real, reachable issue
// to data's Story Index, and reports whether anything actually changed.
//
// It never fails the whole rewrite over one story: [planbrief.RewriteSubIssueCell]
// failing for one story id (a row that has since vanished from the file, for
// instance) is logged and the loop moves on to the next result, on the same
// reasoning [buildSubIssueSpecs] skips a story it cannot build a spec for —
// one row's problem is not its siblings'.
func rewriteStoryIndex(data []byte, results []github.SubIssueResult, log *slog.Logger) ([]byte, bool) {
	rewritten := data
	changed := false
	for _, res := range results {
		url, ok := subIssueURL(res)
		if !ok {
			continue
		}
		next, did, err := planbrief.RewriteSubIssueCell(rewritten, res.StoryID, url)
		if err != nil {
			log.Error("rewrite the Sub-issue cell",
				slog.String("story", res.StoryID), slog.Any("error", err))
			continue
		}
		rewritten = next
		changed = changed || did
	}
	return rewritten, changed
}

// subIssueURL says which of a batch result's outcomes carries a link worth
// writing into the Story Index, and what that link is.
//
// [github.SubIssueCreated] and [github.SubIssueExisted] both name a real,
// attached sub-issue. [github.SubIssueUnattached] does too — the issue
// genuinely exists and is reachable at HTMLURL, it is simply not yet nested
// under the parent — and the architecture note is explicit that this is
// written back deliberately: it is what stops a duplicate orphan being
// created on the next retry, at the cost of the row not formally reading as
// a nested sub-issue until a human reattaches it by hand (a named, deferred
// gap — see the note's Deferred section). [github.SubIssueFailed] and
// [github.SubIssueNotAttempted] carry nothing that was actually created, so
// the row is left exactly as it was for the next attempt to find again.
func subIssueURL(res github.SubIssueResult) (string, bool) {
	switch res.Outcome {
	case github.SubIssueCreated, github.SubIssueExisted, github.SubIssueUnattached:
		return res.HTMLURL, res.HTMLURL != ""
	default:
		return "", false
	}
}
