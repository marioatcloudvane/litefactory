package store

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestDoctorTypedRepairCatalog(t *testing.T) {
	t.Parallel()
	want := []string{"release_instance", "resume_story", "park_story", "stop_run", "requeue_schedule", "cancel_schedule", "consume_comment", "sync_branch"}
	if got := DoctorRepairToolKinds(); !slices.Equal(got, want) {
		t.Fatalf("catalog = %v, want %v", got, want)
	}
	for _, kind := range want {
		if !DoctorRepairToolKind(kind) {
			t.Errorf("typed repair kind %q refused", kind)
		}
	}
	for _, kind := range []string{"", "poll_flag", "session_file", "worktree_file", "run_interrupt", "session_unlock", "report_completion", "watermark_rewind", "graph_dispatch_cancel", "graph_wait_cancel", "graph_attempt_cancel", "graph_fail", "doctor_write", "execute_sql", "release_instance ", "Release_instance"} {
		if DoctorRepairToolKind(kind) {
			t.Errorf("noncatalog kind %q accepted", kind)
		}
	}
	callerKinds := DoctorRepairToolKinds()
	callerKinds[0] = "execute_sql"
	if DoctorRepairToolKind(callerKinds[0]) || !slices.Equal(DoctorRepairToolKinds(), want) {
		t.Fatal("caller mutated the factory catalog")
	}
}

func TestDoctorTypedRepairMigrationRetainsLegacyJournal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "doctor_typed_repair_kinds"))
	session := testSession(t, db, 406)
	_, presentation := retainedPollProposal(t, db, session)
	op, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "retained", SourceID: "approval", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AcknowledgeDoctorExclusion(ctx, op.ID, "instance"); err != nil {
		t.Fatal(err)
	}
	if err = db.BackupDoctorRows(ctx, op.ID, 1); err != nil {
		t.Fatal(err)
	}
	legacy := []string{"session_file", "worktree_file", "run_interrupt", "session_unlock", "report_completion", "watermark_rewind", "graph_dispatch_cancel", "graph_wait_cancel", "graph_attempt_cancel", "graph_fail"}
	for i, kind := range legacy {
		if _, err = db.sql.ExecContext(ctx, `INSERT INTO doctor_repair_steps(repair_id,number,kind,target,before_value,after_value,source_identity,state,backup,backup_reference,backup_digest,observed_after) VALUES(?,?,?,'retained-target','before','after','identity','verified',X'000102FF','private-reference','digest','observed')`, op.ID, i+2, kind); err != nil {
			t.Fatal(kind, err)
		}
	}
	before, err := db.DoctorRepairSteps(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeBackups := joinRows(t, db, `SELECT number||':'||COALESCE(hex(backup),'NULL') FROM doctor_repair_steps ORDER BY number`)
	insertKind := func(number int, kind string) error {
		_, err := db.sql.ExecContext(ctx, `INSERT INTO doctor_repair_steps(repair_id,number,kind,target,before_value,after_value) VALUES(?,?,?,'target','before','after')`, op.ID, number, kind)
		return err
	}
	if err = insertKind(20, "release_instance"); err == nil {
		t.Fatal("old schema accepted a typed kind")
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("idempotent migration", err)
	}
	after, err := db.DoctorRepairSteps(ctx, op.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("retained journal changed: before=%+v after=%+v error=%v", before, after, err)
	}
	if afterBackups := joinRows(t, db, `SELECT number||':'||COALESCE(hex(backup),'NULL') FROM doctor_repair_steps ORDER BY number`); afterBackups != beforeBackups {
		t.Fatal("private backup bytes changed")
	}
	for i, kind := range DoctorRepairToolKinds() {
		if err = insertKind(20+i, kind); err != nil {
			t.Fatal("new schema refused typed kind", kind, err)
		}
	}
	if err = insertKind(30, "execute_sql"); err == nil {
		t.Fatal("schema accepted an unknown kind")
	}
	for _, query := range []string{
		`UPDATE doctor_repair_steps SET kind='stop_run' WHERE repair_id=? AND number=1`,
		`DELETE FROM doctor_repair_steps WHERE repair_id=? AND number=1`,
	} {
		if _, err = db.sql.ExecContext(ctx, query, op.ID); err == nil {
			t.Fatal("migration removed an immutability or hold guard", query)
		}
	}
	if _, err = db.sql.ExecContext(ctx, `UPDATE sessions SET needs_poll=1 WHERE id=?`, session.ID); err == nil {
		t.Fatal("migration removed the session hold guard")
	}
}
