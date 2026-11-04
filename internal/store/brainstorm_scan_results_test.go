package store

import (
	"errors"
	"testing"
	"time"
)

// runningBrainstormScan admits one scan and places its attempt in the worker slot.
func runningBrainstormScan(t *testing.T, db *DB, b BrainstormRecord, n int, at time.Time) WorkflowAttempt {
	t.Helper()
	s, _, err := db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, n), at)
	if err != nil {
		t.Fatal(err)
	}
	stamp := ownerActionTime(at)
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_attempts SET state='running',fence=fence+1,started_at=?,deadline=? WHERE execution_id=?`, stamp, ownerActionTime(at.Add(time.Hour)), s.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_jobs SET state='running' WHERE id=?`, s.JobID); err != nil {
		t.Fatal(err)
	}
	a, err := db.WorkflowAttemptByExecution(t.Context(), s.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_worker SET attempt_id=?,generation=?,expires_at=? WHERE singleton=1`, a.ID, a.Fence, ownerActionTime(at.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFinishBrainstormScanAppendsExchangeAndQuestions(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	a := runningBrainstormScan(t, db, b, 1, at)
	run, err := db.ReadBrainstormScanForAttempt(t.Context(), a)
	if err != nil || run.ID != "scan-1" || run.Question != "question 1?" {
		t.Fatalf("read = %+v %v", run, err)
	}
	if _, err = db.ReadBrainstormScanForAttempt(t.Context(), WorkflowAttempt{AdmissionKind: "advisory", JobID: a.JobID, ExecutionID: a.ExecutionID}); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("other kind = %v", err)
	}
	f := BrainstormScanFinish{State: "succeeded", Answer: "The worker retries twice.", Research: `{"request":"q","findings":[{"id":"F1","text":"retry"}]}`, BasisCommit: "abc1234def", Cost: "cost unknown", Questions: []string{"Should it retry three times?"}}
	if err = db.FinishBrainstormScan(t.Context(), a, f, at.Add(time.Minute), false); err != nil {
		t.Fatal(err)
	}
	entries, earlier, err := db.BrainstormScanContext(t.Context(), b.ID, "")
	if err != nil || len(entries) != 2 || len(earlier) != 1 {
		t.Fatalf("context = %+v %+v %v", entries, earlier, err)
	}
	if entries[0].Kind != "exchange" || entries[0].ScanID != "scan-1" || entries[1].Kind != "open_question" || entries[1].Text != "Should it retry three times?" {
		t.Fatalf("entries = %+v", entries)
	}
	var key string
	if err = db.sql.QueryRowContext(t.Context(), `SELECT request_key FROM brainstorm_entries WHERE kind='exchange'`).Scan(&key); err != nil || key != "scan:scan-1" {
		t.Fatalf("exchange key = %q %v", key, err)
	}
	if earlier[0].ExchangeSeq != entries[0].Seq || earlier[0].Answer != f.Answer || earlier[0].BasisCommit != f.BasisCommit || earlier[0].FinishedAt.IsZero() {
		t.Fatalf("scan = %+v", earlier[0])
	}
	var revision int64
	var jobState, attemptState string
	var slot int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT revision FROM brainstorms WHERE id=?`, b.ID).Scan(&revision); err != nil || revision != b.Revision+1 {
		t.Fatalf("revision = %d %v", revision, err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT j.state,a.state FROM durable_workflow_jobs j JOIN durable_workflow_attempts a ON a.job_id=j.id WHERE j.id=?`, a.JobID).Scan(&jobState, &attemptState); err != nil || jobState != "succeeded" || attemptState != "succeeded" {
		t.Fatalf("states = %q %q %v", jobState, attemptState, err)
	}
	if err = db.sql.QueryRowContext(t.Context(), `SELECT attempt_id FROM durable_workflow_worker WHERE singleton=1`).Scan(&slot); err != nil || slot != 0 {
		t.Fatalf("slot = %d %v", slot, err)
	}
	if err = db.FinishBrainstormScan(t.Context(), a, f, at.Add(time.Minute), false); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("second finish = %v", err)
	}
}

func TestFinishBrainstormScanFailureKeepsNothingPartial(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	a := runningBrainstormScan(t, db, b, 1, at)
	if err := db.FinishBrainstormScan(t.Context(), a, BrainstormScanFinish{State: "succeeded"}, at.Add(2*time.Hour), false); !errors.Is(err, ErrWorkflowFenced) {
		t.Fatalf("past deadline = %v", err)
	}
	if err := db.FinishBrainstormScan(t.Context(), a, BrainstormScanFinish{State: "uncertain", Answer: "partial", FailureReason: "ask again"}, at.Add(2*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	run, err := db.ReadBrainstormScanByExecution(t.Context(), a.ExecutionID)
	if err != nil || run.Answer != "" || run.FailureReason != "ask again" || run.ExchangeSeq != 0 {
		t.Fatalf("scan = %+v %v", run, err)
	}
	var n int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM brainstorm_entries WHERE brainstorm_id=?`, b.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("entries = %d %v", n, err)
	}
}

func TestBrainstormScanReceiptReadOnly(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	a := runningBrainstormScan(t, db, b, 1, at)
	if _, err := db.BindBrainstormScanResult(t.Context(), a.ExecutionID); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("bind without grant = %v", err)
	}
	binding, err := db.BrainstormScanResultBinding(t.Context(), a.ExecutionID)
	if err != nil || binding.ScanID != "scan-1" || binding.Fence != a.Fence {
		t.Fatalf("binding = %+v %v", binding, err)
	}
	if _, _, err = db.ReadBrainstormScanResult(t.Context(), binding, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no receipt = %v", err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO brainstorm_scan_receipts(scan_id,execution_id,request_id,digest,payload,created_at) VALUES(?,?,?,?,?,?)`, binding.ScanID, binding.ExecutionID, "r1", submissionDigest([]byte(`{}`)), `{}`, FormatTime(at)); err != nil {
		t.Fatal(err)
	}
	id, payload, err := db.ReadBrainstormScanResult(t.Context(), binding, "")
	if err != nil || id != "r1" || string(payload) != `{}` {
		t.Fatalf("receipt = %q %q %v", id, payload, err)
	}
	if replay, err := db.SubmitBrainstormScanResult(t.Context(), binding, "r1", []byte(`{}`)); err != nil || !replay {
		t.Fatalf("replay = %v %v", replay, err)
	}
	if _, err = db.SubmitBrainstormScanResult(t.Context(), binding, "r2", []byte(`{}`)); !errors.Is(err, ErrResultCorrelation) {
		t.Fatalf("second receipt = %v", err)
	}
}
