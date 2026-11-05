package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func brainstormTaskFixture(t *testing.T, sets ...string) (*DB, time.Time) {
	t.Helper()
	db := newTestDB(t)
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	ts := FormatTime(at)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.sql.ExecContext(t.Context(), q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO brainstorms(id,owner_id,owner_generation,repository_ref,state,created_at,last_activity_at) VALUES('b',42,1,'github:acme/api','open',?,?)`, ts, ts)
	for i, set := range sets {
		scan := "scan-" + set
		exec(`INSERT INTO brainstorm_scans(id,brainstorm_id,scan_seq,owner_id,owner_generation,repository_ref,request_key,digest,question,team_version,team_files,grant_id,consent_revision,job_id,execution_id,created_at) VALUES(?,'b',?,42,1,'github:acme/api',?,'d','Why?','v','{}','grant','1',?,?,?)`, scan, i+1, "k"+set, "job-"+set, "exec-"+set, ts)
		state := "latest"
		if i < len(sets)-1 {
			state = "superseded"
		}
		exec(`INSERT INTO brainstorm_draft_sets(id,brainstorm_id,scan_id,seq,state,summary,created_at) VALUES(?,'b',?,?,?,'s',?)`, set, scan, i+1, state, ts)
		for j := 1; j <= 2; j++ {
			exec(`INSERT INTO brainstorm_drafts(id,draft_set_id,key,ordinal,title,size,impact,ledger,digest) VALUES(?,?,?,?,?,'S','i','{}','x')`, fmt.Sprintf("%s-d%d", set, j), set, fmt.Sprintf("k%d", j), j, fmt.Sprintf("Title %d", j))
		}
	}
	return db, at
}

func admitFixtureBatch(t *testing.T, db *DB, set, execution string, at time.Time) (BrainstormTaskBatch, BrainstormTaskContinuation, bool, error) {
	t.Helper()
	items := []BrainstormTaskItem{{DraftID: set + "-d1", Title: "Title 1", Body: "body 1", Marker: "m1"}, {DraftID: set + "-d2", Title: "Title 2", Body: "body 2", Marker: "m2"}}
	return db.AdmitBrainstormTaskBatch(t.Context(), BrainstormTaskBatch{ExecutionID: execution, OwnerID: "42", OwnerGeneration: 1, RepositoryID: 1, RepositoryRef: "github:acme/api", BrainstormID: "b", DraftSetID: set}, items, BrainstormTaskContinuation{}, at)
}

func TestBrainstormTaskBatchAdmissionIsIdempotentAndMarksSetUsed(t *testing.T) {
	db, at := brainstormTaskFixture(t, "set1")
	b, c, replayed, err := admitFixtureBatch(t, db, "set1", "exec-1", at)
	if err != nil || replayed || b.State != "running" || c.Kind != "create" || len(c.Items) != 2 {
		t.Fatalf("admit = %+v %+v %v %v", b, c, replayed, err)
	}
	set, err := db.ReadBrainstormDraftSet(t.Context(), "set1")
	if err != nil || set.State != "used" {
		t.Fatalf("set = %+v %v", set, err)
	}
	b2, c2, replayed, err := admitFixtureBatch(t, db, "set1", "exec-1", at.Add(time.Minute))
	if err != nil || !replayed || b2.ID != b.ID || c2.ID != c.ID {
		t.Fatalf("replay = %+v %+v %v %v", b2, c2, replayed, err)
	}
	items, err := db.BrainstormTaskItems(t.Context(), b.ID)
	if err != nil || len(items) != 2 || items[0].State != BrainstormItemPending || items[0].Ordinal != 1 || items[1].DraftID != "set1-d2" {
		t.Fatalf("items = %+v %v", items, err)
	}
	if got, err := db.BrainstormTaskBatchForDraftSet(t.Context(), "set1"); err != nil || got.ID != b.ID {
		t.Fatalf("batch for set = %+v %v", got, err)
	}
	if active, err := db.ActiveBrainstormTaskItems(t.Context(), 10); err != nil || len(active) != 2 {
		t.Fatalf("active = %d %v", len(active), err)
	}
}

func TestBrainstormTaskDraftBelongsToOneBatch(t *testing.T) {
	db, at := brainstormTaskFixture(t, "set1")
	if _, _, _, err := admitFixtureBatch(t, db, "set1", "exec-1", at); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := admitFixtureBatch(t, db, "set1", "exec-2", at); err == nil {
		t.Fatal("second batch for the same drafts admitted")
	}
	if n := countRows(t, db, "brainstorm_task_batches"); n != 1 {
		t.Fatalf("batches = %d", n)
	}
}

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBrainstormTaskContinuationResetsFrozenItemsAndRefusesChanges(t *testing.T) {
	db, at := brainstormTaskFixture(t, "set1")
	b, _, _, err := admitFixtureBatch(t, db, "set1", "exec-1", at)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := db.BrainstormTaskItems(t.Context(), b.ID)
	first, second := items[0], items[1]
	first.State, first.CreateRetries, first.Reason = BrainstormItemNotCreated, 3, "forbidden"
	if ok, err := db.SaveBrainstormTaskItem(t.Context(), first, BrainstormItemPending, at); err != nil || !ok {
		t.Fatalf("save = %v %v", ok, err)
	}
	if ok, err := db.SaveBrainstormTaskItem(t.Context(), first, BrainstormItemPending, at); err != nil || ok {
		t.Fatalf("stale CAS accepted = %v %v", ok, err)
	}
	second.State, second.FailedLaunchAttempts = BrainstormItemPlanStartFailed, BrainstormLaunchFailureLimit
	if ok, err := db.SaveBrainstormTaskItem(t.Context(), second, BrainstormItemPending, at); err != nil || !ok {
		t.Fatalf("save = %v %v", ok, err)
	}
	frozen := []BrainstormFrozenItem{{ItemID: first.ID, State: BrainstormItemNotCreated, Generation: 1}, {ItemID: second.ID, State: BrainstormItemPlanStartFailed, Generation: 1}}
	stale := []BrainstormFrozenItem{{ItemID: first.ID, State: BrainstormItemPending, Generation: 1}}
	if _, _, err = db.AdmitBrainstormTaskContinuation(t.Context(), BrainstormTaskContinuation{BatchID: b.ID, ExecutionID: "exec-stale", Kind: "continue", Items: stale, FrozenDigest: BrainstormFrozenDigest(stale)}, nil, at); !errors.Is(err, ErrBrainstormFrozenChanged) {
		t.Fatalf("stale frozen item accepted: %v", err)
	}
	c, replayed, err := db.AdmitBrainstormTaskContinuation(t.Context(), BrainstormTaskContinuation{BatchID: b.ID, ExecutionID: "exec-2", Kind: "continue", Items: frozen, FrozenDigest: BrainstormFrozenDigest(frozen)}, nil, at)
	if err != nil || replayed || c.State != "running" {
		t.Fatalf("continue = %+v %v %v", c, replayed, err)
	}
	if again, replayed, err := db.AdmitBrainstormTaskContinuation(t.Context(), BrainstormTaskContinuation{BatchID: b.ID, ExecutionID: "exec-2", Kind: "continue", Items: frozen, FrozenDigest: BrainstormFrozenDigest(frozen)}, nil, at); err != nil || !replayed || again.ID != c.ID {
		t.Fatalf("replay = %+v %v %v", again, replayed, err)
	}
	got1, _ := db.ReadBrainstormTaskItem(t.Context(), first.ID)
	got2, _ := db.ReadBrainstormTaskItem(t.Context(), second.ID)
	if got1.State != BrainstormItemPending || got1.CreateRetries != 0 || got2.State != BrainstormItemCreated || got2.FailedLaunchAttempts != BrainstormLaunchFailureLimit-1 {
		t.Fatalf("reset = %+v %+v", got1, got2)
	}
}

func TestBrainstormTaskRecreateBumpsGeneration(t *testing.T) {
	db, at := brainstormTaskFixture(t, "set1")
	b, _, _, err := admitFixtureBatch(t, db, "set1", "exec-1", at)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := db.BrainstormTaskItems(t.Context(), b.ID)
	it := items[0]
	it.State, it.ChecksDone = BrainstormItemUncertainExhausted, 6
	if ok, err := db.SaveBrainstormTaskItem(t.Context(), it, BrainstormItemPending, at); err != nil || !ok {
		t.Fatal(ok, err)
	}
	frozen := []BrainstormFrozenItem{{ItemID: it.ID, State: BrainstormItemUncertainExhausted, Generation: 1}}
	recreate := &BrainstormTaskItem{ID: it.ID, Generation: 2, Title: "Title 1", Body: "body gen 2", Marker: "m1-2"}
	if _, _, err = db.AdmitBrainstormTaskContinuation(t.Context(), BrainstormTaskContinuation{BatchID: b.ID, ExecutionID: "exec-r", Kind: "recreate", Items: frozen, FrozenDigest: BrainstormFrozenDigest(frozen)}, recreate, at); err != nil {
		t.Fatal(err)
	}
	got, _ := db.ReadBrainstormTaskItem(t.Context(), it.ID)
	if got.Generation != 2 || got.State != BrainstormItemPending || got.Marker != "m1-2" || got.Body != "body gen 2" || got.ChecksDone != 0 {
		t.Fatalf("recreated = %+v", got)
	}
	if cs, err := db.BrainstormTaskContinuations(t.Context(), b.ID); err != nil || len(cs) != 2 {
		t.Fatalf("continuations = %d %v", len(cs), err)
	}
}
