package daemon_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/store"
)

// An exact approved repair only removes the stale dispatch. The ordinary owner
// reply still crosses the existing admission boundary and resumes saved Validate.
func TestVisibleStopRecoveryAfterApprovedDispatchRepair(t *testing.T) {
	f, instance, _, calls := stoppedValidateFixture(t, true)
	ctx := t.Context()
	pending, err := f.db.ParkGraphInstance(ctx, instance.ID, instance.CurrentNode, f.clock.Now(), f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Model the stale blocker: a live dispatch remains while its owning instance
	// has no owed continuation. The repair must leave that metadata unchanged.
	if err = f.db.SetGraphInstanceStatus(ctx, instance.ID, store.GraphRunning, time.Time{}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("graph_dispatch:%d", pending.ID)
	action, err := f.db.PrepareDoctorGraphAction(ctx, instanceTask(t, f, instance), "graph_dispatch_cancel", target, "Remove the stale dispatch so the owner may continue saved validation")
	if err != nil {
		t.Fatal(err)
	}
	op := approveStoppedDispatch(t, f, instance, target, action)
	if err = f.db.ApplyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.VerifyDoctorGraphRepair(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.SettleDoctorTypedRepair(ctx, op.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.db.GetGraphInstance(ctx, instance.ID)
	if err != nil || stopped.Status != store.GraphRunning || !stopped.NextDueAt.IsZero() || len(*calls) != 0 {
		t.Fatalf("repair claimed continuation or changed instance: %+v calls=%d %v", stopped, len(*calls), err)
	}
	f.forge.addComment(testIssue, 42, owner(), "Continue saved validation with the retained edits.")
	f.settle()
	latest, err := f.db.LatestGraphDispatch(ctx, instance.ID)
	if err != nil || len(*calls) != 1 || latest.ID == pending.ID || latest.InstanceID != instance.ID || latest.LaunchID != pending.LaunchID || latest.Node != instance.CurrentNode || (*calls)[0].Mode != "validate" {
		t.Fatalf("repair did not enable exactly the saved step: dispatch=%+v calls=%+v %v\n%s", latest, *calls, err, f.logs)
	}
	f.settle()
	again, err := f.db.LatestGraphDispatch(ctx, instance.ID)
	if err != nil || len(*calls) != 1 || again.ID != latest.ID {
		t.Fatalf("reply replay dispatched again: %+v calls=%d %v", again, len(*calls), err)
	}
}

func instanceTask(t *testing.T, f *fixture, instance store.GraphInstance) string {
	t.Helper()
	session, err := f.db.GetSession(t.Context(), instance.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return session.TaskRef
}

func approveStoppedDispatch(t *testing.T, f *fixture, instance store.GraphInstance, target string, action store.DoctorRepairAction) store.DoctorRepair {
	t.Helper()
	ctx := t.Context()
	const id = "visible-stop-repair"
	ref := instanceTask(t, f, instance)
	conversation, err := f.db.BeginDoctorAttempt(ctx, ref, store.DoctorAttempt{ID: id, SourceID: id, OwnerText: "Inspect the stale dispatch", PromptVersion: doctor.PromptVersion, GuideVersion: doctor.GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := json.Marshal(store.DoctorEvidence{TaskRef: ref, SessionID: instance.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: id, Provider: "claude", ConfiguredModel: "fixture", Evidence: string(evidence)}); err != nil {
		t.Fatal(err)
	}
	binding, err := f.db.BindDoctorResultAttempt(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	proposal := []doctor.RepairStep{{Number: 1, Target: target, Precondition: "Exact retained manifest and no active related work", Change: "Cancel the stale dispatch", Reason: "Owner may continue the retained step", Reversibility: "Guarded inverse only before related progress", Rollback: "Separate exact proposal approval", Action: &action}}
	manifest, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(doctor.Diagnosis{Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.RecordResult(ctx, store.ResultRecord{Binding: binding, ExpectedRevision: 0, RequestID: id, Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.AdoptDoctorResult(ctx, binding, id, manifest); err != nil {
		t.Fatal(err)
	}
	proposalID, _, err := f.db.DoctorProposalReference(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.PresentDoctorProposal(ctx, ref, id, proposalID); err != nil {
		t.Fatal(err)
	}
	op, err := f.db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: id, SourceID: id + "-approval", Principal: "local-owner", TaskRef: ref, PresentationID: id})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.AcknowledgeDoctorExclusion(ctx, op.ID, "fixture-drained"); err != nil {
		t.Fatal(err)
	}
	return op
}
