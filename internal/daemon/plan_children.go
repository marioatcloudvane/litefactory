package daemon

import (
	"context"
	"errors"
	"slices"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// refinePlanChildren keeps draft child discussion on the parent execution slot.
// Numeric owner identity and current parent relation are observed before routing.
func (d *Daemon) refinePlanChildren(ctx context.Context, t *task) (bool, error) {
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if route != "graph" {
		return false, nil
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
		if draft.IssueNumber < 1 {
			continue
		}
		child, available, err := d.observePlanChild(ctx, t, draft, relation)
		if err != nil {
			return false, err
		}
		state, _, err = d.db.GetPlanWorkflowState(ctx, t.ref.String())
		if err != nil {
			return false, err
		}

		if available && turn.PlanIssueRevision(child.Title, child.Body) != draft.Revision {
			scope := planbrief.ContentScope{Roles: []string{"go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}, IssueRefs: []string{}}
			if err = turn.ReconcilePlanChildEdit(ctx, d.db, t.ref.String(), t.issue.Number, t.issue.ID, child, scope); err != nil {
				if !errors.Is(err, turn.ErrPlanChildEdit) {
					return false, err
				}
				if err = d.db.ObservePlanStoryLifecycle(ctx, t.ref.String(), draft.Key, planbrief.PlanLifecycle{}, planChildContentHold); err != nil {
					return false, err
				}
				available = false
			}
			state, _, err = d.db.GetPlanWorkflowState(ctx, t.ref.String())
			if err != nil {
				return false, err
			}
		}

		// A restored valid revision resolves only the discovery content hold.
		for _, item := range state.Stories {
			if available && item.Key == draft.Key && item.LifecycleHold == planChildContentHold {
				if err = d.db.ObservePlanStoryLifecycle(ctx, t.ref.String(), draft.Key, planbrief.PlanLifecycle{}, ""); err != nil {
					return false, err
				}
				state, _, err = d.db.GetPlanWorkflowState(ctx, t.ref.String())
				if err != nil {
					return false, err
				}
				break
			}
		}

		if child.ID != draft.IssueID || child.Number != draft.IssueNumber {
			continue
		}
		if !available && state.PlanReplyHold(draft.Key) == "" {
			continue
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
			explanation, explainErr := d.db.PlanReplyExplanation(ctx, t.ref.String(), comment.ID)
			if explainErr != nil && !errors.Is(explainErr, store.ErrNotFound) {
				return false, explainErr
			}
			reason := state.PlanReplyReason(draft.Key, comment.Body)
			if explainErr == nil {
				reason = explanation.Reason
			}
			if reason != "" {
				confirmed, err := d.explainPlanReply(ctx, t, draft, comment.ID, reason)
				if err != nil {
					return false, err
				}
				if !confirmed {
					break
				}
				after = comment.ID
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
