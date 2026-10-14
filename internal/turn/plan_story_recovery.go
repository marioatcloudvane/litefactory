package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

// ErrPlanStoryHeld describes a recorded story-local authority/content conflict.
// Storage, parent-authority and service failures are deliberately not wrapped.
var ErrPlanStoryHeld = errors.New("plan story held")

func heldPlanStory(reason string) error { return fmt.Errorf("%w: %s", ErrPlanStoryHeld, reason) }

// Reconcile revisits original requests independently of shaping selection. Its
// caller owns scheduling; each pass has a durable bounded rotating budget.
func (h PlanStoryActivity) Reconcile(ctx context.Context, a PlanStoryAuthority, budget int) error {
	return h.reconcile(ctx, a, budget, "")
}

// ReconcileStory is [PlanStoryActivity.Reconcile] for one story's requests only,
// which is all a story task reconciles: a sibling's debt is its own task's.
func (h PlanStoryActivity) ReconcileStory(ctx context.Context, a PlanStoryAuthority, budget int, key string) error {
	return h.reconcile(ctx, a, budget, key)
}

func (h PlanStoryActivity) reconcile(ctx context.Context, a PlanStoryAuthority, budget int, only string) error {
	debt, err := h.DB.PlanStoryRecoveryDebt(ctx, a.Parent, budget)
	if err != nil {
		return err
	}
	for _, c := range debt {
		// A story task reconciles its own story's effects only.
		if only != "" && c.Key != only {
			continue
		}
		if err = h.DB.AdvancePlanStoryRecovery(ctx, a.Parent, c.RequestID); err != nil {
			return err
		}
		request, err := graphexec.ParsePlanStoryRequest([]byte(c.RequestJSON))
		if err != nil {
			return err
		}
		authority := a
		authority.SelectedKey = c.Key
		authority.Candidate = c.Candidate
		authority.Stage = request.Stage
		authority.Recovery = true
		raw, loadErr := h.DB.PlanStoryAuthority(ctx, c.Parent, c.RequestID)
		if errors.Is(loadErr, store.ErrNotFound) {
			effect, effectErr := h.DB.LatestPlanStoryEffect(ctx, c.Parent, c.RequestID)
			if effectErr != nil && !errors.Is(effectErr, store.ErrNotFound) {
				return effectErr
			}
			if request.Operation == "retire" || effectErr != nil || effect.Outcome != "applied" {
				if err = h.hold(ctx, c, request, "Original request authority is missing; review its retained identity and confirmation before recovery. Closure alone cannot settle it."); err != nil {
					return err
				}
				continue
			}
			// An already-applied non-retirement needs only verified local
			// workflow repair, never a new remote mutation or owner approval.
		} else {
			if loadErr != nil {
				return loadErr
			}
			if err = json.Unmarshal([]byte(raw), &authority); err != nil {
				return err
			}
			if authority.Parent != a.Parent || authority.ParentID != a.ParentID || authority.ParentNumber != a.ParentNumber || authority.Repo != a.Repo || (authority.OwnerID > 0 && authority.OwnerID != a.OwnerID) {
				return errors.New("retained Plan parent or owner authority changed")
			}
			authority.Recovery = true
		}

		receipt, applyErr := h.Apply(ctx, authority, request)
		if applyErr != nil && !errors.Is(applyErr, ErrPlanStoryHeld) {
			return applyErr
		}
		if applyErr != nil || receipt.Outcome != "applied" {
			settled, settleErr := h.supersedeKnownIntent(ctx, c, request, applyErr == nil && receipt.Outcome == "failed")
			if settleErr != nil {
				return settleErr
			}
			if settled {
				continue
			}
			detail := receipt.Detail
			if applyErr != nil {
				detail = applyErr.Error()
			}
			if detail == "" {
				detail = "Original request remains unresolved; verify the bound issue and retained request."
			}
			if err = h.hold(ctx, c, request, detail); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h PlanStoryActivity) hold(ctx context.Context, c store.PlanStoryCandidate, r graphexec.PlanStoryRequest, reason string) error {
	return h.DB.UpdatePlanWorkflowState(ctx, c.Parent, func(s *planbrief.PlanState) (bool, error) {
		if s.SharedRevision == "" {
			s.SharedRevision = "initial"
		}
		found := false
		for i := range s.Stories {
			item := &s.Stories[i]
			if item.Key != c.Key {
				continue
			}
			found = true
			if item.EffectHolds[c.RequestID] == reason || (item.EffectRequest == c.RequestID && item.EffectHold == reason) {
				return false, nil
			}
			item.SetEffectHold(c.RequestID, reason)
		}
		if !found {
			s.Stories = append(s.Stories, planbrief.PlanStoryState{Key: c.Key, Candidate: c.Candidate, Stage: r.Stage, Draft: r.Draft, Effect: "uncertain", EffectRequest: c.RequestID, EffectHold: reason, EffectHolds: map[string]string{c.RequestID: reason}})
		}
		s.AssembledApproval = ""
		s.AssembledWait = ""
		s.PublishedRevision = ""
		s.SavedRevision = ""
		return true, nil
	})
}

func (h PlanStoryActivity) retainRetirement(ctx context.Context, a *PlanStoryAuthority, r graphexec.PlanStoryRequest, d store.PlanStoryDraft) error {
	raw, err := h.DB.PlanStoryAuthority(ctx, a.Parent, r.RequestID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	state, _, loadErr := h.DB.GetPlanWorkflowState(ctx, a.Parent)
	if loadErr != nil {
		return loadErr
	}
	if errors.Is(err, store.ErrNotFound) {
		if a.Recovery || a.Confirmation == nil {
			return heldPlanStory("original owner confirmation unavailable")
		}
		if err = state.ConfirmRetirement(*a.Confirmation); err != nil {
			return heldPlanStory(err.Error())
		}
		for _, item := range state.Stories {
			if item.Key == d.Key && item.Wait != nil {
				a.ReviewRevision = item.Wait.Revision
			}
		}
		if a.Confirmation.OwnerID != a.OwnerID || a.Confirmation.ActorID != a.ActorID || a.Confirmation.IssueID != d.IssueID {
			return heldPlanStory("retirement confirmation identity changed")
		}
		retained := *a
		retained.Recovery = false
		data, marshalErr := json.Marshal(retained)
		if marshalErr != nil {
			return marshalErr
		}
		if err = h.DB.RetainPlanStoryAuthority(ctx, a.Parent, r.RequestID, string(data)); err != nil {
			return err
		}
	} else {
		var original PlanStoryAuthority
		if err = json.Unmarshal([]byte(raw), &original); err != nil {
			return err
		}
		original.Recovery = a.Recovery
		// The stored authentication cannot be replaced by a duplicate owner's input.
		if original.ParentID != a.ParentID || original.OwnerID != a.OwnerID || original.Candidate != a.Candidate || original.RetirementRevision != d.Revision {
			return heldPlanStory("retained retirement authority changed")
		}
		*a = original
	}
	if err = validateRetirementReview(state, *a, r, d); err != nil {
		return err
	}
	for _, key := range r.Replacements {
		replacement, loadErr := h.DB.GetPlanStoryDraft(ctx, a.Parent, key)
		if loadErr != nil {
			return loadErr
		}
		valid := false
		for _, item := range state.Stories {
			if item.Key == key {
				valid = item.Active() && !item.Blocked()
			}
		}
		if !valid || replacement.Retired || replacement.IssueID < 1 {
			return heldPlanStory("retirement replacement unavailable")
		}
		child, readErr := h.Forge.GetIssue(ctx, a.Repo, replacement.IssueNumber)
		if readErr != nil {
			if errors.Is(readErr, github.ErrNotFound) {
				return heldPlanStory("retirement replacement missing")
			}
			return readErr
		}
		parent, readErr := h.Forge.GetParentIssue(ctx, a.Repo, replacement.IssueNumber)
		if readErr != nil {
			if errors.Is(readErr, github.ErrNotFound) {
				return heldPlanStory("retirement replacement parent unavailable")
			}
			return readErr
		}
		if child.ID != replacement.IssueID || child.State != github.StateOpen || parent.ID != a.ParentID || parent.Number != a.ParentNumber || PlanIssueRevision(child.Title, child.Body) != replacement.Revision {
			return heldPlanStory("retirement replacement identity, revision or parent changed")
		}
	}
	return nil
}

func (h PlanStoryActivity) prepareCompletion(ctx context.Context, a PlanStoryAuthority, r graphexec.PlanStoryRequest, draft planbrief.PlanDraft) error {
	return h.DB.UpdatePlanWorkflowState(ctx, a.Parent, func(state *planbrief.PlanState) (bool, error) {
		if state.SharedRevision == "" {
			state.SharedRevision = "initial"
		}
		found := false
		for i := range state.Stories {
			item := &state.Stories[i]
			if item.Key == r.Draft.Key {
				found = true
				item.Effect = "pending"
				if len(item.EffectHolds) == 0 && item.EffectHold == "" {
					item.EffectRequest = r.RequestID
				}
			}
		}
		if !found {
			state.Stories = append(state.Stories, planbrief.PlanStoryState{Key: r.Draft.Key, Candidate: r.Candidate, Stage: r.Stage, Draft: draft, Effect: "pending", EffectRequest: r.RequestID})
		}
		state.AssembledApproval = ""
		state.AssembledWait = ""
		state.PublishedRevision = ""
		state.SavedRevision = ""
		return true, nil
	})
}

func completePlanState(state *planbrief.PlanState, a PlanStoryAuthority, r graphexec.PlanStoryRequest, d store.PlanStoryDraft, child github.Issue) error {
	for i := range state.Stories {
		item := &state.Stories[i]
		if item.Key == d.Key {
			item.Effect = "applied"
			item.ClearEffectHold(r.RequestID)
		}
	}
	if r.Operation == "retire" {
		if a.Confirmation == nil {
			return heldPlanStory("retirement completion lacks owner confirmation")
		}
		if !slices.Contains(state.ConsumedReplies, a.Confirmation.ID) {
			if err := state.MarkRetired(d.Key, r.Replacements, a.Confirmation.ID); err != nil {
				return heldPlanStory(err.Error())
			}
		}
	} else {
		var draft planbrief.PlanDraft
		if err := json.Unmarshal([]byte(d.DraftJSON), &draft); err != nil {
			return err
		}
		if err := state.PutCandidate(draft, r.Stage, r.Candidate); err != nil {
			return heldPlanStory(err.Error())
		}
	}
	for i := range state.Stories {
		if state.Stories[i].Key == d.Key {
			state.Stories[i].ClearEffectHold(r.RequestID)
		}
	}
	_, err := state.ObserveLifecycle(d.Key, planbrief.PlanLifecycle{State: child.State, Reason: child.StateReason, Revision: PlanIssueRevision(child.Title, child.Body)}, "")
	return err
}

func (h PlanStoryActivity) repairCompletion(ctx context.Context, a PlanStoryAuthority, r graphexec.PlanStoryRequest, d store.PlanStoryDraft, e store.PlanStoryEffect) error {
	complete, err := h.DB.PlanStoryCompleted(ctx, a.Parent, r.RequestID)
	if err != nil || complete {
		return err
	}
	if d.Candidate != r.Candidate || d.Revision != e.Revision {
		if r.Operation == "retire" {
			return heldPlanStory("historical retirement completion changed")
		}
		state, revision, loadErr := h.DB.GetPlanWorkflowState(ctx, a.Parent)
		if loadErr != nil {
			return loadErr
		}
		for i := range state.Stories {
			item := &state.Stories[i]
			if item.Key == d.Key {
				if item.EffectRequest == r.RequestID {
					item.Effect = "applied"
				}
				item.ClearEffectHold(r.RequestID)
			}
		}
		return h.DB.SettlePlanStoryDebt(ctx, a.Parent, r.RequestID, state, revision, true)
	}
	if r.Operation == "retire" {
		raw, loadErr := h.DB.PlanStoryAuthority(ctx, a.Parent, r.RequestID)
		if errors.Is(loadErr, store.ErrNotFound) {
			return heldPlanStory("historical retirement lacks original confirmation")
		}
		if loadErr != nil {
			return loadErr
		}
		var original PlanStoryAuthority
		if err = json.Unmarshal([]byte(raw), &original); err != nil {
			return err
		}
		if original.ParentID != a.ParentID || original.Parent != a.Parent || original.Repo != a.Repo || original.OwnerID != a.OwnerID {
			return errors.New("retirement completion authority changed")
		}
		a = original
		a.Recovery = true
		if err = h.retainRetirement(ctx, &a, r, d); err != nil {
			return err
		}
	}
	state, revision, err := h.DB.GetPlanWorkflowState(ctx, a.Parent)
	if err != nil {
		return err
	}
	if state.SharedRevision == "" {
		state.SharedRevision = "initial"
	}
	child, err := h.Forge.GetIssue(ctx, a.Repo, d.IssueNumber)
	if err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return heldPlanStory("historical completion child unavailable")
		}
		return err
	}
	parent, err := h.Forge.GetParentIssue(ctx, a.Repo, d.IssueNumber)
	if err != nil {
		if errors.Is(err, github.ErrNotFound) {
			return heldPlanStory("historical completion parent unavailable")
		}
		return err
	}
	if child.ID != d.IssueID || parent.ID != a.ParentID || parent.Number != a.ParentNumber || !strings.HasPrefix(child.Body, planStoryMarker(a, d.Key)+"\n") || PlanIssueRevision(child.Title, child.Body) != d.Revision {
		return heldPlanStory("historical completion target changed")
	}
	if r.Operation == "retire" && (child.State != "closed" || child.StateReason != "not_planned") {
		return heldPlanStory("historical retirement lifecycle changed")
	}
	if err = completePlanState(&state, a, r, d, child); err != nil {
		return err
	}
	return h.DB.CompletePlanStoryEffect(ctx, d, d.Revision, e, state, revision, true)
}

// validateRetirementReview rechecks the original review basis without treating
// externally observed closure as fresh approval. It does not change its input.
func validateRetirementReview(state planbrief.PlanState, a PlanStoryAuthority, r graphexec.PlanStoryRequest, d store.PlanStoryDraft) error {
	state.Stories = slices.Clone(state.Stories)
	if a.Confirmation == nil || a.ReviewRevision == "" {
		return heldPlanStory("retirement lacks retained proposal and wait")
	}
	for i := range state.Stories {
		item := &state.Stories[i]
		if item.Key != d.Key {
			continue
		}
		if item.Lifecycle.Reopened {
			return heldPlanStory("a later reopen prohibits replay of the old retirement")
		}
		if item.LifecycleHold != "" {
			return heldPlanStory(item.LifecycleHold)
		}
		if item.Candidate != a.RetirementCandidate {
			return heldPlanStory("retirement candidate changed")
		}
		// Closure can invalidate approval presentation, but cannot erase the exact
		// original review basis. Clear only this request's local recovery debt while
		// recomputing that basis; dependency holds and all content remain checked.
		item.Lifecycle = planbrief.PlanLifecycle{}
		if item.EffectRequest == "" && item.Effect == r.RequestID {
			item.EffectRequest = r.RequestID
		}
		item.Effect = ""
		item.ClearEffectHold(r.RequestID)
	}
	review, err := state.ReviewRevision(d.Key, r.Stage)
	if err != nil || review != a.ReviewRevision {
		return heldPlanStory("retirement dependency or shared review changed")
	}
	return nil
}

// supersedeKnownIntent settles obsolete intent only when no mutation could have
// occurred. A retained unknown remote outcome survives any newer lifecycle. A
// failed request, such as a draft too large to render, can never publish, so it
// is obsolete as soon as no mutation is on record; a new attempt replaces it.
func (h PlanStoryActivity) supersedeKnownIntent(ctx context.Context, c store.PlanStoryCandidate, r graphexec.PlanStoryRequest, failed bool) (bool, error) {
	for _, phase := range []string{"create", "attach", "update", "retire"} {
		mutated, err := h.DB.HasPlanStoryMutation(ctx, c.Parent, c.RequestID, phase)
		if err != nil {
			return false, err
		}
		if mutated {
			return false, nil
		}
	}
	state, revision, err := h.DB.GetPlanWorkflowState(ctx, c.Parent)
	if err != nil {
		return false, err
	}
	draft, err := h.DB.GetPlanStoryDraft(ctx, c.Parent, c.Key)
	if err != nil {
		return false, err
	}
	obsolete := failed || (r.Operation != "create" && draft.Revision != r.ExpectedRevision)
	for i := range state.Stories {
		item := &state.Stories[i]
		if item.Key != c.Key {
			continue
		}
		obsolete = obsolete || item.Lifecycle.State == "closed"
		if obsolete {
			if item.EffectRequest == r.RequestID {
				item.Effect = ""
			}
			item.ClearEffectHold(r.RequestID)
		}
	}
	if !obsolete {
		return false, nil
	}
	err = h.DB.SettlePlanStoryDebt(ctx, c.Parent, c.RequestID, state, revision, false)
	return err == nil, err
}

// Only a retained content conflict with original authority and update intent can
// be reconsidered. Apply still checks the qualified provider on the fresh child.
func (h PlanStoryActivity) canReverifyContentConflict(ctx context.Context, a PlanStoryAuthority, r graphexec.PlanStoryRequest, d store.PlanStoryDraft, effect store.PlanStoryEffect) (bool, error) {
	if !a.Recovery || r.Operation == "retire" || effect.Outcome != "conflict" || effect.Phase != "update" || effect.Detail != "owner edits conflict with expected content revision" || d.Retired || d.IssueID == 0 || effect.IssueID != d.IssueID || effect.IssueNumber != d.IssueNumber {
		return false, nil
	}
	raw, err := h.DB.PlanStoryAuthority(ctx, a.Parent, r.RequestID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	original := a
	original.Recovery = false
	expected, err := json.Marshal(original)
	if err != nil {
		return false, err
	}
	if raw != string(expected) {
		return false, nil
	}
	return h.DB.HasPlanStoryMutation(ctx, a.Parent, r.RequestID, "update")
}
