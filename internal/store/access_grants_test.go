package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func accessActor(id string) ticket.Identity {
	return ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: id}
}

func accessCount(t *testing.T, db *DB, repoID int64) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM project_access_grants WHERE repo_id=?`, repoID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAccessGrantRefusalsStoreNothing(t *testing.T) {
	db, conn, appt := approverFixture(t)
	if _, err := db.SetApprover(t.Context(), appt, conn.Revision, 0); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		actor ticket.Identity
		text  string
		want  error
	}{
		"blank":        {accessActor("1"), " \t\n ", ErrAccessGrantText},
		"empty":        {accessActor("1"), "", ErrAccessGrantText},
		"invalid utf8": {accessActor("1"), "bad \xff text", ErrAccessGrantText},
		"too long":     {accessActor("1"), strings.Repeat("a", MaxAccessGrantText+1), ErrAccessGrantText},
		"approver":     {appt.Actor, "full authority please", ErrAccessGrantApprover},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: appt.RepoID, Actor: tc.actor, Handle: "h", Text: tc.text})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error %v; want %v", err, tc.want)
			}
			if n := accessCount(t, db, appt.RepoID); n != 0 {
				t.Fatalf("%d rows stored after refusal", n)
			}
		})
	}
	// The limit itself is accepted, and multi-byte text counts bytes.
	if _, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: appt.RepoID, Actor: accessActor("1"), Handle: "h", Text: strings.Repeat("a", MaxAccessGrantText)}); err != nil {
		t.Fatalf("max-size text refused: %v", err)
	}
	if _, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: appt.RepoID, Actor: accessActor("2"), Handle: "h", Text: strings.Repeat("é", MaxAccessGrantText/2+1)}); !errors.Is(err, ErrAccessGrantText) {
		t.Fatalf("over-limit multibyte text: %v", err)
	}
}

func TestAccessGrantRevisionsAndRevoke(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	a, b := accessActor("1"), accessActor("2")
	grant := func(actor ticket.Identity, text string) AccessGrant {
		t.Helper()
		g, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: actor, Handle: "h", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	g1, g2, other := grant(a, "one"), grant(a, "two"), grant(b, "other")
	if g1.Revision != 1 || g2.Revision != 2 || other.Revision != 1 {
		t.Fatalf("revisions %d %d %d", g1.Revision, g2.Revision, other.Revision)
	}
	active, err := db.ListAccessGrants(t.Context(), repo.ID)
	if err != nil || len(active) != 2 || active[0].ID != g2.ID || active[1].ID != other.ID {
		t.Fatalf("active %+v %v", active, err)
	}
	if _, err = db.RevokeAccessGrant(t.Context(), repo.ID, g1.ID); !errors.Is(err, ErrAccessGrantMissing) {
		t.Fatalf("revoking a replaced revision: %v", err)
	}
	if _, err = db.RevokeAccessGrant(t.Context(), repo.ID+1, g2.ID); !errors.Is(err, ErrAccessGrantMissing) {
		t.Fatalf("revoking through another project: %v", err)
	}
	revoked, err := db.RevokeAccessGrant(t.Context(), repo.ID, g2.ID)
	if err != nil || revoked.Open() || revoked.CloseReason != "revoked" || revoked.Text != "two" {
		t.Fatalf("revoke %+v %v", revoked, err)
	}
	if _, err = db.RevokeAccessGrant(t.Context(), repo.ID, g2.ID); !errors.Is(err, ErrAccessGrantMissing) {
		t.Fatalf("second revoke: %v", err)
	}
	// Granting again after a revoke continues the numbering.
	if g := grant(a, "three"); g.Revision != 3 {
		t.Fatalf("revision after revoke %d", g.Revision)
	}
	history, err := db.AccessGrantHistory(t.Context(), repo.ID)
	if err != nil || len(history) != 4 {
		t.Fatalf("history %+v %v", history, err)
	}
	for i, want := range []string{"one", "two", "other", "three"} {
		if history[i].Text != want {
			t.Fatalf("history[%d] text %q; want %q", i, history[i].Text, want)
		}
	}
	if history[0].CloseReason != "replaced" || history[1].CloseReason != "revoked" || !history[3].Open() {
		t.Fatalf("close reasons %+v", history)
	}
}

func TestAccessGrantsAreAppendOnlyAndCascadeWithRepo(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "owner", "repo")
	g, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: accessActor("1"), Handle: "h", Text: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := db.RevokeAccessGrant(t.Context(), repo.ID, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	open, err := db.GrantAccess(t.Context(), AccessGrant{RepoID: repo.ID, Actor: accessActor("2"), Handle: "h", Text: "open"})
	if err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]struct {
		sql  string
		args []any
	}{
		"delete open":        {`DELETE FROM project_access_grants WHERE id=?`, []any{open.ID}},
		"delete closed":      {`DELETE FROM project_access_grants WHERE id=?`, []any{closed.ID}},
		"edit open text":     {`UPDATE project_access_grants SET grant_text='x' WHERE id=?`, []any{open.ID}},
		"edit closed text":   {`UPDATE project_access_grants SET grant_text='x' WHERE id=?`, []any{closed.ID}},
		"reopen closed":      {`UPDATE project_access_grants SET closed_at=NULL,close_reason=NULL WHERE id=?`, []any{closed.ID}},
		"reclose closed":     {`UPDATE project_access_grants SET closed_at='2030-01-01T00:00:00Z',close_reason='replaced' WHERE id=?`, []any{closed.ID}},
		"change revision":    {`UPDATE project_access_grants SET revision=9 WHERE id=?`, []any{open.ID}},
		"close without why":  {`UPDATE project_access_grants SET closed_at='2030-01-01T00:00:00Z' WHERE id=?`, []any{open.ID}},
		"blank text insert":  {`INSERT INTO project_access_grants(repo_id,provider,api_origin,actor_id,handle,revision,grant_text,created_at) VALUES(?,'github','o','9','h',1,'   ','2030-01-01T00:00:00Z')`, []any{repo.ID}},
		"second open insert": {`INSERT INTO project_access_grants(repo_id,provider,api_origin,actor_id,handle,revision,grant_text,created_at) SELECT repo_id,provider,api_origin,actor_id,handle,5,'dup','2030-01-01T00:00:00Z' FROM project_access_grants WHERE id=?`, []any{open.ID}},
	} {
		if _, err := db.sql.ExecContext(t.Context(), stmt.sql, stmt.args...); err == nil {
			t.Errorf("%s: statement succeeded", name)
		}
	}
	if history, err := db.AccessGrantHistory(t.Context(), repo.ID); err != nil || len(history) != 2 || history[0].Text != "keep" || history[1].Text != "open" || !history[1].Open() {
		t.Fatalf("rows changed: %+v %v", history, err)
	}
	// A repository delete still removes its grants.
	if _, err := db.sql.ExecContext(t.Context(), `DELETE FROM repos WHERE id=?`, repo.ID); err != nil {
		t.Fatalf("repo delete blocked: %v", err)
	}
	if n := accessCount(t, db, repo.ID); n != 0 {
		t.Fatalf("%d grants survived repo delete", n)
	}
}

func TestOldRightsTablesAreDropped(t *testing.T) {
	db := newTestDB(t)
	for _, name := range []string{"rights_grants", "reviewer_assignments", "rights_history"} {
		var n int
		if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE name=?`, name).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s still present (%d, %v)", name, n, err)
		}
	}
	var n int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE name='project_access_grants'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("project_access_grants missing (%d, %v)", n, err)
	}
}
