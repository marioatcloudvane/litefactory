package daemon

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// planWorkPayload is the instruction of a run the factory starts by itself for a
// Plan task. The stage, the story and everything else the run needs come from the
// saved Plan state, never from this text.
const planWorkPayload = "Continue the Plan work of this issue."

// planWorkID is the identity of one piece of factory-started Plan work. It is a
// pure function of what the work is, so the same work is the same schedule: a
// stage that failed is not started again until the owner replies, and work whose
// state moved on is new work. The band keeps it clear of every forge comment id.
func planWorkID(kind string, parts ...string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind + "\x00" + strings.Join(parts, "\x00")))
	return int64(h.Sum64()&(1<<56-1)) | 1<<56
}

// storyWorkID names the work a story task has in the saved Plan state, if any:
// the next stage it may run, or an effect it has to reconcile. One rule decides
// it for the offer and for the run, so a story is offered exactly when its run
// would find something to do.
func storyWorkID(state planbrief.PlanState, key string) (int64, bool) {
	if stage, create, ok := state.StoryNextStep(key); ok {
		candidate := ""
		for _, item := range state.Stories {
			if item.Key == key {
				candidate = item.Candidate
			}
		}
		return planWorkID("story", key, stage, candidate, strconv.FormatBool(create)), true
	}
	for _, item := range state.Stories {
		if item.Key == key && item.Active() && item.Effect != "" && item.Effect != "applied" {
			return planWorkID("effect", key, item.EffectRequest), true
		}
	}
	return 0, false
}

// parentWorkIDWithScope names the work a Plan parent has in the saved state: posting the
// assembled review once every story has all three approvals.
func parentWorkIDWithScope(state planbrief.PlanState, scope planbrief.ContentScope) (int64, bool) {
	if ref, due := state.AssemblyDue(scope); due {
		return planWorkID("assemble", ref), true
	}
	return 0, false
}

// planFrozen reports that the parent's feature generation is past planning, so
// none of its stories may run another stage.
func (d *Daemon) planFrozen(ctx context.Context, parent string) (bool, error) {
	feature, err := d.db.GetManagedFeature(ctx, parent)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return feature.State != "planning", nil
}

// planWorkFor reads the Plan work of a task: its story's when it is a story
// issue, the parent's when it is a graph Plan parent, and none otherwise.
func (d *Daemon) planWorkFor(ctx context.Context, t *task) (int64, bool, error) {
	story, owned, err := d.db.GraphPlanStoryAt(ctx, t.repo.ID, t.issue.Number)
	if err != nil {
		return 0, false, err
	}
	if owned {
		if frozen, err := d.planFrozen(ctx, story.Parent); err != nil || frozen {
			return 0, false, err
		}
		state, _, err := d.db.GetPlanWorkflowState(ctx, story.Parent)
		if err != nil {
			return 0, false, err
		}
		id, ok := storyWorkID(state, story.Key)
		return id, ok, nil
	}
	route, err := d.db.PlanExecutionPath(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && route != "graph") {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	state, _, err := d.db.GetPlanWorkflowState(ctx, t.ref.String())
	if err != nil {
		return 0, false, err
	}
	scope, err := d.planContentScope(ctx, t)
	if err != nil {
		return 0, false, err
	}
	id, ok := parentWorkIDWithScope(state, scope)
	return id, ok, nil
}

// startPlanWork runs the Plan work a task owes, after its comments: the next
// stage of a story, or the assembled review of a parent. The work is a schedule
// keyed by what it is, which gives it the private run identity every run needs,
// and keeps a failed stage from being started again on every tick. The owner's
// reply on the issue is what retries it.
func (d *Daemon) startPlanWork(ctx context.Context, t *task) error {
	id, ok, err := d.planWorkFor(ctx, t)
	if err != nil || !ok {
		return err
	}
	attempted, err := d.db.ScheduleAttempted(ctx, t.session.ID, id)
	if err != nil || attempted {
		return err
	}
	stored, _, err := d.db.CreateOrGetSchedule(ctx, store.CreateScheduleParams{
		SessionID: t.session.ID, SourceCommentID: id, OwnerHandle: d.filter.Identities().OwnerHandle,
		Payload: planWorkPayload, DueAt: t.tickAt.UTC(),
	})
	if err != nil {
		return err
	}
	if stored.Status != store.SchedulePending {
		return nil
	}
	reserved, ok, err := d.db.ReserveSchedule(ctx, stored.ID, t.tickAt.UTC())
	if err != nil || !ok {
		return err
	}
	scheduled := *t
	scheduled.scheduled = &reserved
	d.runScheduled(ctx, &scheduled, reserved)
	return nil
}

// planWorkSettled reports that a schedule the factory started for Plan work found
// nothing left to run: the saved state moved on before the run began. Such a
// schedule is finished, not returned to pending, or it would be offered again on
// every tick.
func (d *Daemon) planWorkSettled(ctx context.Context, t *task, scheduled store.Schedule) bool {
	if scheduled.Payload != planWorkPayload {
		return false
	}
	id, ok, err := d.planWorkFor(ctx, t)
	return err == nil && (!ok || id != scheduled.SourceCommentID)
}

// graphPlanStories offers every story task that has work: a stage it may run, or
// an effect it has to reconcile. The existing pool admits as many as the run
// limit allows and the rest are offered again on the next tick, so no run ever
// waits inside a slot for a review or an upstream story.
func (d *Daemon) graphPlanStories(ctx context.Context, byRepo map[int64]*repoQueue, seen map[int64]*candidate) error {
	stories, err := d.db.ListGraphPlanStories(ctx)
	if err != nil {
		return err
	}
	states := map[string]planbrief.PlanState{}
	frozen := map[string]bool{}
	for _, story := range stories {
		q := byRepo[story.RepoID]
		if q == nil {
			continue
		}
		// A generation that is past planning is frozen: its stories run no stage.
		isFrozen, known := frozen[story.Parent]
		if !known {
			var frozenErr error
			if isFrozen, frozenErr = d.planFrozen(ctx, story.Parent); frozenErr != nil {
				return frozenErr
			}
			frozen[story.Parent] = isFrozen
		}
		if isFrozen {
			continue
		}
		state, loaded := states[story.Parent]
		if !loaded {
			if state, _, err = d.db.GetPlanWorkflowState(ctx, story.Parent); err != nil {
				return err
			}
			states[story.Parent] = state
		}
		id, ok := storyWorkID(state, story.Key)
		if !ok {
			continue
		}
		session, err := d.db.GetOrCreateSession(ctx, q.repo, story.IssueNumber)
		if err != nil {
			return fmt.Errorf("find the task of story %s: %w", story.Key, err)
		}
		attempted, err := d.db.ScheduleAttempted(ctx, session.ID, id)
		if err != nil {
			return err
		}
		if attempted {
			continue
		}
		if prior := seen[session.ID]; prior != nil {
			prior.addSource("plan_story")
			continue
		}
		ref, err := naming.ParseRef(session.TaskRef)
		if err != nil {
			return err
		}
		c := q.candidate(session, ref)
		c.addSource("plan_story")
		q.candidates = append(q.candidates, c)
		seen[session.ID] = c
	}
	return nil
}

// handlePlanStoryComment answers an owner's comment on a story issue with that
// story's own task: the reply is handled where it was written. Approval, feedback
// and retirement follow the same exact rules as ever; a reply the rules refuse is
// explained on this issue and consumed.
func (d *Daemon) handlePlanStoryComment(ctx context.Context, t *task, c github.Comment, story store.GraphPlanStory, log *slog.Logger) error {
	ids := d.filter.Identities()
	if c.Author.ID != ids.OwnerUserID || turn.HasMarker(c.Body) {
		d.commentFate(ctx, t, c.ID, fateIgnored, "ignored, only the owner's plain comments are Plan input")
		_, err := d.reporter.Consume(ctx, t.session.ID, c.ID)
		return d.leave(ctx, t, log, "could not consume a comment that is not Plan input", err)
	}
	d.acknowledge(ctx, t, c.ID)
	parent := story.Parent
	draft, err := d.db.GetPlanStoryDraft(ctx, parent, story.Key)
	if err != nil {
		return d.leave(ctx, t, log, "could not read the story draft binding", err)
	}
	state, _, err := d.db.GetPlanWorkflowState(ctx, parent)
	if err != nil {
		return d.leave(ctx, t, log, "could not read the Plan state", err)
	}
	reason := state.PlanReplyReason(story.Key, c.Body)
	explanation, explainErr := d.db.PlanReplyExplanation(ctx, t.ref.String(), c.ID)
	if explainErr != nil && !errors.Is(explainErr, store.ErrNotFound) {
		return d.leave(ctx, t, log, "could not read the retained reply explanation", explainErr)
	}
	if explainErr == nil {
		reason = explanation.Reason
	}
	if reason != "" {
		return d.explainAndConsume(ctx, t, c, draft, reason, log)
	}
	var wait *planbrief.PlanWait
	for _, item := range state.Stories {
		if item.Key == story.Key {
			wait = item.Wait
		}
	}
	reply := &planbrief.PlanOwnerReply{ID: c.ID, OwnerID: ids.OwnerUserID, ActorID: c.Author.ID, IssueID: t.issue.ID, Key: story.Key, Body: c.Body}
	if wait != nil {
		reply.Stage, reply.Candidate, reply.WaitID = wait.Stage, wait.Candidate, wait.ID
	}

	trigger := turn.Trigger{CommentID: c.ID, Body: c.Body, Author: c.Author.Login, AuthorID: c.Author.ID, PlanReply: reply}
	err = d.run(ctx, t, trigger, turn.Command{Kind: turn.KindFreeText, Text: c.Body}, log)
	var refusal *turn.PlanReplyRefusal
	if errors.As(err, &refusal) {
		// The review rules refuse this reply for good. The explanation is keyed
		// by the comment, so a retry resumes it; confirmed delivery consumes it.
		return d.explainAndConsume(ctx, t, c, draft, refusal.Reason, log)
	}
	return err
}

// explainAndConsume answers a reply the review rules decline, on the issue where
// it was written, and consumes it once the explanation is confirmed delivered.
func (d *Daemon) explainAndConsume(ctx context.Context, t *task, c github.Comment, draft store.PlanStoryDraft, reason string, log *slog.Logger) error {
	confirmed, err := d.explainPlanReply(ctx, t, draft, c.ID, reason)
	if err != nil {
		return d.leave(ctx, t, log, "could not explain a refused Plan reply", err)
	}
	if !confirmed {
		return nil
	}
	d.commentFate(ctx, t, c.ID, fateIgnored, "explained, the reply does not match the current review")
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not consume an explained Plan reply", err)
}
