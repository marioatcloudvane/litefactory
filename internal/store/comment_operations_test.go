package store

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

func TestCommentReservationCrossConnectionAndReopen(t *testing.T) {
	db := newTestDB(t)
	session := testSession(t, db, 704)
	b, err := db.BindResultAttempt(t.Context(), resultRun(t, db, session, "research", "comment-attempt", 1))
	if err != nil {
		t.Fatal(err)
	}
	scope := FamilyScope{Repository: strings.TrimPrefix(strings.Split(session.TaskRef, "#")[0], "github:"), Task: b.Task, RunID: b.RunID, Attempt: b.Attempt}
	request := FamilyRequest{Scope: scope, RequestID: "comment", Tool: "issues_comment", PayloadHash: strings.Repeat("a", 64)}
	if _, _, err = db.BeginFamilyRequest(t.Context(), b, request); err != nil {
		t.Fatal(err)
	}
	other, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	op := CommentOperation{Scope: scope, RequestID: request.RequestID, IssueNumber: 284, BotID: 9, Body: "prepared exact marker content"}
	var owners atomic.Int32
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			conn := db
			if i%2 == 1 {
				conn = other
			}
			_, first, e := conn.ReserveCommentDispatch(t.Context(), b, op)
			if e != nil {
				var sqliteErr *sqlite.Error
				if !errors.As(e, &sqliteErr) || sqliteErr.Code()&0xff != 5 {
					t.Error(e)
				}
				// Separate SQLite connections may fail closed with BUSY while
				// another transaction wins. Such a failure grants no dispatch.
				if first {
					t.Error("failed reservation granted dispatch")
				}
			}
			if first {
				owners.Add(1)
			}
		})
	}
	wg.Wait()
	if owners.Load() != 1 {
		t.Fatalf("dispatch owners %d", owners.Load())
	}
	for _, conn := range []*DB{db, other} {
		got, first, e := conn.ReserveCommentDispatch(t.Context(), b, op)
		if e != nil || first || got.State != "uncertain" {
			t.Fatalf("retry after contention %+v %v %v", got, first, e)
		}
	}

	for _, mutate := range []func(*CommentOperation){func(o *CommentOperation) { o.Body += "changed" }, func(o *CommentOperation) { o.IssueNumber++ }, func(o *CommentOperation) { o.BotID++ }, func(o *CommentOperation) { o.Scope.Repository = "foreign/repo" }} {
		changed := op
		mutate(&changed)
		if _, first, e := db.ReserveCommentDispatch(t.Context(), b, changed); e == nil || first {
			t.Fatalf("changed intent accepted: %v", e)
		}
	}
	if err = db.ConfirmCommentOperation(t.Context(), op, 555); err != nil {
		t.Fatal(err)
	}
	if err = db.ConfirmCommentOperation(t.Context(), op, 556); !errors.Is(err, ErrResultCorrelation) {
		t.Fatalf("identity overwritten: %v", err)
	}
	reopened, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, first, err := reopened.ReserveCommentDispatch(t.Context(), b, op)
	if err != nil || first || got.State != "confirmed" || got.CommentID != 555 {
		t.Fatalf("reopened reservation %+v %v %v", got, first, err)
	}
	foreign := scope
	foreign.RunID++
	if _, err = reopened.GetCommentOperation(t.Context(), foreign, op.RequestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-run read %v", err)
	}
	// Without an admitted matching-family request there is no dispatch right.
	missing := op
	missing.RequestID = "missing"
	if _, first, err = reopened.ReserveCommentDispatch(t.Context(), b, missing); err == nil || first {
		t.Fatal("unadmitted dispatch accepted")
	}
}
