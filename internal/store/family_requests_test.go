package store

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFamilyLedgerConcurrentReopenAndIsolation(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 701)
	b, err := db.BindResultAttempt(t.Context(), resultRun(t, db, session, "research", "family-attempt", 1))
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.TrimPrefix(strings.Split(session.TaskRef, "#")[0], "github:")
	q := FamilyRequest{Scope: FamilyScope{Repository: repo, Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}, RequestID: "stable", Tool: "feedback_preflight", PayloadHash: strings.Repeat("a", 64)}
	var owners atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			_, first, e := db.BeginFamilyRequest(t.Context(), b, q)
			if e != nil {
				t.Error(e)
			}
			if first {
				owners.Add(1)
			}
		})
	}
	wg.Wait()
	if owners.Load() != 1 {
		t.Fatalf("dispatch owners=%d", owners.Load())
	}
	for _, change := range []func(*FamilyRequest){func(r *FamilyRequest) { r.Tool = "activity_update" }, func(r *FamilyRequest) { r.PayloadHash = strings.Repeat("b", 64) }, func(r *FamilyRequest) { r.Scope.Attempt = "foreign" }, func(r *FamilyRequest) { r.Scope.RunID++ }, func(r *FamilyRequest) { r.Scope.Repository = "foreign/repo" }} {
		changed := q
		change(&changed)
		if _, _, e := db.BeginFamilyRequest(t.Context(), b, changed); e == nil {
			t.Fatal("cross-family/scope collision accepted")
		}
	}
	if err = db.FinishFamilyRequest(t.Context(), b, q.RequestID, "confirmed", FamilyReference{Kind: "result", RequestID: "underlying"}); err != nil {
		t.Fatal(err)
	}
	path := db.Path()
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	got, first, err := reopened.BeginFamilyRequest(t.Context(), b, q)
	if err != nil || first || got.Disposition != "confirmed" || got.Reference.RequestID != "underlying" {
		t.Fatalf("reopen replay: %+v first=%v err=%v", got, first, err)
	}
	if err = reopened.FinishFamilyRequest(t.Context(), b, q.RequestID, "rejected", FamilyReference{}); !errors.Is(err, ErrResultCorrelation) {
		t.Fatalf("changed terminal outcome: %v", err)
	}
	var count int
	if err = reopened.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM family_requests`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ledger count %d: %v", count, err)
	}
}
