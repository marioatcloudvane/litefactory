package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func discussionConsentFixture(t *testing.T, db *DB, id string, target *ConnectorGrantTarget) (ConnectorBrowser, ConnectorGrant, ConnectorCode, ConnectorExchange) {
	t.Helper()
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	scope := connectorTestScope + " " + connectorDiscussionScope
	b := ConnectorBrowser{ID: "browser-" + id, ClientID: "client", RedirectURI: "https://client.test/callback", Resource: connectorTestResource, Scope: scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), b, 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	g := ConnectorGrant{ID: id, ClientID: b.ClientID, Resource: b.Resource, Scope: scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: connectorTestNow, ExpiresAt: connectorTestNow.Add(24 * time.Hour), Target: target}
	c := ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: connectorTestNow.Add(5 * time.Minute)}
	x := ConnectorExchange{Digest: c.Digest, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, OwnerID: 42, Now: connectorTestNow, Pair: ConnectorTokenPair{AccessDigest: "access-" + id, RefreshDigest: "refresh-" + id, AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}}
	return b, g, c, x
}

func TestConnectorDiscussionConsentAtomicAndImmutable(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	session := testSession(t, db, 7)
	target, err := db.ResolveConnectorDiscussionTarget(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	b, g, c, x := discussionConsentFixture(t, db, "grant", &target)
	if _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, session.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("before Allow: %v", err)
	}
	// Invalid task and missing binding roll back browser consumption, grant and code.
	for _, bad := range []*ConnectorGrantTarget{nil, {TaskRef: "github:acme/api#999"}, {TaskRef: target.TaskRef, SessionID: target.SessionID + 1}} {
		g.Target = bad
		if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err == nil {
			t.Fatal("invalid target allowed")
		}
		var grants, codes, targets int
		if err = db.sql.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM connector_grants),(SELECT count(*) FROM connector_codes),(SELECT count(*) FROM connector_grant_targets)`).Scan(&grants, &codes, &targets); err != nil {
			t.Fatal(err)
		}
		if grants+codes+targets != 0 {
			t.Fatal("failed consent left partial authorization")
		}
	}
	g.Target = &target
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	issued, err := db.ExchangeConnectorCode(t.Context(), x)
	if err != nil || !reflect.DeepEqual(issued.Target, &target) {
		t.Fatalf("issued target: %+v %v", issued.Target, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE connector_grant_targets SET task_ref='github:other/repo#8' WHERE grant_id=?`, g.ID); err == nil {
		t.Fatal("target update allowed")
	}
	if _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "github:acme/api#8", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("wrong task: %v", err)
	}
	x.Digest = x.Pair.RefreshDigest
	x.Pair = ConnectorTokenPair{AccessDigest: "renewed", RefreshDigest: "renewed-refresh", AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}
	refreshed, err := db.RotateConnectorRefresh(t.Context(), x)
	if err != nil || !reflect.DeepEqual(refreshed.Target, &target) {
		t.Fatalf("refresh target: %+v %v", refreshed.Target, err)
	}
	if err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CheckConnectorDiscussionAccess(t.Context(), "renewed", 42, connectorTestResource, session.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked access: %v", err)
	}
}

func TestConnectorDiscussionTargetLifecycleFailsClosed(t *testing.T) {
	for _, scenario := range []string{"deleted target", "deleted session", "session rebound", "repository rebound", "expiry", "owner generation"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			connectorTestOwner(t, db, 42)
			connectorTestClient(t, db, "client")
			session := testSession(t, db, 7)
			target, err := db.ResolveConnectorDiscussionTarget(t.Context(), session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			b, g, c, x := discussionConsentFixture(t, db, "grant", &target)
			if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExchangeConnectorCode(t.Context(), x); err != nil {
				t.Fatal(err)
			}
			now := connectorTestNow
			switch scenario {
			case "deleted target":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM connector_grant_targets WHERE grant_id=?`, g.ID)
			case "deleted session":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE id=?`, session.ID)
			case "session rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET task_ref='github:acme/api#8' WHERE id=?`, session.ID)
			case "repository rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE repos SET owner='another' WHERE id=?`, session.RepoID)
			case "expiry":
				now = now.Add(24 * time.Hour)
			case "owner generation":
				connectorTestOwner(t, db, 99)
				connectorTestOwner(t, db, 42)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, session.TaskRef, now); !errors.Is(err, ErrConnectorInvalid) {
				t.Fatalf("%s allowed: %v", scenario, err)
			}
			// Losing discussion's target does not revoke independently consented overview.
			if scenario == "deleted target" || scenario == "deleted session" || scenario == "session rebound" || scenario == "repository rebound" {
				if _, err = db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTestScope, now); err != nil {
					t.Fatalf("overview denied: %v", err)
				}
			}
		})
	}
}

func TestConnectorDiscussionMigrationPreservesExistingGrants(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "prior.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "connector_discussion"))
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	x := connectorTestFamily(t, db, "client", "old")
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, x.Pair.AccessDigest, true)
	if _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "github:acme/api#7", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("old grant gained discussion: %v", err)
	}
	var targets int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM connector_grant_targets`).Scan(&targets); err != nil || targets != 0 {
		t.Fatalf("migration targets: %d %v", targets, err)
	}
}
