package turn

import (
	"encoding/json"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestPlanRecoveryBoundaryRestartCreateAndUpdate(t *testing.T) {
	for _, phase := range []string{"create", "attach", "update"} {
		t.Run(phase, func(t *testing.T) {
			h, f, a, r, restart := planBoundarySetup(t)
			if phase == "update" {
				first := planApply(t, h, a, r, "applied")
				r.Operation = "update"
				r.RequestID = "update-recovery"
				r.Candidate = "new-candidate"
				a.Candidate = r.Candidate
				r.ExpectedRevision = first.Revision
				r.ChangedSections = []string{"intent"}
				r.Draft.Problem = "Updated intent"
				f.failUpdate = true
			} else {
				f.failCreate = phase == "create"
				f.failAttach = phase == "attach"
			}
			planApplySharedFailure(t, h, a, r)
			restart()
			if err := h.Reconcile(t.Context(), a, 1); err != nil {
				t.Fatal(err)
			}
			if err := h.Reconcile(t.Context(), a, 1); err != nil {
				t.Fatal(err)
			}
			complete, err := h.DB.PlanStoryCompleted(t.Context(), a.Parent, r.RequestID)
			if err != nil || !complete || f.creates != 1 || (phase == "update" && f.patches != 1) {
				t.Fatalf("completion=%v err=%v creates=%d patches=%d", complete, err, f.creates, f.patches)
			}
			state, _, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
			if err != nil || len(state.Stories) != 1 || state.Stories[0].Effect != "applied" || state.Stories[0].EffectHold != "" {
				t.Fatalf("workflow %+v %v", state, err)
			}
		})
	}
}

func TestPlanRecoveryBoundaryUnknownCreationFairAcrossRestart(t *testing.T) {
	h, f, a, r, restart := planBoundarySetup(t)
	f.failCreate = true
	planApplySharedFailure(t, h, a, r)
	f.hidden = true
	// Add a later historical retirement debt. Neither can be selected for shaping,
	// but both must receive a recovery decision across a one-item budget restart.
	other := r
	other.RequestID = "z-retire"
	other.Operation = "retire"
	other.ExpectedRevision = "0123456789012345678901234567890123456789012345678901234567890123"
	other.Draft.Key = "held-two"
	if _, err := h.DB.ReservePlanStory(t.Context(), a.Parent, other.Draft.Key); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.DB.PutPlanStoryCandidate(t.Context(), store.PlanStoryCandidate{Parent: a.Parent, RequestID: other.RequestID, Key: other.Draft.Key, Candidate: other.Candidate, RequestJSON: string(raw)}); err != nil {
		t.Fatal(err)
	}
	if err = h.Reconcile(t.Context(), a, 1); err != nil {
		t.Fatal(err)
	}
	restart()
	if err = h.Reconcile(t.Context(), a, 1); err != nil {
		t.Fatal(err)
	}
	state, _, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]bool{}
	for _, item := range state.Stories {
		held[item.Key] = item.EffectHold != ""
	}
	if !held[r.Draft.Key] || !held[other.Draft.Key] || f.creates != 1 {
		t.Fatalf("held=%v creates=%d", held, f.creates)
	}
}

func TestPlanRecoveryBoundaryRetirementGapsAndLaterChanges(t *testing.T) {
	for _, kind := range []string{"remote-gap", "local-gap", "reopened", "reparented", "identity", "owner"} {
		t.Run(kind, func(t *testing.T) {
			h, f, a, r, restart := planBoundarySetup(t)
			created := planApply(t, h, a, r, "applied")
			r.RequestID = "retire-recovery"
			r.Operation = "retire"
			r.ExpectedRevision = created.Revision
			r.Candidate = "retired-candidate"
			a.Candidate = r.Candidate
			a.OwnerID = 10
			a.ActorID = 10
			a.RetirementCandidate = "candidate-one"
			a.RetirementRevision = created.Revision
			planAuthorizeRetirement(t, h, &a, r)
			f.failRetire = true
			planApplySharedFailure(t, h, a, r)
			if kind == "local-gap" {
				d, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
				if err != nil {
					t.Fatal(err)
				}
				d.Retired = true
				d.Candidate = r.Candidate
				if err = h.DB.ApplyPlanStoryDraft(t.Context(), d, d.Revision, store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Phase: "retire", Outcome: "applied", IssueID: d.IssueID, IssueNumber: d.IssueNumber, Revision: d.Revision}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "reopened" {
				f.child["state"] = "open"
				f.child["state_reason"] = ""
				if err := h.DB.ObservePlanStoryLifecycle(t.Context(), a.Parent, a.SelectedKey, planbrief.PlanLifecycle{State: "closed", Reason: "not_planned", Revision: created.Revision}, ""); err != nil {
					t.Fatal(err)
				}
				if err := h.DB.ObservePlanStoryLifecycle(t.Context(), a.Parent, a.SelectedKey, planbrief.PlanLifecycle{State: "open", Revision: created.Revision}, ""); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "reparented" {
				f.reparent = true
			}
			if kind == "identity" {
				f.child["id"] = int64(7777)
			}
			if kind == "owner" {
				a.OwnerID = 20
			}
			restart()
			err := h.Reconcile(t.Context(), a, 8)
			if kind == "owner" {
				if err == nil {
					t.Fatal("changed shared owner authority concealed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			state, _, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
			if err != nil {
				t.Fatal(err)
			}
			success := kind == "remote-gap" || kind == "local-gap"
			if state.Stories[0].Retired != success || (success && len(state.ConsumedReplies) != 1) || (!success && state.Stories[0].EffectHold == "") {
				t.Fatalf("state %+v", state)
			}
			if err = h.Reconcile(t.Context(), a, 8); err != nil {
				t.Fatal(err)
			}
			if f.patches != 1 {
				t.Fatalf("duplicate retirement: %d", f.patches)
			}
		})
	}
}

func TestPlanRecoveryBoundaryStaleCompletionRollsBack(t *testing.T) {
	h, _, a, r, _ := planBoundarySetup(t)
	planApply(t, h, a, r, "applied")
	draft, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
	if err != nil {
		t.Fatal(err)
	}
	state, revision, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
	if err != nil {
		t.Fatal(err)
	}
	state.SharedRevision = "new-owner-decision"
	if _, err = h.DB.SavePlanWorkflowState(t.Context(), a.Parent, revision, state); err != nil {
		t.Fatal(err)
	}
	changed := draft
	changed.Retired = true
	err = h.DB.CompletePlanStoryEffect(t.Context(), changed, draft.Revision, store.PlanStoryEffect{Parent: a.Parent, RequestID: r.RequestID, Outcome: "applied"}, state, revision, false)
	if err == nil {
		t.Fatal("stale workflow completion accepted")
	}
	retained, err := h.DB.GetPlanStoryDraft(t.Context(), a.Parent, a.SelectedKey)
	if err != nil || retained.Retired {
		t.Fatalf("draft write escaped rollback %+v %v", retained, err)
	}
	current, _, err := h.DB.GetPlanWorkflowState(t.Context(), a.Parent)
	if err != nil || current.SharedRevision != "new-owner-decision" {
		t.Fatalf("new state overwritten %+v %v", current, err)
	}
}
