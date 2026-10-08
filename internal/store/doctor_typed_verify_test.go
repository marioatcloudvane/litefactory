package store

import (
	"fmt"
	"testing"
)

func TestDoctorTypedRowVerificationRereadsSettledState(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 650)
	schedule := typedSchedule(t, db, session, true)
	op := typedRowApproval(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
	if err := db.VerifyDoctorRows(t.Context(), op.ID, 1); err == nil {
		t.Fatal("pending step passed verification")
	}
	step, err := db.ApplyDoctorTypedRows(t.Context(), op.ID, 1, "requeue_schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyDoctorRows(t.Context(), op.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err = db.StopDoctorRepair(t.Context(), session.TaskRef, "interrupted after write"); err != nil {
		t.Fatal(err)
	}
	// Verification is observational and remains available after fencing.
	if err = db.VerifyDoctorRows(t.Context(), op.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE schedules SET status='failed' WHERE id=?`, schedule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyDoctorRows(t.Context(), op.ID, 1); err == nil {
		t.Fatal("drifted typed row passed verification")
	}
	rows, err := db.DoctorRepairSteps(t.Context(), op.ID)
	if err != nil || len(rows) != 1 || rows[0].BackupReference != step.BackupReference || rows[0].BackupDigest != step.BackupDigest || rows[0].State != "verified" {
		t.Fatalf("verification changed journal: %+v %v", rows, err)
	}
}
