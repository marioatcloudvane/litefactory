package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestJudgeMigrationPreservesPopulatedLedgers(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "judge"))
	session := testSession(t, db, 814)
	malformed := testSession(t, db, 816)
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,'{"stories":')`, malformed.TaskRef); err != nil {
		t.Fatal(err)
	}

	a := resultRun(t, db, session, "research", "retained-writer", 1)
	var schema []string
	rows, err := db.sql.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type IN ('index','trigger') AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, name)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = db.sql.ExecContext(ctx, `INSERT INTO application_results(id,task,mode,work_context,context_hash,schema_version,workflow_version,submission_version,revision) VALUES(1,?,'research','retained','hash',1,2,2,1)`, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active) VALUES(?,?,1,1)`, a.RunID, a.Attempt); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO application_result_requests(id,result_id,run_id,attempt,request_id,request_hash,payload,diagnostics,input_state,applied_revision) VALUES(1,1,?,?,'saved','hash','{}','{}','accepted',1)`, a.RunID, a.Attempt); err != nil {
		t.Fatal(err)
	}
	snapshot := []byte(`{"retained":"before upgrade"}`)
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO application_result_revisions(result_id,revision,run_id,attempt,request_row,snapshot,snapshot_hash,retired) VALUES(1,1,?,?,1,?,'hash','[]')`, a.RunID, a.Attempt, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(ctx, `INSERT INTO application_result_observations(request_row,sequence,run_id,attempt,dimension,state) VALUES(1,1,?,?,'input','accepted')`, a.RunID, a.Attempt); err != nil {
		t.Fatal(err)
	}
	meter, err := db.StartMeterAttempt(ctx, a.RunID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	doctorAttempt := doctorIOAttempt("upgrade-doctor")
	doctorAttempt.Provider = "claude"
	doctor, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorAttempt)
	if err != nil {
		t.Fatal(err)
	}
	if doctor.ID == 0 {
		t.Fatal("missing doctor conversation")
	}
	doctorAttempt.Evidence = "retained evidence"
	if err = db.PrepareDoctorAttempt(ctx, doctorAttempt); err != nil {
		t.Fatal(err)
	}
	dm, err := db.StartDoctorMeterAttempt(ctx, "upgrade-doctor", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	var malformedState string
	if err = db.sql.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, malformed.TaskRef).Scan(&malformedState); err != nil || malformedState != `{"stories":` {
		t.Fatalf("corrupt legacy state changed during migration: %q %v", malformedState, err)
	}
	if _, _, err = db.GetPlanWorkflowState(ctx, malformed.TaskRef); err == nil {
		t.Fatal("corrupt legacy state became a fresh Plan")
	}
	for _, name := range schema {
		var n int
		if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name=? AND type IN ('index','trigger')`, name).Scan(&n); err != nil || n != 1 {
			t.Fatalf("lost schema %s: %d %v", name, n, err)
		}
	}
	rows, err = db.sql.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("upgrade broke retained foreign keys")
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	var retained []byte
	if err = db.sql.QueryRowContext(ctx, `SELECT snapshot FROM application_result_revisions WHERE result_id=1 AND revision=1`).Scan(&retained); err != nil || !bytes.Equal(retained, snapshot) {
		t.Fatalf("retained result changed: %s %v", retained, err)
	}
	got, err := db.GetMeterAttempt(ctx, meter.ID)
	if err != nil || got.RunID != a.RunID {
		t.Fatalf("run meter lost: %+v %v", got, err)
	}
	got, err = db.GetMeterAttempt(ctx, dm.ID)
	if err != nil || got.DoctorAttemptID != "upgrade-doctor" {
		t.Fatalf("doctor meter lost: %+v %v", got, err)
	}
	b := ResultBinding{ResultID: 1, RunID: a.RunID, Task: session.TaskRef, Mode: "research", WorkContext: "retained", ContextHash: "hash", Attempt: a.Attempt, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}
	if err = db.CheckResultWriter(ctx, b); err != nil {
		t.Fatalf("active writer lost: %v", err)
	}
	receipt, err := db.GetResultReceipt(ctx, b, a.RunID, "saved")
	if err != nil || receipt.AppliedRevision != 1 {
		t.Fatalf("receipt lost: %+v %v", receipt, err)
	}
}
