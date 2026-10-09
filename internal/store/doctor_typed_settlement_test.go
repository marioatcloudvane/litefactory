package store

import (
	"fmt"
	"testing"
)

func TestDoctorTypedSettlementReleasesHoldAndRetainsBackup(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			db := newTestDB(t)
			session := testSession(t, db, 660)
			schedule := typedSchedule(t, db, session, true)
			op := typedRowApproval(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
			step, err := db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, "requeue_schedule")
			if err != nil {
				t.Fatal(err)
			}
			reason := ""
			want := "completed"
			if !success {
				reason = "later step refused"
				want = "failed"
			}
			if err = db.SettleDoctorTypedRepair(t.Context(), op.ID, success, reason); err != nil {
				t.Fatal(err)
			}
			got, err := db.ReadDoctorRepair(t.Context(), session.TaskRef)
			if err != nil || got.State != want || !got.Fenced {
				t.Fatal(got, err)
			}
			held, err := db.DoctorRepairHeld(t.Context(), session.TaskRef)
			if err != nil || held {
				t.Fatal("hold retained", held, err)
			}
			steps, err := db.DoctorRepairSteps(t.Context(), op.ID)
			if err != nil || steps[0].BackupReference != step.BackupReference || steps[0].State != "verified" {
				t.Fatal(steps, err)
			}
			if _, err = db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, "requeue_schedule"); err == nil {
				t.Fatal("settled repair retained authority")
			}
			if err = db.SettleDoctorTypedRepair(t.Context(), op.ID, success, reason); err != nil {
				t.Fatal("settlement retry", err)
			}
		})
	}
}

func TestDoctorTypedSettlementRejectsUnverifiedSuccess(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 661)
	schedule := typedSchedule(t, db, session, true)
	op := typedRowApproval(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
	if err := db.SettleDoctorTypedRepair(t.Context(), op.ID, true, ""); err == nil {
		t.Fatal("unverified repair completed")
	}
	held, err := db.DoctorRepairHeld(t.Context(), session.TaskRef)
	if err != nil || !held {
		t.Fatal("refusal released hold", held, err)
	}
	if err = db.SettleDoctorTypedRepair(t.Context(), op.ID, false, "interrupted before effect"); err != nil {
		t.Fatal(err)
	}
}
