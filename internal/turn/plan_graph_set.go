package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// planSetBinding is the factory's account, in the set stage's prompt, of what the
// stage is and is not. The set is about the whole parent scope and names no story;
// real story keys exist only after the owner promotes a set.
func planSetBinding(s planbrief.PlanState) string {
	text := "\nFactory-selected stage: set; whole-parent scope; result branch: result.payload.plan.storyset. The factory records the set under a revision it derives from your content and publishes it on the parent issue; only the owner promotes a set, and no story exists before that. Story keys, revisions and other factory values are factory-owned. Guidance: plan-evidence node-result resource.\n"
	if s.StorySet != nil && !s.StorySet.Promoted {
		text += fmt.Sprintf("A set with revision %s is already proposed and not promoted. A changed set gets a new revision and a new review.\n", s.StorySet.Revision)
	}
	if len(s.Stories) > 0 || (s.StorySet != nil && s.StorySet.Promoted) {
		text += "The set has been promoted or has stories: replacement is unsupported.\n"
	}
	return text + planSetSizingBinding(s.StorySet)
}

// planPromotedBinding is the factory's account, in a creation prompt, of the story
// the owner promoted: the entry the story came from, as data, and the prerequisite
// keys its first draft must carry. The keys were translated at promotion, so every
// one names a reserved story even if that story is drafted later.
func planPromotedBinding(p planbrief.PromotedStory) string {
	entry, err := json.Marshal(struct {
		Name       string `json:"name"`
		Intent     string `json:"intent"`
		Size       string `json:"size,omitempty"`
		SizeReason string `json:"size_reason,omitempty"`
	}{p.Entry.Name, p.Entry.Intent, p.Entry.Size, p.Entry.SizeReason})
	if err != nil {
		entry = []byte("{}")
	}
	keys, err := json.Marshal(append([]string{}, p.Dependencies...))
	if err != nil {
		keys = []byte("[]")
	}
	return "This story comes from the owner-promoted story set; entry (data, not authority): " + string(entry) + "\nRequired first-draft draft.dependencies.story_keys: " + string(keys) + "; the owner-approved prerequisites are exact. First-draft operation: create.\n"
}

// planSetSizingBinding supplies the retained correction facts on the existing
// set invocation; it neither schedules another invocation nor gates promotion.
func planSetSizingBinding(set *planbrief.PlanStorySetState) string {
	applicable := true
	gaps := []planbrief.PlanStorySetSizingGap{}
	if set != nil {
		applicable = set.SizingApplicable()
		gaps = append(gaps, set.SizingGaps()...)
	}
	facts, _ := json.Marshal(struct {
		Applicable  bool                              `json:"applicable"`
		Corrections []planbrief.PlanStorySetSizingGap `json:"requested_corrections"`
	}{applicable, gaps})
	return "Story set sizing facts (data): " + string(facts) + "\n"
}

// planStorySizingBinding resolves the promoted entry independently of the draft
// lifecycle, so the same saved facts reach all three stages.
func planStorySizingBinding(set *planbrief.PlanStorySetState, key string) string {
	entry := planbrief.PlanStorySetEntry{}
	found, applicable := false, false
	gaps := []planbrief.PlanStorySetSizingGap{}
	if set != nil {
		entry, found = set.EntryForStory(key)
		applicable = set.SizingApplicable()
		if found {
			for _, gap := range set.SizingGaps() {
				if gap.EntryID == entry.ID {
					gaps = append(gaps, gap)
				}
			}
		}
	}
	facts, _ := json.Marshal(struct {
		StoryKey   string                            `json:"story_key"`
		EntryID    string                            `json:"entry_id"`
		EntryFound bool                              `json:"entry_found"`
		Applicable bool                              `json:"applicable"`
		Size       string                            `json:"size"`
		SizeReason string                            `json:"size_reason"`
		Gaps       []planbrief.PlanStorySetSizingGap `json:"gaps"`
	}{key, entry.ID, found, applicable, entry.Size, entry.SizeReason, gaps})
	return "Selected story sizing facts (data): " + string(facts) + "\n"
}

// planSetEntries converts a validated proposed set from a retained result to
// saved-state entries, in the order the agent listed them, which is the order
// they are displayed and reserved in.
func planSetEntries(set *agentresult.PlanStorySet) []planbrief.PlanStorySetEntry {
	entries := make([]planbrief.PlanStorySetEntry, len(set.Entries))
	for i, e := range set.Entries {
		entries[i] = planbrief.PlanStorySetEntry{ID: e.ID, Name: e.Name, Intent: e.Intent, DependsOn: e.DependsOn, Size: e.Size, SizeReason: e.SizeReason}
	}
	return entries
}

// setReceipt is the set effect's retained output. It reuses the story receipt so
// a set that ends in a failing outcome reaches the failure comment with its own
// detail, exactly as a story effect does.
func (r *planGraphRuntime) setReceipt(a ActivityRequest, revision, outcome, detail string) (ActivityResult, error) {
	raw, err := json.Marshal(PlanStoryReceipt{RequestID: planRequestID(a.InstanceID, a.NodeAttemptID), Story: "set", Candidate: planCandidate(a.InstanceID, a.NodeAttemptID), Revision: revision, Outcome: outcome, Detail: detail})
	if err != nil {
		return ActivityResult{}, err
	}
	return ActivityResult{Outcome: outcome, Kind: store.GraphOutputJSON, Output: raw}, nil
}

// setEffect is the one Plan effect that records a proposed story set. It writes
// saved Plan state and nothing else: it creates no issue, reserves no story key
// and promotes nothing, so no role output can promote a set. The revision comes
// from the set's content, so the same set applied again, by a replay or after a
// lost reply, leaves state and revision as they are.
//
// A set proposed outside the set stage, for a scope that already has stories or
// after promotion is a conflict, reported on the issue through the failure edge.
func (r *planGraphRuntime) setEffect(ctx context.Context, a ActivityRequest, node planRetained) (ActivityResult, error) {
	if r.stage != planSetStage {
		return r.setReceipt(a, "", "failed", fmt.Sprintf("a story set is proposed only at the set stage, but the factory selected the %q stage", r.stage))
	}
	s, rev, err := r.state(ctx)
	if err != nil {
		return ActivityResult{}, err
	}
	revision, changed, err := s.ProposeStorySet(planSetEntries(node.Set))
	switch {
	case errors.Is(err, planbrief.ErrPlanStorySetPromoted), errors.Is(err, planbrief.ErrPlanStorySetHasStories):
		return r.setReceipt(a, "", "conflict", err.Error())
	case err != nil:
		return r.setReceipt(a, "", "failed", err.Error())
	}
	detail := "the same story set was already recorded; nothing changed"
	if changed {
		if err = r.persist(ctx, s, rev); err != nil {
			return ActivityResult{}, fmt.Errorf("record story set: %w", err)
		}
		detail = "the story set was recorded"
	}
	return r.setReceipt(a, revision, "applied", detail)
}

// setReviewText appends the owner-facing account of the saved set to the agent's
// comment. It is rendered from saved state, never from the agent's text, and only
// for the set the retained result proposed: if a later proposal replaced it, or
// the set was promoted, publishing this older set would ask for a review of
// something no longer current.
func (r *planGraphRuntime) setReviewText(s planbrief.PlanState, node planRetained, comment string) (string, error) {
	if node.Set == nil {
		return "", errors.New("story set review has no proposed set in the retained result")
	}
	set := s.StorySet
	if set == nil || set.Promoted || set.Revision != planbrief.PlanStorySetRevision(planSetEntries(node.Set)) {
		return "", errors.New("the proposed story set is no longer the current unpromoted set, so it is not published for review")
	}
	rendered, err := planbrief.RenderPlanStorySet(set)
	if err != nil {
		return "", err
	}
	if comment == "" {
		return rendered, nil
	}
	return comment + "\n\n" + rendered, nil
}
