package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
)

func doctorIOAttempt(id string) DoctorAttempt {
	return DoctorAttempt{ID: id, SourceID: "test:" + id, OwnerText: "explain the blockage", PromptVersion: "test-prompt", GuideVersion: "test-guide"}
}

func TestDoctorHistoryReopenStopAndResultReceipts(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	const ref = "github:acme/api#406"
	c, err := db.BeginDoctorAttempt(ctx, ref, doctorIOAttempt("first"))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	if binding.RunID != 0 || binding.DoctorAttemptID != "first" {
		t.Fatalf("wrong owner: %+v", binding)
	}
	request := resultRecord(binding, "retained-reply", 0)
	receipt, err := db.RecordResult(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.AppliedRevision != 1 {
		t.Fatalf("revision = %d", receipt.AppliedRevision)
	}
	if err := db.StopDoctorConversation(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecordResult(ctx, resultRecord(binding, "late", 1)); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("late result: %v", err)
	}
	if err := db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: "first", Evidence: "late evidence"}); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("late prepare: %v", err)
	}
	path := db.Path()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	replay, err := reopened.ReplayResultRequest(ctx, binding, request.RequestID, request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != receipt.ID || replay.AppliedRevision != receipt.AppliedRevision {
		t.Fatalf("replay changed receipt: %+v", replay)
	}
	saved, err := reopened.GetResultReceipt(ctx, binding, 0, request.RequestID)
	if err != nil || saved.ID != receipt.ID {
		t.Fatalf("receipt lookup: %+v %v", saved, err)
	}
	history, err := reopened.DoctorAttempts(ctx, c.ID)
	if err != nil || len(history) != 1 || history[0].State != "interrupted" || history[0].OwnerText != "explain the blockage" {
		t.Fatalf("history: %+v %v", history, err)
	}
	next, err := reopened.BeginDoctorAttempt(ctx, ref, doctorIOAttempt("next"))
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == c.ID || next.Generation != c.Generation+1 {
		t.Fatalf("generation not advanced: %+v %+v", c, next)
	}
	var sessions, runs int
	if err := reopened.sql.QueryRowContext(ctx, `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := reopened.sql.QueryRowContext(ctx, `SELECT count(*) FROM runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || runs != 0 {
		t.Fatalf("fabricated workflow rows: sessions=%d runs=%d", sessions, runs)
	}
}

func TestDoctorOrphanRecoveryPreservesWorkflowAndOtherIssue(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	run := testRun(t, db, session.ID, 1)
	c, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("orphan"))
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := db.BeginDoctorAttempt(ctx, "github:acme/api#407", doctorIOAttempt("sibling"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("recovery")); err != nil {
		t.Fatal(err)
	}
	history, err := db.DoctorAttempts(ctx, c.ID)
	if err != nil || len(history) != 2 || history[0].State != "interrupted" || history[1].State != "running" {
		t.Fatalf("recovery: %+v %v", history, err)
	}
	other, err := db.DoctorAttempts(ctx, sibling.ID)
	if err != nil || len(other) != 1 || other[0].State != "running" {
		t.Fatalf("sibling changed: %+v %v", other, err)
	}
	var status string
	if err := db.sql.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(run.Status) {
		t.Fatalf("workflow run changed: %s -> %s", run.Status, status)
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := db.ReadDoctorEvidence(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.SessionID != session.ID || evidence.TaskRef != session.TaskRef || evidence.CapturedAt.IsZero() {
		t.Fatalf("bad provenance: %+v", evidence)
	}
	if _, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("recovery")); err == nil {
		t.Fatal("duplicate source admitted")
	}
	after, err := db.DoctorAttempts(ctx, c.ID)
	if err != nil || !reflect.DeepEqual(after, history) {
		t.Fatalf("duplicate source changed history: %+v %v", after, err)
	}
}

func TestDoctorSchemaCheckNeverInitializesDatabase(t *testing.T) {
	db, err := Open(t.Context(), t.TempDir()+"/uninitialized.db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.CheckDoctorSchema(t.Context()); err == nil {
		t.Fatal("uninitialized schema accepted")
	}
	var tables int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("schema check created %d tables", tables)
	}
}

func TestDoctorResultOwnershipConstraintsRejectMissingAndMixedOwners(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	if _, err := db.BeginDoctorAttempt(ctx, "github:acme/api#406", doctorIOAttempt("owned")); err != nil {
		t.Fatal(err)
	}
	b, err := db.BindDoctorResultAttempt(ctx, "owned")
	if err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 407)
	run := testRun(t, db, session.ID, 1)
	for _, test := range []struct {
		name   string
		run    any
		doctor any
	}{
		{"missing", nil, nil}, {"mixed", run.ID, "owned"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := db.sql.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active,doctor_attempt_id) VALUES(?,?,?,1,?)`, test.run, "invalid-"+test.name, b.ResultID, test.doctor)
			if err == nil {
				t.Fatal("invalid result ownership accepted")
			}
		})
	}
	if _, err := db.sql.ExecContext(ctx, `INSERT INTO application_result_attempts(run_id,attempt,result_id,active,doctor_attempt_id) VALUES(NULL,'duplicate',?,1,'owned')`, b.ResultID); err == nil {
		t.Fatal("duplicate doctor ownership accepted")
	}
}
