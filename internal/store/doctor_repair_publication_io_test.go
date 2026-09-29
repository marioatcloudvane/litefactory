package store

import (
	"fmt"
	"reflect"
	"testing"
)

func TestDoctorRepairOutcomeReopenCompletesWithoutWorkflowCursorEffects(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	sibling := testSession(t, db, 407)
	if _, _, err := db.BootstrapWatermark(ctx, session.ID, 41); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BootstrapWatermark(ctx, sibling.ID, 81); err != nil {
		t.Fatal(err)
	}
	_, presentation := retainedPollProposal(t, db, session)
	input := doctorInboxInput(Repo{ID: session.RepoID}, 406, 200, "apply")
	input.TaskRef = session.TaskRef
	// This fixture represents the factory-authenticated source. Exact GitHub
	// presentation/approval admission is covered separately by the catalog test.
	command, err := db.AdmitDoctorCommand(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	operation, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "outcome-repair", SourceID: fmt.Sprintf("github-doctor-apply:%d", command.ID), Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, operation.ID, "old-instance"); err != nil {
		t.Fatal(err)
	}
	if err = db.BackupDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.ApplyDoctorPoll(ctx, operation.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.CommitDoctorRestart(ctx, operation.ID, "outcome-token", "Rows verified; backups retained"); err != nil {
		t.Fatal(err)
	}
	intent, err := db.ClaimDoctorRestart(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorRestart(ctx, operation.ID, intent.Token, "new-instance"); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	siblingBefore, err := db.GetSession(ctx, sibling.ID)
	if err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	targets, err := reopened.DoctorRepairRecoveryTargets(ctx)
	if err != nil || len(targets) != 1 || targets[0] != session.TaskRef {
		t.Fatalf("outcome debt missing after reopen: %v %v", targets, err)
	}
	parts := []string{"Verified rows and retained backup references", "Restart confirmed; no unresolved findings"}
	if err = reopened.EnsureDoctorRepairOutcome(ctx, operation.ID, parts, 99); err != nil {
		t.Fatal(err)
	}
	if err = reopened.EnsureDoctorRepairOutcome(ctx, operation.ID, parts, 99); err != nil {
		t.Fatal(err)
	}
	deliveries, err := reopened.PendingDoctorDeliveries(ctx)
	if err != nil || len(deliveries) != 2 {
		t.Fatalf("duplicate or missing outcome: %+v %v", deliveries, err)
	}
	for i, delivery := range deliveries {
		if delivery.Destination != session.TaskRef || delivery.Task != session.TaskRef {
			t.Fatalf("outcome redirected: %+v", delivery)
		}
		if _, err = reopened.MarkReportDeliverySent(ctx, delivery.Key); err != nil {
			t.Fatal(err)
		}
		if _, err = reopened.ConfirmReportDelivery(ctx, delivery.Key, int64(900+i)); err != nil {
			t.Fatal(err)
		}
		if err = reopened.CompleteDoctorDelivery(ctx, delivery.Key); err != nil {
			t.Fatal(err)
		}
	}
	after, err := reopened.GetSession(ctx, session.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("publication altered workflow: %+v %+v %v", before, after, err)
	}
	siblingAfter, err := reopened.GetSession(ctx, sibling.ID)
	if err != nil || !reflect.DeepEqual(siblingBefore, siblingAfter) {
		t.Fatalf("publication altered sibling: %+v %+v %v", siblingBefore, siblingAfter, err)
	}
	for _, check := range []struct{ id, watermark int64 }{{session.ID, 41}, {sibling.ID, 81}} {
		wm, ok, err := reopened.GetWatermark(ctx, check.id)
		if err != nil || !ok || wm.LastCommentID != check.watermark {
			t.Fatalf("cursor advanced: %+v %v", wm, err)
		}
	}
	if _, err = reopened.ClaimDoctorRestart(ctx, operation.ID); err == nil {
		t.Fatal("publication authorized restart replay")
	}
	targets, err = reopened.DoctorRepairRecoveryTargets(ctx)
	if err != nil || len(targets) != 0 {
		t.Fatalf("completed outcome recovery replayed: %v %v", targets, err)
	}
}
