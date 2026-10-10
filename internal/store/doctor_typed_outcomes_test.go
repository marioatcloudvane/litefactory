package store

import (
	"fmt"
	"testing"
)

func TestDoctorTypedOutcomeDebtAndDeduplication(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			session := testSession(t, db, 662)
			schedule := typedSchedule(t, db, session, true)
			presentation := typedRowProposal(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
			input := doctorInboxInput(Repo{ID: session.RepoID}, 662, 200, "apply")
			input.TaskRef = session.TaskRef
			command, err := db.AdmitDoctorCommand(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "typed-outcome", SourceID: fmt.Sprintf("github-doctor-apply:%d", command.ID), Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
			if err != nil {
				t.Fatal(err)
			}
			if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "daemon"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ApplyDoctorTypedRows(ctx, op.ID, 1, "requeue_schedule"); err != nil {
				t.Fatal(err)
			}
			reason := ""
			if !success {
				reason = "failed later"
			}
			if err = db.SettleDoctorTypedRepair(ctx, op.ID, success, reason); err != nil {
				t.Fatal(err)
			}
			refs, err := db.DoctorRepairRecoveryTargets(ctx)
			if err != nil || len(refs) != 1 || refs[0] != session.TaskRef {
				t.Fatal(refs, err)
			}
			for range 2 {
				if err = db.EnsureDoctorRepairOutcome(ctx, op.ID, []string{"Rechecked step and retained backup"}, 99); err != nil {
					t.Fatal(err)
				}
			}
			deliveries, err := db.PendingDoctorDeliveries(ctx)
			if err != nil || len(deliveries) != 1 {
				t.Fatal(deliveries, err)
			}
			refs, err = db.DoctorRepairRecoveryTargets(ctx)
			if err != nil || len(refs) != 0 {
				t.Fatal("outcome debt retained", refs, err)
			}
		})
	}
}
