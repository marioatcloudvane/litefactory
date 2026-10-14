package turn

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// planSetStage is the stage that proposes a story set. It is about no story, so
// its pick carries no story key.
const planSetStage = "set"

// planPick is the factory's choice of the next Plan work. Stage is empty when
// nothing is eligible; Key is empty at the set stage, which is about no story.
type planPick struct {
	Key, Stage string
	// Create marks a promoted story that has no draft yet.
	Create bool
}

// pickPlanStep chooses the next work from saved Plan state alone, so the same
// state always yields the same pick and a pass can ask again after every draft.
//
// A story task passes its own story key and is offered only that story's next
// stage, by the one upstream-approval rule every stage obeys. The parent passes
// no key and issued, the stories that already have an issue and so a task of
// their own: it never picks one of them, and keeps only the set stage and any
// story that has no issue yet. Of those, promoted stories owed a first draft
// come first, in stable topological order: dependencies before dependants,
// displayed order among the ready.
func pickPlanStep(s planbrief.PlanState, story string, issued map[string]bool) (planPick, error) {
	if story != "" {
		stage, create, ok := s.StoryNextStep(story)
		if !ok {
			return planPick{}, nil
		}
		return planPick{Key: story, Stage: stage, Create: create}, nil
	}
	drafted := func(key string) bool { return s.StoryDrafted(key) || issued[key] }
	if _, key, ok, err := s.StorySet.NextUndrafted(drafted); err != nil {
		return planPick{}, fmt.Errorf("select next promoted story: %w", err)
	} else if ok {
		return planPick{Key: key, Stage: "story", Create: true}, nil
	}
	if len(s.Stories) == 0 {
		if s.StorySet != nil && s.StorySet.Promoted {
			// Promoted with keys not yet reserved: there is nothing to propose again.
			return planPick{}, nil
		}
		// No set yet, or a proposed set the owner has not promoted: the set stage
		// proposes it, or a changed one under a new revision. A scope with stories
		// never reaches it, and neither does a promoted set.
		return planPick{Stage: planSetStage}, nil
	}
	for _, key := range s.EligiblePlanStories() {
		if issued[key] {
			continue
		}
		for _, item := range s.Stories {
			if item.Key == key {
				return planPick{Key: item.Key, Stage: item.Stage}, nil
			}
		}
		return planPick{}, fmt.Errorf("eligible Plan story %q missing", key)
	}
	return planPick{}, nil
}

// selectDiscussion binds ordinary input to its issue without changing saved work.
func (r *planGraphRuntime) selectDiscussion(ctx context.Context) error {
	s, rev, err := r.state(ctx)
	if err != nil {
		return err
	}
	if s.SharedRevision == "" {
		s.SharedRevision = "initial"
		if err = r.persist(ctx, s, rev); err != nil {
			return err
		}
	}
	key := r.story
	if reply := r.req.Trigger.PlanReply; reply != nil {
		key = reply.Key
	}
	if key == "" {
		r.selected, r.stage = "", planSetStage
		return nil
	}
	if reason := s.PlanReplyReason(key, r.req.Trigger.Body); reason != "" {
		return &PlanReplyRefusal{Reason: reason, Err: errors.New(reason)}
	}
	for _, item := range s.Stories {
		if item.Key == key {
			r.selected, r.stage = key, item.Stage
			return nil
		}
	}
	return &PlanReplyRefusal{Reason: "This story is no longer in the plan.", Err: errors.New("plan discussion story not found")}
}

// retryFailedStory keeps healthy discussion unchanged, while an owner reply to
// a failed, non-waiting story uses recovery and normal stage selection. It runs
// after admitReply so recovery never substitutes for source authentication.
func (r *planGraphRuntime) retryFailedStory(ctx context.Context) (bool, error) {
	if !r.discussion || r.story == "" {
		return false, nil
	}
	s, _, err := r.state(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range s.Stories {
		if item.Key != r.story {
			continue
		}
		if !item.Active() || item.Wait != nil {
			return false, nil
		}
		if item.Blocked() {
			return true, r.releaseOwnerEditConflicts(ctx, item)
		}
		instance, err := r.opts.DB.LatestWorkflowInstance(ctx, r.req.SessionID, "plan")
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		source, err := latestResearchAgentAttempt(ctx, r.opts, r.definition.Profile, instance.ID, "")
		if err != nil || source.ID == 0 {
			return false, err
		}
		return r.opts.DB.GraphFailureReported(ctx, source.RunID, PlanFailureNode)
	}
	return false, nil
}

// ownerEditConflict is the hold a story gets when a write's readback differs
// from the expected content, on the assumption that the owner edited it.
const ownerEditConflict = "owner edits conflict with expected content revision"

// releaseOwnerEditConflicts lets the owner's own retry settle a story held only
// by owner-edit conflicts. The hold exists to protect the owner's edits; the
// owner asking to retry says there are none to keep, so the stale requests are
// settled and the retry drafts afresh, as the failure notice promises. Such
// holds recorded before #821 can never re-verify: their content was rendered
// before #824/#825 changed titles and markers. Any other hold is left alone.
func (r *planGraphRuntime) releaseOwnerEditConflicts(ctx context.Context, item planbrief.PlanStoryState) error {
	if item.Hold != "" || item.LifecycleHold != "" || (item.EffectHold != "" && item.EffectHold != ownerEditConflict) {
		return nil
	}
	requests := map[string]bool{}
	for request, hold := range item.EffectHolds {
		if hold != ownerEditConflict {
			return nil
		}
		requests[request] = true
	}
	if item.EffectHold == ownerEditConflict && item.EffectRequest != "" {
		requests[item.EffectRequest] = true
	}
	parent := r.parentRef.String()
	for request := range requests {
		state, revision, err := r.opts.DB.GetPlanWorkflowState(ctx, parent)
		if err != nil {
			return err
		}
		for i := range state.Stories {
			s := &state.Stories[i]
			if s.Key != item.Key {
				continue
			}
			if s.EffectRequest == request {
				s.Effect = ""
			}
			s.ClearEffectHold(request)
		}
		if err = r.opts.DB.SettlePlanStoryDebt(ctx, parent, request, state, revision, false); err != nil {
			return err
		}
	}
	return nil
}

// unappliedReply explains retained state without clearing recovery or approval debt.
func (r *planGraphRuntime) unappliedReply(ctx context.Context) error {
	s, _, err := r.state(ctx)
	if err != nil {
		return err
	}
	reason := "This story is no longer part of the plan."
	for _, item := range s.Stories {
		if item.Key != r.story {
			continue
		}
		var holds []string
		for request, hold := range item.EffectHolds {
			holds = append(holds, request+": "+hold)
		}
		sort.Strings(holds)
		for _, hold := range []string{item.Hold, item.LifecycleHold, item.EffectHold} {
			if hold != "" {
				holds = append(holds, hold)
			}
		}
		switch {
		case len(holds) > 0:
			reason = "This reply cannot advance the story: " + strings.Join(holds, "; ")
		case !item.Active():
			reason = s.PlanReplyHold(r.story)
		case item.Wait != nil:
			reason = "This story is waiting for the owner's reply to its current review."
		case !s.UpstreamApproved(item.Key, item.Stage):
			reason = "This story needs approval of its upstream stories before its next stage can run."
		default:
			reason = "This story has no eligible next stage; its current stage and earlier approvals must be settled first."
		}
		break
	}
	return &PlanReplyRefusal{Reason: reason, Err: errors.New(reason)}
}
