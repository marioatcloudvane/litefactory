package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// planShellBodyLimit bounds the intent text copied into a story's drafting body.
const planShellBodyLimit = 4000

// planStoryShell renders the short body a story issue has until its first draft:
// its name, its intent and a line saying the first draft is in progress. It
// carries the story marker, which binds the issue to its story, and nothing the
// draft later adds.
func planStoryShell(a PlanStoryAuthority, parentNumber int, ordinal int64, key string, entry planbrief.PlanStorySetEntry, parentKey ...string) (title, body string) {
	name := strings.Join(strings.Fields(entry.Name), " ")
	keyPrefix := fmt.Sprint(parentNumber)
	if len(parentKey) > 0 {
		keyPrefix = parentKey[0]
	}
	title = fmt.Sprintf("%s-%03d: %s", keyPrefix, ordinal, name)
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
	forge, err := r.storyForge()
	if err != nil {
		return err
	}
	h := PlanStoryActivity{DB: r.opts.DB, Forge: forge, Redactor: r.opts.MCP.Redactor, LockDir: r.opts.MCP.Paths.StoryProgressLocksDir()}
	return h.ReconcileShells(ctx, PlanStoryAuthority{Parent: r.parentRef.String(), Repo: github.RepoFromRef(r.parentRef), ParentNumber: r.parentNumber, ParentID: r.parentID})
}

// ReconcileShells restores promotion shells before ordinary child discovery.
// The caller owns the parent worker lock and planning eligibility.
func (h *PlanStoryActivity) ReconcileShells(ctx context.Context, authority PlanStoryAuthority) (err error) {
	s, _, err := h.DB.GetPlanWorkflowState(ctx, authority.Parent)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	set := s.StorySet
	if set == nil || !set.Promoted {
		return nil
	}
	active := map[string]bool{}
	for _, story := range s.Stories {
		active[story.Key] = story.Active()
	}
	parent := authority.Parent
	owed := false
	for _, entry := range set.Entries {
		draft, err := h.DB.GetPlanStoryDraft(ctx, parent, set.Keys[entry.ID])
		if err != nil {
			return fmt.Errorf("read story %s: %w", set.Keys[entry.ID], err)
		}
		owed = owed || (active[draft.Key] && !draft.Retired && draft.Candidate == "")
	}
	if !owed {
		return nil
	}
	unlock, err := h.DB.LockStoryProgress(ctx, parent, h.LockDir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	forge := h.Forge
	repo := authority.Repo
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return err
	}
	display, err := lookupTicketDisplay(ctx, forge, ref, authority.ParentNumber)
	if err != nil {
		return err
	}
	var listed []github.Issue
	haveListing := false
	for _, entry := range set.Entries {
		key := set.Keys[entry.ID]
		draft, err := h.DB.GetPlanStoryDraft(ctx, parent, key)
		if err != nil {
			return fmt.Errorf("read story %s: %w", key, err)
		}
		if !active[key] || draft.Retired || draft.Candidate != "" {
			continue
		}
		if draft.IssueID == 0 {
			title, body := planStoryShell(authority, authority.ParentNumber, draft.Ordinal, key, entry, display.TitleKey)
			if !haveListing {
				if listed, err = forge.ListFollowupIssues(ctx, repo); err != nil {
					return fmt.Errorf("list issues to find story %s: %w", key, err)
				}
				haveListing = true
			}
			var found *github.Issue
			for i := range listed {
				if strings.HasPrefix(listed[i].Body, planStoryMarker(authority, key)+"\n") {
					if found != nil {
						return fmt.Errorf("multiple issues match story %s", key)
					}
					found = &listed[i]
				}
			}
			if found == nil {
				safeTitle, err := h.Redactor.Apply(ctx, title, 256)
				if err != nil {
					return err
				}
				safeBody, err := h.Redactor.Apply(ctx, body, planbrief.MaxStoryBodyBytes)
				if err != nil {
					return err
				}
				created, err := createTicketEffect(ctx, forge, "plan-shell:"+parent+":"+key, repo, authority.ParentNumber, safeTitle, safeBody)
				if err != nil {
					return fmt.Errorf("create the issue of story %s: %w", key, err)
				}
				found = &created
			}
			if found.ID < 1 || found.Number < 1 {
				return fmt.Errorf("invalid shell identity for story %s", key)
			}
			observed, readErr := forge.GetIssue(ctx, repo, found.Number)
			if readErr != nil {
				return fmt.Errorf("read story %s: %w", key, readErr)
			}
			if observed.ID != found.ID || observed.Number != found.Number || !strings.HasPrefix(observed.Body, planStoryMarker(authority, key)+"\n") {
				return fmt.Errorf("shell identity changed for story %s", key)
			}
			if err = ensurePlanShellParent(ctx, forge, authority, observed); err != nil {
				return fmt.Errorf("attach story %s to its parent: %w", key, err)
			}
			found = &observed
			if err = h.DB.BindPlanStoryShell(ctx, parent, key, found.ID, found.Number, PlanIssueRevision(found.Title, found.Body)); err != nil {
				return err
			}
			draft.IssueID, draft.IssueNumber = found.ID, found.Number
		}
		if err = ensurePlanShellParent(ctx, forge, authority, github.Issue{ID: draft.IssueID, Number: draft.IssueNumber}); err != nil {
			return fmt.Errorf("attach story %s to its parent: %w", key, err)
		}
	}
	return nil
}

func ensurePlanShellParent(ctx context.Context, forge PlanStoryForge, a PlanStoryAuthority, issue github.Issue) error {
	parent, err := forge.GetParentIssue(ctx, a.Repo, issue.Number)
	if errors.Is(err, github.ErrNotFound) {
		if err = forge.AddSubIssue(ctx, a.Repo, a.ParentNumber, issue.ID); err != nil {
			return err
		}
		parent, err = forge.GetParentIssue(ctx, a.Repo, issue.Number)
	}
	if err != nil {
		return err
	}
	if parent.Number != a.ParentNumber || (a.ParentID != 0 && parent.ID != a.ParentID) {
		return errors.New("shell belongs to a different parent")
	}
	return nil
}
