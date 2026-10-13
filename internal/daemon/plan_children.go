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
// A child that is a task of its own is only observed here: its owner edits,
// closure and parent relation, never its comments.
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
	ids := d.ticketIdentities(ctx)
	for _, draft := range drafts {
		if draft.IssueNumber < 1 {
			continue
		}
		// Only a child a listing or hint named since its last sweep, or one
		// never swept, is read; the rest have nothing new to say.
		owed, err := d.db.StoryDiscoveryOwed(ctx, t.session.ID, draft.IssueNumber)
		if err != nil {
			return false, err
		}
		if !owed {
			continue
		}
		// Record the debt before the sweep advances any cursor, so an early
		// return keeps it owed even when it had never been swept before.
		if err = d.db.MarkStoryDiscoveryPending(ctx, t.session.ID, draft.IssueNumber); err != nil {
			return false, err
		}
		start, err := d.db.StoryDiscoveryCursor(ctx, t.session.ID, draft.IssueNumber)
		if err != nil {
			return false, err
		}
		child, available, err := d.observePlanChild(ctx, t, draft, relation)
		if err != nil {
			return false, err
		}
		state, _, err = d.db.GetPlanWorkflowState(ctx, t.ref.String())
		if err != nil {
			return false, err
		}

		// A story still drafting has only the short body the factory wrote, which
		// is not a draft an owner edit could be read against.
		if available && draft.Candidate != "" && turn.PlanIssueRevision(child.Title, child.Body) != draft.Revision {
			scope, err := d.planContentScope(ctx, t)
			if err != nil {
				return false, err
			}
			if err = turn.ReconcilePlanChildEdit(ctx, d.db, t.ref.String(), t.issue.Number, t.issue.ID, child, scope, planParentTitleKey(t.issue, t.ref.Forge)); err != nil {
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

		// A story that is a task of its own answers its own comments and runs its
		// own stages; the parent only observes its issue.
		if story, task, taskErr := d.db.GraphPlanStoryAt(ctx, t.repo.ID, draft.IssueNumber); taskErr != nil {
			return false, taskErr
		} else if task && child.State == "open" {
			// Transfer durable polling responsibility before retiring the parent's
			// discovery debt, including observations received while awaiting review.
			session, err := d.managedChildSession(ctx, t.repo, story.Parent, story.IssueNumber, story.IssueID)
			if err != nil {
				return false, err
			}
			if err = d.db.SetSessionNeedsPoll(ctx, session.ID); err != nil {
				return false, err
			}
			t.log.Info("plan story polling handed to child", "parent_session", t.session.ID,
				"child_session", session.ID, "issue", story.IssueNumber, "reason", "open_graph_plan_story")
			if err = d.sweptPlanChild(ctx, t, draft.IssueNumber, start); err != nil {
				return false, err
			}
			continue
		}
		if child.ID != draft.IssueID || child.Number != draft.IssueNumber {
			if err = d.sweptPlanChild(ctx, t, draft.IssueNumber, start); err != nil {
				return false, err
			}
			continue
		}
		if !available && state.PlanReplyHold(draft.Key) == "" {
			if err = d.sweptPlanChild(ctx, t, draft.IssueNumber, start); err != nil {
				return false, err
			}
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
		complete := true
		for _, comment := range comments {
			if comment.ID <= after || slices.Contains(state.ConsumedReplies, comment.ID) {
				continue
			}
			routed, routeErr := d.admitDoctor(ctx, t.repo, child, comment)
			if routeErr != nil {
				return false, routeErr
			}
			if routed {
				if err := d.db.AdvanceStoryDiscovery(ctx, t.session.ID, draft.IssueNumber, comment.ID); err != nil {
					return false, err
				}
				continue
			}
			eligible, authorityErr := d.eligibleProjectComment(ctx, t.session.ID, comment)
			if authorityErr != nil {
				return false, authorityErr
			}
			if !eligible || comment.Author.ID != ids.OwnerUserID || comment.Author.ID == ids.BotUserID || comment.Author.IsBot || turn.HasMarker(comment.Body) {
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
					complete = false
					break
				}
				after = comment.ID
				continue
			}
			reply := &planbrief.PlanOwnerReply{ID: comment.ID, OwnerID: ids.OwnerUserID, ActorID: comment.Author.ID, IssueID: child.ID, Key: draft.Key, Body: comment.Body}
			if wait != nil {
				reply.Stage, reply.Candidate, reply.WaitID = wait.Stage, wait.Candidate, wait.ID
			}

			trigger, triggerErr := d.projectTrigger(ctx, t.session.ID, comment)
			if triggerErr != nil {
				return false, triggerErr
			}
			trigger.PlanReply = reply
			if err = d.run(ctx, t, trigger, turn.Command{Kind: turn.KindFreeText, Text: comment.Body}, t.log); err != nil {
				var refusal *turn.PlanReplyRefusal
				if !errors.As(err, &refusal) {
					return true, err
				}
				// The review rules refuse this reply for good. The explanation is
				// keyed by the comment, so a retry resumes it; confirmed delivery
				// consumes the reply.
				if _, err = d.explainPlanReply(ctx, t, draft, comment.ID, refusal.Reason); err != nil {
					return true, err
				}
				return true, nil
			}
			if err = d.db.AdvanceStoryDiscovery(ctx, t.session.ID, draft.IssueNumber, comment.ID); err != nil {
				return true, err
			}
			return true, nil
		}
		if complete {
			if err = d.sweptPlanChild(ctx, t, draft.IssueNumber, start); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

// sweptPlanChild retires a child's sweep debt after a complete sweep. The
// cursor row it leaves behind (never rewound) is what stops a child with no
// comments from being owed a sweep on every tick. Every early return leaves
// the debt standing so the next worker sweeps the child again.
func (d *Daemon) sweptPlanChild(ctx context.Context, t *task, child int, cursor int64) error {
	if err := d.db.AdvanceStoryDiscovery(ctx, t.session.ID, child, cursor); err != nil {
		return err
	}
	return d.db.ClearStoryDiscoveryPending(ctx, t.session.ID, child)
}

func planParentTitleKey(issue github.Issue, forge string) string {
	if s := issue.QualifiedSnapshot; s != nil && s.DisplayKey != "" {
		return s.DisplayKey
	}
	if forge == "linear" {
		return "story"
	}
	return fmt.Sprint(issue.Number)
}
