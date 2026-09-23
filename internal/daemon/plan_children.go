package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// refinePlanChildren keeps draft child discussion on the parent execution slot.
// Numeric owner identity and current parent relation are observed before routing.
func (d *Daemon) refinePlanChildren(ctx context.Context, t *task) (bool, error) {
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) || route != "graph" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	state, _, err := d.db.GetPlanWorkflowState(ctx, t.ref.String())
	if err != nil {
		return false, err
	}
	drafts, err := d.db.ListPlanStoryDrafts(ctx, t.ref.String())
	if err != nil {
		return false, err
	}
	relation, ok := d.forge.(storyRelationForge)
	if !ok {
		return false, errors.New("plan child routing requires direct parent observation")
	}
	ids := d.filter.Identities()
	for _, draft := range drafts {
		if draft.Retired || draft.IssueNumber < 1 {
			continue
		}
		child, err := d.discoveryIssue(ctx, t.forgeRepo, draft.IssueNumber)
		if err != nil {
			return false, err
		}
		parent, err := discoveryCall(ctx, d, discoveryKey(t.forgeRepo, "parent", draft.IssueNumber), 1, func(ctx context.Context) (github.Issue, error) {
			return relation.GetParentIssue(ctx, t.forgeRepo, draft.IssueNumber)
		})
		if err != nil {
			return false, err
		}
		if child.ID != draft.IssueID || parent.ID != t.issue.ID || parent.Number != t.issue.Number || child.State != github.StateOpen {
			return false, fmt.Errorf("plan child %s identity, state or parent changed", draft.Key)
		}
		if turn.PlanIssueRevision(child.Title, child.Body) != draft.Revision {
			scope := planbrief.ContentScope{Roles: []string{"go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}, IssueRefs: []string{}}
			if err = turn.ReconcilePlanChildEdit(ctx, d.db, t.ref.String(), t.issue.Number, t.issue.ID, child, scope); err != nil {
				return false, err
			}
			state, _, err = d.db.GetPlanWorkflowState(ctx, t.ref.String())
			if err != nil {
				return false, err
			}
		}

		var wait *planbrief.PlanWait
		for _, item := range state.Stories {
			if item.Key == draft.Key {
				wait = item.Wait
				break
			}
		}
		after, err := d.db.StoryDiscoveryCursor(ctx, t.session.ID, draft.IssueNumber)
		if err != nil {
			return false, err
		}
		comments, err := d.discoveryComments(ctx, t.forgeRepo, draft.IssueNumber, after)
		if err != nil {
			return false, err
		}
		for _, comment := range comments {
			if comment.ID <= after || slices.Contains(state.ConsumedReplies, comment.ID) {
				continue
			}
			if comment.Author.ID != ids.OwnerUserID || comment.Author.ID == ids.BotUserID || comment.Author.IsBot || turn.HasMarker(comment.Body) {
				if err = d.db.AdvanceStoryDiscovery(ctx, t.session.ID, draft.IssueNumber, comment.ID); err != nil {
					return false, err
				}
				continue
			}
			if wait == nil {
				current, revision, err := d.db.GetPlanWorkflowState(ctx, t.ref.String())
				if err != nil {
					return false, err
				}
				if err = current.OpenPlanWait(draft.Key, "feedback-"+draft.Candidate, "question", child.ID); err != nil {
					return false, err
				}
				if _, err = d.db.SavePlanWorkflowState(ctx, t.ref.String(), revision, current); err != nil {
					return false, err
				}
				for _, item := range current.Stories {
					if item.Key == draft.Key {
						wait = item.Wait
						break
					}
				}
				if wait == nil {
					return false, errors.New("plan feedback wait was not retained")
				}
			}
			reply := &planbrief.PlanOwnerReply{ID: comment.ID, OwnerID: ids.OwnerUserID, ActorID: comment.Author.ID, IssueID: child.ID, Key: draft.Key, Stage: wait.Stage, Candidate: wait.Candidate, WaitID: wait.ID, Body: comment.Body}
			trigger := turn.Trigger{CommentID: comment.ID, Body: comment.Body, Author: comment.Author.Login, AuthorID: comment.Author.ID, PlanReply: reply}
			if err = d.run(ctx, t, trigger, turn.Command{Kind: turn.KindFreeText, Text: comment.Body}, t.log); err != nil {
				return true, err
			}
			if err = d.db.AdvanceStoryDiscovery(ctx, t.session.ID, draft.IssueNumber, comment.ID); err != nil {
				return true, err
			}
			return true, nil
		}
	}
	return false, nil
}
