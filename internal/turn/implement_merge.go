package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reasons a story is parked. They are plain language: the owner reads them in
// the hand-off report.
const (
	implementNoCheckReason = "no build check configured"
	implementCommentPages  = 50
	implementCommentLimit  = 100
)

// implementCheckPhases are the recipe phases a merge runs, in order.
var implementCheckPhases = []string{"regression", "focused", "full"}

// ImplementMergeActivity merges the oldest done story into the integration
// branch, one story per call, while holding the parent's progress lock.
type ImplementMergeActivity struct{ b ImplementBindings }

type implementMergeOutput struct {
	Story  string `json:"story,omitempty"`
	Result string `json:"result"`
	Commit string `json:"commit,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Execute returns
//
//	merged  the story landed after a clean merge and passing checks
//	parked  a conflict, a failing check or a missing recipe; the integration
//	        branch is unchanged
//	none    no story is done
//	retry   the lock is busy or the integration branch moved; nothing changed
//
// A cancelled context, a git outage or a forge outage is an error, never a park.
func (h ImplementMergeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	a, err := h.b.bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	unlock, err := h.b.DB.LockStoryProgress(ctx, a.Parent.String(), h.b.LockDir)
	if errors.Is(err, store.ErrStoryProgressBusy) {
		return implementResult("retry", implementMergeOutput{Result: "retry", Reason: "another merge is in progress"})
	}
	if err != nil {
		return ActivityResult{}, fmt.Errorf("implement merge lock: %w", err)
	}
	defer func() { err = errors.Join(err, unlock()) }()

	rows, err := h.b.DB.ListImplementStories(ctx, a.Parent.String(), a.Generation)
	if err != nil {
		return ActivityResult{}, err
	}
	row, found := oldestDone(rows)
	if !found {
		return implementResult("none", implementMergeOutput{Result: "none"})
	}
	story, ok := a.story(row.Key)
	if !ok {
		return ActivityResult{}, fmt.Errorf("done story %q is not part of the bound story set", row.Key)
	}
	return h.mergeOne(ctx, opts, a, story, row)
}

func (h ImplementMergeActivity) mergeOne(ctx context.Context, opts Options, a ImplementAuthority, story ImplementStory, row store.ImplementStory) (res ActivityResult, err error) {
	now := h.b.now(opts)
	repo := github.RepoFromRef(a.Parent)
	finish := func(commit string, noted bool) (ActivityResult, error) {
		if !noted {
			if err := h.postMergedNote(ctx, a, story, repo, commit); err != nil {
				return ActivityResult{}, err
			}
		}
		if _, err := h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: a.Parent.String(), Generation: a.Generation, Key: story.Key,
			From: store.ImplementDone, To: store.ImplementMerged, ExpectedRevision: row.Revision, MergeCommit: commit, At: now,
		}); err != nil {
			return ActivityResult{}, err
		}
		return implementResult("merged", implementMergeOutput{Story: story.Key, Result: "merged", Commit: commit})
	}
	park := func(reason string) (ActivityResult, error) {
		if _, err := h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: a.Parent.String(), Generation: a.Generation, Key: story.Key,
			From: store.ImplementDone, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: reason, At: now,
		}); err != nil {
			return ActivityResult{}, err
		}
		return implementResult("parked", implementMergeOutput{Story: story.Key, Result: "parked", Reason: reason})
	}

	// A note that already exists means an earlier call pushed the merge and was
	// interrupted before the ledger moved; finish that call's work.
	prior, found, err := h.findMergedNote(ctx, a, story, repo)
	if err != nil {
		return ActivityResult{}, err
	}
	if found {
		return finish(prior, true)
	}

	scratch, err := h.b.Repo.Begin(ctx, a.IntegrationBranch)
	if err != nil {
		return ActivityResult{}, fmt.Errorf("prepare merge: %w", err)
	}
	defer func() {
		if closeErr := scratch.Close(context.WithoutCancel(ctx)); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("discard scratch worktree: %w", closeErr))
		}
	}()

	recipeRaw, err := scratch.Recipe(ctx)
	if errors.Is(err, git.ErrBugCheckRecipe) {
		return park(implementNoCheckReason)
	}
	if err != nil {
		return ActivityResult{}, fmt.Errorf("read build check recipe: %w", err)
	}
	recipe, err := config.ParseBugCheckRecipe(recipeRaw)
	if err != nil {
		return park("the build check recipe is invalid: " + clipText(err.Error(), 300))
	}

	head, merged, err := scratch.Merge(ctx, story.Branch)
	switch {
	case errors.Is(err, git.ErrMergeConflict):
		return park("the story conflicts with the work already merged; it could not be merged cleanly")
	case errors.Is(err, git.ErrRevisionNotFound):
		return park("the story's branch was not found, so there is nothing to merge")
	case err != nil:
		return ActivityResult{}, fmt.Errorf("merge story %s: %w", story.Key, err)
	}
	if !merged {
		// Already part of the integration branch: nothing to check or push.
		return finish(scratch.Head(), false)
	}

	failure, err := h.runChecks(ctx, opts, recipe, scratch.Path())
	if err != nil {
		return ActivityResult{}, err
	}
	if failure != "" {
		return park(failure)
	}

	err = scratch.Push(ctx, a.IntegrationBranch, scratch.Head())
	if errors.Is(err, git.ErrIntegrationHeadMoved) {
		return implementResult("retry", implementMergeOutput{Story: story.Key, Result: "retry", Reason: "the integration branch moved while the story was being checked"})
	}
	if err != nil {
		return ActivityResult{}, fmt.Errorf("publish merge of %s: %w", story.Key, err)
	}
	return finish(head, false)
}

// runChecks runs every phase's commands in order. It returns a plain reason for
// the first failing check, or an error when a check could not be run at all.
func (h ImplementMergeActivity) runChecks(ctx context.Context, opts Options, recipe config.BugCheckRecipe, worktree string) (string, error) {
	run := h.b.RunCheck
	if run == nil {
		if opts.MCP == nil {
			return "", errors.New("the check sandbox is not configured")
		}
		isolation := opts.MCP.BugChecksIsolation
		if err := isolation.Validate(); err != nil {
			return "", fmt.Errorf("the check sandbox is unavailable: %w", err)
		}
		run = func(ctx context.Context, worktree string, command config.BugCheckCommand) bugchecks.CommandResult {
			return isolation.Run(ctx, worktree, command, func(bugchecks.CommandResult) error { return nil })
		}
	}
	for _, name := range implementCheckPhases {
		phase, err := recipe.Phase(name)
		if err != nil {
			return "", fmt.Errorf("recipe phase %q: %w", name, err)
		}
		for _, command := range phase.Commands {
			result := run(ctx, worktree, command)
			switch result.Outcome {
			case "passed":
			case "failed":
				return fmt.Sprintf("the %s build check failed: %s", name, describeImplementCommand(command, result)), nil
			case "cancelled":
				return "", fmt.Errorf("build check cancelled: %w", errors.Join(ctx.Err(), errors.New(result.Reason)))
			default:
				return "", fmt.Errorf("a %s build check could not run: %s", name, result.Reason)
			}
		}
	}
	return "", nil
}

func describeImplementCommand(command config.BugCheckCommand, result bugchecks.CommandResult) string {
	text := "`" + clipText(strings.Join(command.Argv, " "), 120) + "`"
	if result.Exit != nil {
		text += fmt.Sprintf(" exited with code %d", *result.Exit)
	}
	return text
}

// findMergedNote looks for an existing story-merged note for this story and
// returns the commit it names.
func (h ImplementMergeActivity) findMergedNote(ctx context.Context, a ImplementAuthority, story ImplementStory, repo github.Repo) (string, bool, error) {
	if story.IssueNumber < 1 {
		return "", false, fmt.Errorf("story %q has no issue to carry its merge note", story.Key)
	}
	for page := 1; page > 0 && page <= implementCommentPages; {
		got, err := h.b.Forge.ListCommentsPage(ctx, repo, story.IssueNumber, page, implementCommentLimit)
		if err != nil {
			return "", false, fmt.Errorf("look for merge note: %w", err)
		}
		for _, c := range got.Comments {
			if commit, ok := report.ParseStoryMergedMarker(c.Body, a.Parent, story.Key); ok {
				return commit, true, nil
			}
		}
		page = got.NextPage
	}
	return "", false, nil
}

// postMergedNote posts the visible note on the story.
func (h ImplementMergeActivity) postMergedNote(ctx context.Context, a ImplementAuthority, story ImplementStory, repo github.Repo, commit string) error {
	text := report.StoryMergedMarker(a.Parent, story.Key, commit) + "\n\n" +
		fmt.Sprintf("This story was merged into `%s` as commit `%s`.", a.IntegrationBranch, commit)
	body, err := h.b.Redactor.Apply(ctx, text, github.CommentSizeLimit)
	if err != nil {
		return fmt.Errorf("render merge note: %w", err)
	}
	if body.Truncated() {
		return errors.New("merge note was truncated")
	}
	if _, err := h.b.Forge.PostComment(ctx, repo, story.IssueNumber, body); err != nil {
		return fmt.Errorf("post merge note: %w", err)
	}
	return nil
}
