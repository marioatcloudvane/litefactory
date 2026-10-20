package store

import (
	"errors"
	"testing"
	"time"
)

func TestPriorRunsScopeBeforeLimitAndFrozenCutoff(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	allowed := testSession(t, db, 200)
	denied := testSession(t, db, 201)
	first := testRun(t, db, allowed.ID, 1)
	if _, err := db.FinishRun(t.Context(), first.ID, RunOutcome{Status: RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	hidden := testRun(t, db, denied.ID, 2)
	if _, err := db.FinishRun(t.Context(), hidden.ID, RunOutcome{Status: RunFailed}); err != nil {
		t.Fatal(err)
	}
	_ = testRun(t, db, allowed.ID, 3)
	cutoff := time.Now().UTC().Add(time.Second)
	scope := PriorRunScope{RepositoryIDs: []int64{allowed.RepoID}}
	high, err := db.PriorRunCutoff(t.Context(), scope, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	q := PriorRunQuery{Scope: scope, Cutoff: cutoff, HighWater: high, Limit: 1}
	page, err := db.ListPriorRuns(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != first.ID || page[0].TaskRef != allowed.TaskRef {
		t.Fatalf("page: %+v", page)
	}
	if _, err := db.GetPriorRun(t.Context(), q, hidden.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("denied lookup: %v", err)
	}
	q.Scope = PriorRunScope{}
	page, err = db.ListPriorRuns(t.Context(), q)
	if err != nil || len(page) != 0 {
		t.Fatalf("empty grant: %+v %v", page, err)
	}
	q.Scope = scope
	q.HighWater = first.ID - 1
	page, err = db.ListPriorRuns(t.Context(), q)
	if err != nil || len(page) != 0 {
		t.Fatalf("watermark: %+v %v", page, err)
	}
}
