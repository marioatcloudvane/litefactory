package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// planShellBodyLimit bounds the intent text copied into a story's drafting body.
const planShellBodyLimit = 4000

// planStoryShell renders the short body a story issue has until its first draft:
// its name, its intent and a line saying the first draft is in progress. It
// carries the story marker, which binds the issue to its story, and nothing the
// draft later adds.
func planStoryShell(a PlanStoryAuthority, parentNumber int, ordinal int64, key string, entry planbrief.PlanStorySetEntry) (title, body string) {
	name := strings.Join(strings.Fields(entry.Name), " ")
	title = fmt.Sprintf("%d-%03d: %s", parentNumber, ordinal, name)
	intent := strings.TrimSpace(entry.Intent)
	if len(intent) > planShellBodyLimit {
		intent = intent[:planShellBodyLimit]
	}
	body = planStoryMarker(a, key) + "\n# " + name + "\n\n" + intent + "\n\n_First draft in progress. This issue is updated with the story draft as soon as it is ready._\n"
	return title, body
}

// createStoryIssues gives every promoted story its issue at once, so the owner
// sees all of them right away and each story can be a task of its own. Each issue
// gets the story marker and a short drafting body, is attached to the parent and
// is bound to its story; the first draft later updates that issue.
//
// It is the parent's work and is safe to repeat: an issue created in an earlier,
// interrupted pass is found again by its marker and never created twice, and an
// attachment that did not complete is retried until the story has a draft.
func (r *planGraphRuntime) createStoryIssues(ctx context.Context) (err error) {
	if r.story != "" {
		return nil
	}
	s, _, err := r.state(ctx)
	if err != nil {
		return err
	}
	set := s.StorySet
	if set == nil || !set.Promoted {
		return nil
	}
	parent := r.parentRef.String()
	owed := false
	for _, entry := range set.Entries {
		draft, err := r.opts.DB.GetPlanStoryDraft(ctx, parent, set.Keys[entry.ID])
		if err != nil {
			return fmt.Errorf("read story %s: %w", set.Keys[entry.ID], err)
		}
		owed = owed || (!draft.Retired && draft.Candidate == "")
	}
	if !owed {
		return nil
	}
	unlock, err := r.opts.DB.LockStoryProgress(ctx, parent, r.opts.MCP.Paths.StoryProgressLocksDir())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	forge, err := r.storyForge()
	if err != nil {
		return err
	}
	repo := github.Repo{Owner: r.parentRef.Owner, Name: r.parentRef.Repo}
	authority := PlanStoryAuthority{Parent: parent, Repo: repo, ParentNumber: r.parentNumber, ParentID: r.parentID}
	var listed []github.Issue
	haveListing := false
	for _, entry := range set.Entries {
		key := set.Keys[entry.ID]
		draft, err := r.opts.DB.GetPlanStoryDraft(ctx, parent, key)
		if err != nil {
			return fmt.Errorf("read story %s: %w", key, err)
		}
		if draft.Retired || draft.Candidate != "" {
			continue
		}
		if draft.IssueID == 0 {
			title, body := planStoryShell(authority, r.parentNumber, draft.Ordinal, key, entry)
			if !haveListing {
				if listed, err = forge.ListFollowupIssues(ctx, repo); err != nil {
					return fmt.Errorf("list issues to find story %s: %w", key, err)
				}
				haveListing = true
			}
			var found *github.Issue
			for i := range listed {
				if strings.HasPrefix(listed[i].Body, planStoryMarker(authority, key)+"\n") {
					found = &listed[i]
					break
				}
			}
			if found == nil {
				safeTitle, err := r.opts.MCP.Redactor.Apply(ctx, title, 256)
				if err != nil {
					return err
				}
				safeBody, err := r.opts.MCP.Redactor.Apply(ctx, body, planbrief.MaxStoryBodyBytes)
				if err != nil {
					return err
				}
				created, err := forge.CreateIssue(ctx, repo, safeTitle, safeBody)
				if err != nil {
					return fmt.Errorf("create the issue of story %s: %w", key, err)
				}
				found = &created
			}
			if err = r.opts.DB.BindPlanStoryShell(ctx, parent, key, found.ID, found.Number, PlanIssueRevision(found.Title, found.Body)); err != nil {
				return err
			}
			draft.IssueID, draft.IssueNumber = found.ID, found.Number
		}
		if _, err = forge.GetParentIssue(ctx, repo, draft.IssueNumber); errors.Is(err, github.ErrNotFound) {
			err = forge.AddSubIssue(ctx, repo, r.parentNumber, draft.IssueID)
		}
		if err != nil {
			return fmt.Errorf("attach story %s to its parent: %w", key, err)
		}
	}
	return nil
}
