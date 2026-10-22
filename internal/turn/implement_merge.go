package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const (
	implementCommentPages = 50
	implementCommentLimit = 100
)

// ImplementMergeActivity merges the oldest done story into the integration
// branch, one story per call, while holding the parent's progress lock. It runs
// no tests: the story worker tests its story against the integration branch
// before it reports done, and the full suite belongs to Validate.
type ImplementMergeActivity struct{ b ImplementBindings }

type implementMergeOutput struct {
	Story  string `json:"story,omitempty"`
	Result string `json:"result"`
	Commit string `json:"commit,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Execute returns
//
//	merged  the story landed after a clean merge
//	parked  a conflict or a missing story branch; the integration branch is
//	        unchanged. A content conflict moves the story back to waiting so
//	        its worker resolves it; the others wait for the owner
//	none    no story is done
//	retry   the lock is busy, the integration branch moved, or discovery was
//	        deferred or rate limited; nothing changed
//
// A cancelled context, a git outage or a forge outage is an error, never a park.
func (h ImplementMergeActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	res, err = h.execute(ctx, opts, req)
	if reason, ok := implementMergeTransient(err); ok {
		return implementResult("retry", implementMergeOutput{Result: "retry", Reason: reason})
	}
	return res, err
}

// implementMergeTransient reports whether err is a deferral the merge step
// waits out: local discovery-budget pressure or a forge rate limit.
func implementMergeTransient(err error) (string, bool) {
	switch {
	case errors.Is(err, ticket.ErrDiscoveryDeferred):
		return "repository discovery was deferred; the merge is tried again shortly", true
	case errors.Is(err, github.ErrRateLimited):
		return "the forge is rate limiting requests; the merge is tried again shortly", true
	}
	return "", false
}

func (h ImplementMergeActivity) execute(ctx context.Context, opts Options, req ActivityRequest) (res ActivityResult, err error) {
	a, err := h.b.bind(ctx, opts, req)
	if err != nil {
		return ActivityResult{}, err
	}
	defer bestEffortImplementStatus(ctx, opts, h.b.Status, a.Parent.String(), a.Generation)
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
	if !found && slices.ContainsFunc(rows, func(r store.ImplementStory) bool { return r.State == store.ImplementMerged }) {
		_, stale, err := inspectImplementMerged(ctx, h.b.Repo, a.IntegrationBranch, rows)
		if err != nil {
			return ActivityResult{}, err
		}
		if len(stale) > 0 {
			row = stale[0]
			row, err = h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
				Parent: row.Parent, Generation: row.Generation, Key: row.Key,
				From: store.ImplementMerged, To: store.ImplementDone, ExpectedRevision: row.Revision, At: h.b.now(opts),
			})
			if err != nil {
				return ActivityResult{}, err
			}
			found = true
		}
	}
	if !found {
		return implementResult("none", implementMergeOutput{Result: "none"})
	}
	story, ok := a.story(row.Key)
	if !ok {
		revisions, err := h.b.DB.ListApprovedStoryRevisions(ctx, a.Parent.String(), int(a.Generation))
		if err != nil {
			return ActivityResult{}, err
		}
		for _, revision := range revisions {
			if revision.Key == row.Key {
				story = ImplementStory{Key: row.Key, Name: revision.Title, IssueNumber: revision.IssueNumber}
				ok = true
				break
			}
		}
		if !ok {
			return ActivityResult{}, fmt.Errorf("done story %q is not part of the approved story set", row.Key)
		}
	}
	story.Branch = row.Branch
	return h.mergeOne(ctx, opts, a, story, row)
}

func (h ImplementMergeActivity) mergeOne(ctx context.Context, opts Options, a ImplementAuthority, story ImplementStory, row store.ImplementStory) (res ActivityResult, err error) {
	now := h.b.now(opts)
	repo := github.RepoFromRef(a.Parent)
	finish := func(commit string) (ActivityResult, error) {
		// Git has established the result; historical notes only suppress an
		// identical note, never establish that the current story was merged.
		noted, err := h.findMergedNote(ctx, a, story, repo, commit)
		if err != nil {
			return ActivityResult{}, err
		}
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
	parkRow := func(reason string) (store.ImplementStory, error) {
		return h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: a.Parent.String(), Generation: a.Generation, Key: story.Key,
			From: store.ImplementDone, To: store.ImplementParked, ExpectedRevision: row.Revision, Reason: reason, At: now,
		})
	}
	park := func(reason string) (ActivityResult, error) {
		if _, err := parkRow(reason); err != nil {
			return ActivityResult{}, err
		}
		return implementResult("parked", implementMergeOutput{Story: story.Key, Result: "parked", Reason: reason})
	}
	// A content conflict is the story worker's to resolve: the story is parked
	// and at once moved back to waiting, so ready starts its worker again, which
	// merges the feature branch into the story branch and resolves the conflict.
	restart := func(reason string) (ActivityResult, error) {
		parked, err := parkRow(reason)
		if err != nil {
			return ActivityResult{}, err
		}
		if _, err := h.b.DB.TransitionImplementStory(ctx, store.ImplementTransition{
			Parent: a.Parent.String(), Generation: a.Generation, Key: story.Key,
			From: store.ImplementParked, To: store.ImplementWaiting, ExpectedRevision: parked.Revision, At: now,
		}); err != nil {
			return ActivityResult{}, err
		}
		return implementResult("parked", implementMergeOutput{Story: story.Key, Result: "parked", Reason: reason})
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

	head, merged, err := scratch.Merge(ctx, story.Branch)
	switch {
	case errors.Is(err, git.ErrMergeConflict):
		return restart("the story conflicts with the work already merged; its worker is started again to resolve the conflict")
	case errors.Is(err, git.ErrRevisionNotFound):
		return park("the story's branch was not found, so there is nothing to merge")
	case err != nil:
		return ActivityResult{}, fmt.Errorf("merge story %s: %w", story.Key, err)
	}
	if !merged {
		// Already part of the integration branch: nothing to check or push.
		return finish(scratch.Head())
	}

	const migrationPath = "internal/store/migrations"
	_, inspectErr := os.Lstat(filepath.Join(scratch.Path(), migrationPath))
	if inspectErr == nil {
		inspectErr = store.ValidateMigrationFS(os.DirFS(scratch.Path()), migrationPath)
	} else if errors.Is(inspectErr, os.ErrNotExist) {
		// Repositories without the store migration directory need no check.
		inspectErr = nil
	}
	if errors.Is(inspectErr, store.ErrDuplicateMigrationVersion) {
		return park("migration conflict: " + inspectErr.Error() + "; rename only this story's new, unapplied migration and recheck against current integration")
	}
	if inspectErr != nil {
		return ActivityResult{}, fmt.Errorf("inspect migrations for story %s: %w", story.Key, inspectErr)
	}

	err = scratch.Publish(ctx, a.IntegrationBranch, scratch.Head())
	if errors.Is(err, git.ErrIntegrationHeadMoved) {
		return implementResult("retry", implementMergeOutput{Story: story.Key, Result: "retry", Reason: "the integration branch moved while the story was being checked"})
	}
	if err != nil {
		return ActivityResult{}, fmt.Errorf("publish merge of %s: %w", story.Key, err)
	}
	return finish(head)
}

// findMergedNote looks for a note naming the result Git established for this story.
func (h ImplementMergeActivity) findMergedNote(ctx context.Context, a ImplementAuthority, story ImplementStory, repo github.Repo, resultCommit string) (bool, error) {
	if story.IssueNumber < 1 {
		return false, fmt.Errorf("story %q has no issue to carry its merge note", story.Key)
	}
	for page := 1; page > 0 && page <= implementCommentPages; {
		got, err := h.b.Forge.ListCommentsPage(ctx, repo, story.IssueNumber, page, implementCommentLimit)
		if err != nil {
			return false, fmt.Errorf("look for merge note: %w", err)
		}
		for _, c := range got.Comments {
			if commit, ok := report.ParseStoryMergedMarker(c.Body, a.Parent, story.Key); ok && commit == resultCommit {
				return true, nil
			}
		}
		page = got.NextPage
	}
	return false, nil
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
	if _, err := postTicketNotice(ctx, h.b.Forge, report.StoryMergedMarker(a.Parent, story.Key, commit), a.Parent.String(), 0, repo, story.IssueNumber, body); err != nil {
		return fmt.Errorf("post merge note: %w", err)
	}
	return nil
}
