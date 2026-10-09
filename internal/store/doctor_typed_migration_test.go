package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestDoctorTypedSettlementMigrationPreservesApprovedRows(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	cut := -1
	for i, m := range migrations {
		if m.name == "doctor_typed_settlement" {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatal("settlement migration absent")
	}
	if err = db.migrateAll(ctx, migrations[:cut]); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 663)
	schedule := typedSchedule(t, db, session, true)
	op := typedRowApproval(t, db, session, "requeue_schedule", fmt.Sprintf("schedule:%d", schedule.ID), "running", "pending")
	step, err := db.ApplyDoctorTypedRows(ctx, op.ID, 1, "requeue_schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.migrateAll(ctx, migrations[cut:]); err != nil {
		t.Fatal(err)
	}
	retained, err := db.DoctorRepairSteps(ctx, op.ID)
	if err != nil || len(retained) != 1 || retained[0] != step {
		t.Fatal(retained, err)
	}
	if err = db.SettleDoctorTypedRepair(ctx, op.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	var violations int
	rows, err := db.sql.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		violations++
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || violations != 0 {
		t.Fatal("migration broke retained foreign keys", violations, err)
	}
}
