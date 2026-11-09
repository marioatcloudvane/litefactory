package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func accessSource(id string) ticket.Identity {
	return ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "comment-" + id}
}

func beginDecision(t *testing.T, db *DB, repoID int64, actor ticket.Identity, source string) AccessDecision {
	t.Helper()
	d, created, err := db.BeginAccessDecision(t.Context(), AccessDecision{RepoID: repoID, SessionID: 7, Source: accessSource(source), Actor: actor, Handle: "h", TargetJSON: `{"kind":"issue"}`, RequestDigest: "digest-" + source})
	if err != nil || !created {
		t.Fatalf("begin %s: %+v %v %v", source, d, created, err)
	}
	return d
}

func TestAccessDecisionBeginFinishReplay(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	actor := accessActor("1")
	g, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: actor, Handle: "h", Text: "may label issues"})
	if err != nil {
		t.Fatal(err)
	}
	in := AccessDecision{RepoID: repo.ID, SessionID: 7, Source: accessSource("1"), CoveredThrough: "comment-3", Actor: actor, Handle: "h",
		GrantID: g.ID, GrantRevision: g.Revision, TargetJSON: `{"kind":"issue"}`, RequestDigest: "d1", State: AccessDecisionFinal, Decision: AccessAllow, Reason: "ignored"}
	d, created, err := db.BeginAccessDecision(t.Context(), in)
	if err != nil || !created || d.State != AccessDecisionPending || d.Decision != "" || d.Reason != "" || !d.DecidedAt.IsZero() ||
		d.GrantID != g.ID || d.GrantRevision != g.Revision || d.CoveredThrough != "comment-3" || d.CreatedAt.IsZero() {
		t.Fatalf("begin %+v %v %v", d, created, err)
	}
	replay, created, err := db.BeginAccessDecision(t.Context(), AccessDecision{RepoID: repo.ID, SessionID: 8, Source: accessSource("1"), Actor: accessActor("2"), Handle: "x", RequestDigest: "other"})
	if err != nil || created || replay != d {
		t.Fatalf("pending replay %+v %v %v", replay, created, err)
	}
	for name, out := range map[string]AccessDecisionOutcome{
		"unknown decision": {Decision: "maybe", Reason: "r"},
		"blank reason":     {Decision: AccessAllow, Reason: "  \t "},
		"two lines":        {Decision: AccessAllow, Reason: "one\ntwo"},
		"too long":         {Decision: AccessAllow, Reason: strings.Repeat("é", MaxAccessDecisionReason+1)},
	} {
		if _, err := db.FinishAccessDecision(t.Context(), d.ID, out); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := db.FinishAccessDecision(t.Context(), d.ID+100, AccessDecisionOutcome{Decision: AccessDeny, Reason: "r"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finish missing: %v", err)
	}
	final, err := db.FinishAccessDecision(t.Context(), d.ID, AccessDecisionOutcome{Decision: AccessAllow, Reason: "  " + strings.Repeat("a", MaxAccessDecisionReason) + " ", Model: "m", Classified: true})
	if err != nil || final.State != AccessDecisionFinal || final.Decision != AccessAllow || final.Reason != strings.Repeat("a", MaxAccessDecisionReason) ||
		final.Model != "m" || !final.Classified || final.DecidedAt.IsZero() || final.RequestDigest != "d1" {
		t.Fatalf("finish %+v %v", final, err)
	}
	if _, err := db.FinishAccessDecision(t.Context(), d.ID, AccessDecisionOutcome{Decision: AccessDeny, Reason: "again"}); !errors.Is(err, ErrAccessDecisionFinal) {
		t.Fatalf("second finish: %v", err)
	}
	replay, created, err = db.BeginAccessDecision(t.Context(), in)
	if err != nil || created || replay != final {
		t.Fatalf("final replay %+v %v %v", replay, created, err)
	}
	if got, err := db.GetAccessDecision(t.Context(), d.ID); err != nil || got != final {
		t.Fatalf("get %+v %v", got, err)
	}
	if got, err := db.AccessDecisionBySource(t.Context(), repo.ID, accessSource("1")); err != nil || got != final {
		t.Fatalf("by source %+v %v", got, err)
	}
	if _, err := db.GetAccessDecision(t.Context(), d.ID+100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if _, err := db.AccessDecisionBySource(t.Context(), repo.ID, accessSource("9")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("by source missing: %v", err)
	}
	// No grant is stored as NULL and read back as zero.
	none := beginDecision(t, db, repo.ID, actor, "2")
	if none.GrantID != 0 || none.GrantRevision != 0 || none.CoveredThrough != "" {
		t.Fatalf("no grant %+v", none)
	}
	for name, bad := range map[string]AccessDecision{
		"no repo":    {SessionID: 1, Source: accessSource("x"), Actor: actor, RequestDigest: "d"},
		"no session": {RepoID: repo.ID, Source: accessSource("x"), Actor: actor, RequestDigest: "d"},
		"no source":  {RepoID: repo.ID, SessionID: 1, Actor: actor, RequestDigest: "d"},
		"no actor":   {RepoID: repo.ID, SessionID: 1, Source: accessSource("x"), RequestDigest: "d"},
		"no digest":  {RepoID: repo.ID, SessionID: 1, Source: accessSource("x"), Actor: actor},
	} {
		if _, _, err := db.BeginAccessDecision(t.Context(), bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestAccessDecisionRowsAreGuarded(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	pending := beginDecision(t, db, repo.ID, accessActor("1"), "1")
	final := beginDecision(t, db, repo.ID, accessActor("1"), "2")
	if _, err := db.FinishAccessDecision(t.Context(), final.ID, AccessDecisionOutcome{Decision: AccessDeny, Reason: "no grant"}); err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]struct {
		sql string
		id  int64
	}{
		"edit final decision":   {`UPDATE project_access_decisions SET decision='allow' WHERE id=?`, final.ID},
		"edit final reason":     {`UPDATE project_access_decisions SET reason='other' WHERE id=?`, final.ID},
		"reopen final":          {`UPDATE project_access_decisions SET state='pending',decision=NULL,reason=NULL,decided_at=NULL WHERE id=?`, final.ID},
		"edit pending digest":   {`UPDATE project_access_decisions SET request_digest='x' WHERE id=?`, pending.ID},
		"edit pending grant":    {`UPDATE project_access_decisions SET grant_id=5 WHERE id=?`, pending.ID},
		"pending stays pending": {`UPDATE project_access_decisions SET model='m' WHERE id=?`, pending.ID},
		"finish without reason": {`UPDATE project_access_decisions SET state='final',decision='allow',decided_at='2030-01-01T00:00:00.000Z' WHERE id=?`, pending.ID},
		"finish with digest":    {`UPDATE project_access_decisions SET state='final',decision='allow',reason='r',decided_at='2030-01-01T00:00:00.000Z',request_digest='x' WHERE id=?`, pending.ID},
		"delete pending":        {`DELETE FROM project_access_decisions WHERE id=?`, pending.ID},
		"delete final":          {`DELETE FROM project_access_decisions WHERE id=?`, final.ID},
	} {
		if _, err := db.sql.ExecContext(t.Context(), stmt.sql, stmt.id); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if got, err := db.GetAccessDecision(t.Context(), final.ID); err != nil || got.Decision != AccessDeny || got.Reason != "no grant" {
		t.Fatalf("final changed %+v %v", got, err)
	}
	if got, err := db.GetAccessDecision(t.Context(), pending.ID); err != nil || got != pending {
		t.Fatalf("pending changed %+v %v", got, err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM repos WHERE id=?`, repo.ID); err != nil {
		t.Fatalf("repo delete: %v", err)
	}
	if _, err := db.GetAccessDecision(t.Context(), final.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("decision survived repo delete: %v", err)
	}
}

func TestOpenAccessGrant(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	actor := accessActor("1")
	if _, err := db.OpenAccessGrant(t.Context(), repo.ID, actor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no grant: %v", err)
	}
	if _, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: actor, Handle: "h", Text: "one"}); err != nil {
		t.Fatal(err)
	}
	g2, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: actor, Handle: "h", Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.OpenAccessGrant(t.Context(), repo.ID, actor); err != nil || got.ID != g2.ID || got.Text != "two" || !got.Open() {
		t.Fatalf("open grant %+v %v", got, err)
	}
	if _, err := db.OpenAccessGrant(t.Context(), repo.ID, accessActor("2")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other actor: %v", err)
	}
	if _, err := db.RevokeAccessGrant(t.Context(), repo.ID, g2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenAccessGrant(t.Context(), repo.ID, actor); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke: %v", err)
	}
}

func TestCountAccessChecks(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	other := testRepo(t, db, "owner", "other")
	a, b := accessActor("1"), accessActor("2")
	since := nowUTC().Add(-time.Minute)
	finish := func(d AccessDecision, decision string, classified bool) {
		t.Helper()
		if _, err := db.FinishAccessDecision(t.Context(), d.ID, AccessDecisionOutcome{Decision: decision, Reason: "r", Classified: classified}); err != nil {
			t.Fatal(err)
		}
	}
	pending := beginDecision(t, db, repo.ID, a, "1")
	classified := beginDecision(t, db, repo.ID, a, "2")
	finish(classified, AccessAllow, true)
	finish(beginDecision(t, db, repo.ID, a, "3"), AccessFloor, false)
	finish(beginDecision(t, db, repo.ID, a, "4"), AccessRateLimited, false)
	beginDecision(t, db, repo.ID, b, "5")
	beginDecision(t, db, other.ID, a, "6")
	count := func(since time.Time, exclude int64) int {
		t.Helper()
		n, err := db.CountAccessChecks(t.Context(), repo.ID, a, since, exclude)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(since, 0); n != 2 {
		t.Fatalf("count %d; want 2", n)
	}
	if n := count(since, pending.ID); n != 1 {
		t.Fatalf("count excluding pending %d; want 1", n)
	}
	if n := count(since, classified.ID); n != 1 {
		t.Fatalf("count excluding classified %d; want 1", n)
	}
	if n := count(nowUTC().Add(time.Minute), 0); n != 0 {
		t.Fatalf("count after window %d; want 0", n)
	}
	if n, err := db.CountAccessChecks(t.Context(), repo.ID, b, since, 0); err != nil || n != 1 {
		t.Fatalf("other actor %d %v", n, err)
	}
}
