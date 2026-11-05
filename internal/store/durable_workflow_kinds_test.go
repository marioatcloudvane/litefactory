package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestClaimWorkflowAttemptKindNeverClaimsAnotherKind(t *testing.T) {
	db, _, a := discussionFixture(t)
	if _, err := db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ClaimWorkflowAttemptKind(t.Context(), a.CreatedAt, time.Minute, "brainstorm_scan"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign kind claim = %v", err)
	}
	got, _, err := db.ClaimWorkflowAttemptKind(t.Context(), a.CreatedAt, time.Minute, "advisory")
	if err != nil || got.AdmissionKind != "advisory" || got.AdvisoryID != a.ID || got.BrainstormScanID != "" {
		t.Fatalf("claim = %+v %v", got, err)
	}
}

func TestNonConfirmedAttemptSharesStopAndCancelRule(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	ts := FormatTime(at)
	for _, q := range []string{
		`INSERT INTO brainstorms(id,owner_id,owner_generation,repository_ref,state,created_at,last_activity_at) VALUES('b',42,1,'github:acme/api','open','` + ts + `','` + ts + `')`,
		`INSERT INTO brainstorm_scans(id,brainstorm_id,scan_seq,owner_id,owner_generation,repository_ref,request_key,digest,question,team_version,team_files,grant_id,consent_revision,job_id,execution_id,created_at) VALUES('scan','b',1,42,1,'github:acme/api','k','d','Why?','v','{}','grant','1','scan-job','scan-execution','` + ts + `')`,
		`INSERT INTO durable_workflow_jobs(id,owner_id,owner_generation,snapshot,state,created_at,updated_at,admission_kind) VALUES('scan-job','42',1,'{}','queued','` + ts + `','` + ts + `','brainstorm_scan')`,
		`INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at,admission_kind,brainstorm_scan_id) VALUES('scan-job',NULL,'scan-execution','queued','` + ts + `','brainstorm_scan','scan')`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	a, _, err := db.ClaimWorkflowAttempt(t.Context(), at, time.Minute)
	if err != nil || a.AdmissionKind != "brainstorm_scan" || a.BrainstormScanID != "scan" {
		t.Fatalf("claim = %+v %v", a, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_attempts SET state='stop_requested' WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.HeartbeatWorkflowAttempt(t.Context(), a, at.Add(time.Second)); err != nil {
		t.Fatalf("heartbeat while stopping: %v", err)
	}
	if workflowResultState(confirmedWorkflowKind, "cancelled") || !workflowResultState("brainstorm_scan", "cancelled") {
		t.Fatal("cancelled must be limited to non-confirmed kinds")
	}
	f := WorkflowFinish{State: "cancelled", Result: `{}`, Questions: `[]`, Summary: "cancelled"}
	err = db.withTx(t.Context(), func(tx *sql.Tx) error {
		current, e := fencedWorkflowAttemptTx(t.Context(), tx, a, at.Add(2*time.Second), false)
		if e != nil {
			return e
		}
		return finishNonConfirmedAttemptTx(t.Context(), tx, current, f, at.Add(2*time.Second))
	})
	if err != nil {
		t.Fatal(err)
	}
	j, err := db.ReadWorkflowJob(t.Context(), "scan-job")
	if err != nil || j.State != "cancelled" || j.Revision != 1 {
		t.Fatalf("job = %+v %v", j, err)
	}
	if _, err = db.RunningWorkflowAttempt(t.Context()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("slot kept: %v", err)
	}
}
