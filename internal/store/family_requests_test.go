package store

import (
	"errors"
	"fmt"
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

// #1366: the ledger scope accepts every ticket provider whose task belongs to
// the scope repository, and still refuses missing providers, foreign
// repositories and tasks without an issue separator, writing no row.
func TestFamilyScopeAcceptsEveryProviderAndRefusesMalformedTasks(t *testing.T) {
	db := newTestDB(t)
	begin := func(t *testing.T, issue int, task, repository string) error {
		t.Helper()
		session := testSession(t, db, issue)
		if _, err := db.sql.ExecContext(t.Context(), `UPDATE sessions SET task_ref=? WHERE id=?`, task, session.ID); err != nil {
			t.Fatal(err)
		}
		session.TaskRef = task
		b, err := db.BindResultAttempt(t.Context(), resultRun(t, db, session, "validate", fmt.Sprintf("scope-%d", issue), int64(issue)))
		if err != nil {
			t.Fatal(err)
		}
		q := FamilyRequest{Scope: FamilyScope{Repository: repository, Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}, RequestID: "scope", Tool: "standards_read", PayloadHash: strings.Repeat("c", 64)}
		_, first, err := db.BeginFamilyRequest(t.Context(), b, q)
		if err == nil && !first {
			t.Fatal("a new request was not elected")
		}
		return err
	}
	rows := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM family_requests`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for i, c := range []struct{ task, repository string }{
		{"linear:a/b#1", "a/b"},
		{"github:o/r#1", "o/r"},
		{"linear:b6e5f992-a8c2-4824-adee-fe247c92ba59/441971ef-e9fc-48d9-93b2-51cf19c3c675#4503599629192685", "b6e5f992-a8c2-4824-adee-fe247c92ba59/441971ef-e9fc-48d9-93b2-51cf19c3c675"},
	} {
		before := rows(t)
		if err := begin(t, 900+i, c.task, c.repository); err != nil {
			t.Fatalf("%s refused: %v", c.task, err)
		}
		if rows(t) != before+1 {
			t.Fatalf("%s wrote no ledger row", c.task)
		}
	}
	for i, c := range []struct{ task, repository string }{
		{":o/r#1", "o/r"},
		{"o/r#1", "o/r"},
		{"linear:a/b#2", "a/c"},
		{"github:o/r#2", "o/other"},
		{"linear:a/b1", "a/b"},
		{"linear:a/b", "a/b"},
	} {
		before := rows(t)
		if err := begin(t, 950+i, c.task, c.repository); err == nil {
			t.Fatalf("task %q with repository %q accepted", c.task, c.repository)
		}
		if rows(t) != before {
			t.Fatalf("refused task %q wrote a ledger row", c.task)
		}
	}
}
