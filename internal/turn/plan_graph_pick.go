package turn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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
		r.setPick(s, "", planSetStage)
		return nil
	}
	if reason := s.PlanReplyReason(key, r.req.Trigger.Body); reason != "" {
		return &PlanReplyRefusal{Reason: reason, Err: errors.New(reason)}
	}
	for _, item := range s.Stories {
		if item.Key == key {
			r.setPick(s, key, item.Stage)
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
		if r.rewindReason, err = r.storyRewindReason(ctx, s, item); err != nil {
			return false, err
		}
		if item.Blocked() {
			if item.Hold == "" && item.LifecycleHold == "" && item.EffectHold == "" && len(item.EffectHolds) == 0 {
				return true, r.clearSettledEffect(ctx, item)
			}
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

// clearSettledEffect drops a stale Effect marker from a story that has no hold
// of any kind. Recovery never revisits a request once it is completed, so a
// marker left behind by settling its holds would refuse every reply (#908).
// A request still owed recovery keeps its marker: recovery owns that debt.
func (r *planGraphRuntime) clearSettledEffect(ctx context.Context, item planbrief.PlanStoryState) error {
	parent := r.parentRef.String()
	if item.EffectRequest != "" {
		complete, err := r.opts.DB.PlanStoryCompleted(ctx, parent, item.EffectRequest)
		if err != nil || !complete {
			return err
		}
	}
	return r.opts.DB.UpdatePlanWorkflowState(ctx, parent, func(s *planbrief.PlanState) (bool, error) {
		for i := range s.Stories {
			current := &s.Stories[i]
			if current.Key != item.Key {
				continue
			}
			// Only the same shape is cleared; anything changed since is left to
			// recovery and normal selection.
			if current.Effect != item.Effect || current.EffectRequest != item.EffectRequest || current.Hold != "" || current.LifecycleHold != "" || current.EffectHold != "" || len(current.EffectHolds) > 0 {
				return false, nil
			}
			current.Effect = ""
			return true, nil
		}
		return false, nil
	})
}

// ownerEditConflict is the hold a story gets when a write's readback differs
// from the expected content, on the assumption that the owner edited it.
const ownerEditConflict = "owner edits conflict with expected content revision"

// childContentHold prefixes the lifecycle hold the child poll sets when a story
// issue's content can be read neither as the factory's draft nor as an owner
// edit (internal/daemon planChildContentHold).
const childContentHold = "Child content cannot be reconciled"

// releaseOwnerEditConflicts lets the owner's own retry settle a story held only
// by owner-edit conflicts. The hold exists to protect the owner's edits; the
// owner asking to retry says there are none to keep, so the stale requests are
// settled and the retry drafts afresh, as the failure notice promises. Such
// holds recorded before #821 can never re-verify: their content was rendered
// before #824/#825 changed titles and markers. Any other hold is left alone.
func (r *planGraphRuntime) releaseOwnerEditConflicts(ctx context.Context, item planbrief.PlanStoryState) error {
	contentHold := strings.HasPrefix(item.LifecycleHold, childContentHold)
	if item.Hold != "" || (item.LifecycleHold != "" && !contentHold) || (item.EffectHold != "" && item.EffectHold != ownerEditConflict) {
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
	// Only Linear produced false owner-edit conflicts: its normalized readback
	// was misjudged before #865/#866/#869. A GitHub conflict is a real edit and
	// stays the owner's to resolve, whatever the reply says.
	if linear, err := r.linearStory(ctx, item.Key); err != nil || !linear {
		return err
	}
	if len(requests) == 0 && !contentHold {
		// Blocked for another reason, such as an unknown remote outcome:
		// nothing here is the owner's to waive.
		return nil
	}
	if contentHold {
		// The poll could not read the factory's own unrecorded write as an
		// owner edit; the owner's retry says there is none.
		if err := r.opts.DB.UpdatePlanWorkflowState(ctx, parent, func(s *planbrief.PlanState) (bool, error) {
			for i := range s.Stories {
				if s.Stories[i].Key == item.Key && strings.HasPrefix(s.Stories[i].LifecycleHold, childContentHold) {
					s.Stories[i].LifecycleHold = ""
					return true, nil
				}
			}
			return false, nil
		}); err != nil {
			return err
		}
	}
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
	return r.rebaseOwnerConfirmedContent(ctx, item.Key)
}

func (r *planGraphRuntime) linearStory(ctx context.Context, key string) (bool, error) {
	d, err := r.opts.DB.GetPlanStoryDraft(ctx, r.parentRef.String(), key)
	if err != nil || d.IssueID == 0 {
		return false, err
	}
	forge, err := r.storyForge()
	if err != nil {
		return false, err
	}
	child, err := forge.GetIssue(ctx, github.Repo{Owner: r.parentRef.Owner, Name: r.parentRef.Repo}, d.IssueNumber)
	if err != nil {
		return false, err
	}
	return child.QualifiedSnapshot != nil && child.QualifiedSnapshot.Ref.Identity.Provider == ticket.Linear, nil
}

// rebaseOwnerConfirmedContent takes the live story issue as the expected content
// revision. A write whose readback was misjudged (pre-#821) left the factory's
// own draft on the issue without recording it, so every later write saw "owner
// edits". The owner's retry confirms there are none.
func (r *planGraphRuntime) rebaseOwnerConfirmedContent(ctx context.Context, key string) error {
	parent := r.parentRef.String()
	d, err := r.opts.DB.GetPlanStoryDraft(ctx, parent, key)
	if err != nil || d.Retired || d.IssueID == 0 {
		return err
	}
	forge, err := r.storyForge()
	if err != nil {
		return err
	}
	child, err := forge.GetIssue(ctx, github.Repo{Owner: r.parentRef.Owner, Name: r.parentRef.Repo}, d.IssueNumber)
	if err != nil {
		return err
	}
	if child.ID != d.IssueID || child.State != github.StateOpen {
		return nil
	}
	observed := PlanIssueRevision(child.Title, child.Body)
	if observed == d.Revision {
		return nil
	}
	return r.opts.DB.RebasePlanStoryRevision(ctx, parent, key, d.Revision, observed)
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
			reason = planNoticeGuidance(s, item)
			if command := currentPlanNoticeCommand(s, item); command != "" {
				reason += " The current owner command on this story is `" + command + "`."
			}
		case !s.UpstreamApproved(item.Key, item.Stage):
			reason = "This story needs approval of its upstream stories before its next stage can run."
		default:
			reason = "This story has no eligible next stage; its current stage and earlier approvals must be settled first."
		}
		break
	}
	return &PlanReplyRefusal{Reason: reason, Err: errors.New(reason)}
}

// storyRewindReason uses the immutable prior effect request, never candidate
// labels, to explain an earlier stage whose review basis really changed.
func (r *planGraphRuntime) storyRewindReason(ctx context.Context, s planbrief.PlanState, item planbrief.PlanStoryState) (string, error) {
	if item.EffectRequest == "" {
		return "", nil
	}
	candidate, err := r.opts.DB.GetPlanStoryCandidate(ctx, r.parentRef.String(), item.EffectRequest)
	if err != nil {
		return "", err
	}
	prior, err := graphexec.ParsePlanStoryRequest([]byte(candidate.RequestJSON))
	if err != nil {
		return "", err
	}
	basis := ""
	if prior.SharedRevision != s.SharedRevision {
		basis = "shared decisions changed"
	}
	if basis == "" && (!slices.Equal(prior.Draft.Dependencies.StoryKeys, item.Draft.Dependencies.StoryKeys) || !slices.Equal(prior.Draft.Dependencies.IssueRefs, item.Draft.Dependencies.IssueRefs)) {
		basis = "story dependencies changed"
	}
	if basis == "" {
		complete, err := r.opts.DB.PlanStoryCompleted(ctx, r.parentRef.String(), item.EffectRequest)
		if err != nil {
			return "", err
		}
		for _, stage := range []string{"story", "technical", "acceptance"} {
			// An unconfirmed draft may contain new content at its failed stage;
			// that alone is not a change to an earlier approved stage.
			if stage == prior.Stage && !complete {
				break
			}
			if prior.Draft.StageRevision(stage) != item.Draft.StageRevision(stage) {
				basis = stage + " content changed"
				break
			}
			if stage == prior.Stage {
				break
			}
		}
	}
	if basis == "" {
		return "", nil
	}
	return fmt.Sprintf("Review resumes at the %s stage because %s. Approvals from that stage must be renewed.", item.Stage, basis), nil
}

// planPickSnapshot keeps what a later review compares against when this
// request's own result moves the story back to an earlier stage.
type planPickSnapshot struct {
	known bool
	deps  planbrief.DependencyContent
	// description is the story-stage revision without dependencies, so a
	// description change and a dependency change can be told apart.
	description string
	shared      string
}

// setPick records the selected story and stage together with the snapshot
// read from the same Plan state, so all of them always come from one pick.
func (r *planGraphRuntime) setPick(s planbrief.PlanState, key, stage string) {
	r.selected, r.stage, r.picked = key, stage, planPickSnapshot{}
	for _, item := range s.Stories {
		if key != "" && item.Key == key {
			r.picked = planPickSnapshot{known: true, deps: planbrief.DependencyContent{StoryKeys: slices.Clone(item.Draft.Dependencies.StoryKeys), IssueRefs: slices.Clone(item.Draft.Dependencies.IssueRefs)}, description: storyDescriptionRevision(item.Draft), shared: s.SharedRevision}
			return
		}
	}
}

func storyDescriptionRevision(d planbrief.PlanDraft) string {
	d.Dependencies = planbrief.DependencyContent{}
	return d.StageRevision("story")
}
