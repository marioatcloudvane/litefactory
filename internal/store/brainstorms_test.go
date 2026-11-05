package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var brainstormTestOwner = BrainstormOwner{OwnerID: 7, OwnerGeneration: 2}

func openTestBrainstorm(t *testing.T, db *DB, id string, at time.Time) BrainstormRecord {
	t.Helper()
	b, _, err := db.OpenBrainstorm(t.Context(), BrainstormRecord{BrainstormOwner: brainstormTestOwner, ID: id, RepositoryRef: "acme/app", GrantID: "grant-1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testBrainstormScan(b BrainstormRecord, n int) BrainstormScan {
	return BrainstormScan{BrainstormOwner: brainstormTestOwner, ID: fmt.Sprintf("scan-%d", n), BrainstormID: b.ID, RequestKey: fmt.Sprintf("ask-%d", n), Digest: fmt.Sprintf("d-%d", n), Question: fmt.Sprintf("question %d?", n), TeamVersion: "v1", TeamFiles: `{}`, GrantID: "grant-1", ConsentRevision: "{}", JobID: fmt.Sprintf("job-%d", n), ExecutionID: fmt.Sprintf("exec-%d", n)}
}

func TestBrainstormOpenResumeAndExpiry(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	again, created, err := db.OpenBrainstorm(t.Context(), BrainstormRecord{BrainstormOwner: brainstormTestOwner, ID: "b2", RepositoryRef: "acme/app"}, at.Add(time.Hour))
	if err != nil || created || again.ID != b.ID {
		t.Fatalf("resume = %+v %v %v", again, created, err)
	}
	other := BrainstormOwner{OwnerID: 7, OwnerGeneration: 3}
	if _, err = db.ReadBrainstorm(t.Context(), other, b.ID, at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other generation read = %v", err)
	}
	expired, err := db.ReadBrainstorm(t.Context(), brainstormTestOwner, b.ID, at.Add(BrainstormExpiry))
	if err != nil || expired.State != "closed" || expired.CloseReason != "expired" {
		t.Fatalf("expiry = %+v %v", expired, err)
	}
	if _, _, _, err = db.AddBrainstormEntry(t.Context(), brainstormTestOwner, BrainstormEntry{ID: "e1", BrainstormID: b.ID, Kind: "idea", Text: "x", RequestKey: "k", Digest: "d"}, expired.Revision, at.Add(BrainstormExpiry)); !errors.Is(err, ErrBrainstormClosed) {
		t.Fatalf("write on closed = %v", err)
	}
	fresh, created, err := db.OpenBrainstorm(t.Context(), BrainstormRecord{BrainstormOwner: brainstormTestOwner, ID: "b3", RepositoryRef: "acme/app"}, at.Add(BrainstormExpiry))
	if err != nil || !created || fresh.ID != "b3" {
		t.Fatalf("open after expiry = %+v %v %v", fresh, created, err)
	}
	list, err := db.ListBrainstorms(t.Context(), brainstormTestOwner, "acme/app", time.Time{}, "", 10, at.Add(BrainstormExpiry))
	if err != nil || len(list) != 2 || list[0].ID != "b3" || list[1].ID != "b1" {
		t.Fatalf("list = %+v %v", list, err)
	}
}

func TestBrainstormEntriesReplayRevisionAndCaps(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	add := func(e BrainstormEntry, rev int64) (BrainstormRecord, BrainstormEntry, bool, error) {
		e.BrainstormID = b.ID
		if e.Kind == "" {
			e.Kind = "idea"
		}
		return db.AddBrainstormEntry(t.Context(), brainstormTestOwner, e, rev, at)
	}
	got, e, replayed, err := add(BrainstormEntry{ID: "e1", Text: "one", RequestKey: "k1", Digest: "d1"}, 0)
	if err != nil || replayed || e.Seq != 1 || got.Revision != 1 {
		t.Fatalf("add = %+v %+v %v %v", got, e, replayed, err)
	}
	if _, e, replayed, err = add(BrainstormEntry{ID: "e1b", Text: "one", RequestKey: "k1", Digest: "d1"}, 0); err != nil || !replayed || e.ID != "e1" {
		t.Fatalf("replay = %+v %v %v", e, replayed, err)
	}
	if _, _, _, err = add(BrainstormEntry{ID: "e1c", Text: "two", RequestKey: "k1", Digest: "d2"}, 1); !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("reuse = %v", err)
	}
	var rev BrainstormRevisionError
	if _, _, _, err = add(BrainstormEntry{ID: "e2", Text: "two", RequestKey: "k2", Digest: "d2"}, 0); !errors.As(err, &rev) || rev.Current != 1 {
		t.Fatalf("stale = %v", err)
	}
	doc := strings.Repeat("d", BrainstormMaxEntryBytes)
	revision := int64(1)
	for i := range 16 {
		if _, _, _, err = add(BrainstormEntry{ID: fmt.Sprintf("doc-%d", i), Kind: "document_note", DocumentName: "spec.pdf", Text: doc, RequestKey: fmt.Sprintf("doc-%d", i), Digest: "x"}, revision); err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		revision++
	}
	var capErr BrainstormCapError
	if _, _, _, err = add(BrainstormEntry{ID: "doc-x", Kind: "document_note", DocumentName: "spec.pdf", Text: "y", RequestKey: "doc-x", Digest: "x"}, revision); !errors.As(err, &capErr) || !strings.Contains(capErr.Cap, "64 KB") {
		t.Fatalf("document cap = %v", err)
	}
	for i := 17; i < BrainstormMaxOwnerEntries; i++ {
		if _, _, _, err = add(BrainstormEntry{ID: fmt.Sprintf("i-%d", i), Text: "idea", RequestKey: fmt.Sprintf("i-%d", i), Digest: "x"}, revision); err != nil {
			t.Fatalf("idea %d: %v", i, err)
		}
		revision++
	}
	if _, _, _, err = add(BrainstormEntry{ID: "over", Text: "idea", RequestKey: "over", Digest: "x"}, revision); !errors.As(err, &capErr) || !strings.Contains(capErr.Cap, "500") {
		t.Fatalf("entry cap = %v", err)
	}
	counts, err := db.BrainstormEntryCounts(t.Context(), b.ID)
	if err != nil || counts["idea"] != 484 || counts["document_note"] != 16 {
		t.Fatalf("counts = %v %v", counts, err)
	}
}

func TestBrainstormScanAdmissionQueueAndCancel(t *testing.T) {
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	b := openTestBrainstorm(t, db, "b1", at)
	for i := range BrainstormMaxQueuedScans {
		s, replayed, err := db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, i), at)
		if err != nil || replayed || s.JobState != "queued" || s.ScanSeq != int64(i+1) {
			t.Fatalf("admit %d = %+v %v %v", i, s, replayed, err)
		}
	}
	var kind, scanRef string
	if err := db.sql.QueryRowContext(t.Context(), `SELECT j.admission_kind,a.brainstorm_scan_id FROM durable_workflow_jobs j JOIN durable_workflow_attempts a ON a.job_id=j.id WHERE j.id='job-0' AND a.state='queued'`).Scan(&kind, &scanRef); err != nil || kind != "brainstorm_scan" || scanRef != "scan-0" {
		t.Fatalf("job row = %q %q %v", kind, scanRef, err)
	}
	if s, replayed, err := db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, 0), at); err != nil || !replayed || s.ID != "scan-0" {
		t.Fatalf("replay = %+v %v %v", s, replayed, err)
	}
	reuse := testBrainstormScan(b, 0)
	reuse.Digest = "other"
	if _, _, err := db.AdmitBrainstormScan(t.Context(), reuse, at); !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("reuse = %v", err)
	}
	if _, _, err := db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, 9), at); !errors.Is(err, ErrBrainstormQueueFull) {
		t.Fatalf("queue = %v", err)
	}
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM durable_workflow_jobs WHERE id='job-9'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refused admission left job: %d %v", n, err)
	}
	s, won, err := db.CancelBrainstormScan(t.Context(), brainstormTestOwner, "scan-0", at)
	if err != nil || won || s.JobState != "cancelled" {
		t.Fatalf("cancel queued = %+v %v %v", s, won, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_jobs SET state='running' WHERE id='job-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_attempts SET state='running' WHERE job_id='job-1'`); err != nil {
		t.Fatal(err)
	}
	if s, _, err = db.CancelBrainstormScan(t.Context(), brainstormTestOwner, "scan-1", at); err != nil || s.JobState != "stop_requested" {
		t.Fatalf("cancel running = %+v %v", s, err)
	}
	if s, _, err = db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, 9), at); err != nil || s.ScanSeq != 6 {
		t.Fatalf("admit after cancel = %+v %v", s, err)
	}
	counts, err := db.BrainstormScanStateCounts(t.Context(), b.ID)
	if err != nil || counts["queued"] != 4 || counts["cancelled"] != 1 || counts["stop_requested"] != 1 {
		t.Fatalf("scan counts = %v %v", counts, err)
	}
	page, err := db.BrainstormScans(t.Context(), b.ID, 2, 10)
	if err != nil || len(page) != 4 || page[0].ScanSeq != 3 {
		t.Fatalf("scan page = %d %v", len(page), err)
	}
	closed, _, err := db.CloseBrainstorm(t.Context(), brainstormTestOwner, b.ID, 0, at)
	if err != nil || closed.State != "closed" || closed.CloseReason != "owner" {
		t.Fatalf("close = %+v %v", closed, err)
	}
	if _, replayed, err := db.CloseBrainstorm(t.Context(), brainstormTestOwner, b.ID, 0, at); err != nil || !replayed {
		t.Fatalf("close replay = %v %v", replayed, err)
	}
	if _, _, err = db.AdmitBrainstormScan(t.Context(), testBrainstormScan(b, 10), at); !errors.Is(err, ErrBrainstormClosed) {
		t.Fatalf("scan on closed = %v", err)
	}
}
