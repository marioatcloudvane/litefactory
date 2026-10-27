package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func usageConsentFixture(t *testing.T, db *DB, id, scope string, targets []ConnectorUsageTarget) (ConnectorBrowser, ConnectorGrant, ConnectorCode, ConnectorExchange) {
	t.Helper()
	b, g, c, x := discussionConsentFixture(t, db, id, nil)
	b.Scope = scope
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE connector_browsers SET scope=? WHERE id=?`, scope, b.ID); err != nil {
		t.Fatal(err)
	}
	g.Scope, c.Scope, x.Scope = scope, scope, scope
	g.UsageTargets = targets
	return b, g, c, x
}

func TestConnectorUsageConsentAtomicAndIndependent(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	session := testSession(t, db, 7)
	repo, err := db.ResolveConnectorUsageTarget(t.Context(), connectorRepositoryUsageScope, "github:acme/api")
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.ResolveConnectorUsageTarget(t.Context(), connectorTaskUsageScope, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	scope := connectorFactoryUsageScope + " " + connectorRepositoryUsageScope + " " + connectorTaskUsageScope
	b, g, c, x := usageConsentFixture(t, db, "usage", scope, []ConnectorUsageTarget{repo, task})
	if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorFactoryUsageScope, "", "", connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("before consent: %v", err)
	}
	for _, bad := range [][]ConnectorUsageTarget{nil, {repo}, {repo, repo}, {repo, {Scope: connectorTaskUsageScope, TaskRef: session.TaskRef, SessionID: session.ID + 1}}} {
		g.UsageTargets = bad
		if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err == nil {
			t.Fatal("invalid targets allowed")
		}
		var count int
		if err = db.sql.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM connector_grants)+(SELECT count(*) FROM connector_codes)+(SELECT count(*) FROM connector_usage_targets)`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial consent %d %v", count, err)
		}
	}
	g.UsageTargets = []ConnectorUsageTarget{repo, task}
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	issued, err := db.ExchangeConnectorCode(t.Context(), x)
	if err != nil || !reflect.DeepEqual(issued.UsageTargets, g.UsageTargets) {
		t.Fatalf("issued targets %+v %v", issued.UsageTargets, err)
	}
	for _, selection := range []struct{ scope, repo, task string }{{connectorFactoryUsageScope, "", ""}, {connectorRepositoryUsageScope, repo.RepositoryRef, ""}, {connectorTaskUsageScope, task.RepositoryRef, task.TaskRef}} {
		if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, selection.scope, selection.repo, selection.task, connectorTestNow); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, task.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("usage gained discussion: %v", err)
	}
	if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, task.RepositoryRef, "github:acme/api#8", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("sibling allowed: %v", err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE connector_usage_targets SET task_ref='other' WHERE grant_id=?`, g.ID); err == nil {
		t.Fatal("target mutation allowed")
	}
	x.Digest = x.Pair.RefreshDigest
	x.Pair = ConnectorTokenPair{AccessDigest: "renewed-usage", RefreshDigest: "renewed-usage-refresh", AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}
	refreshed, err := db.RotateConnectorRefresh(t.Context(), x)
	if err != nil || !reflect.DeepEqual(refreshed.UsageTargets, g.UsageTargets) {
		t.Fatalf("refresh %+v %v", refreshed.UsageTargets, err)
	}
}

func TestConnectorUsageTargetLifecycle(t *testing.T) {
	for _, scenario := range []string{"target deleted", "session deleted", "session rebound", "repository rebound", "expiry", "revoked", "owner generation"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			connectorTestOwner(t, db, 42)
			connectorTestClient(t, db, "client")
			session := testSession(t, db, 7)
			target, err := db.ResolveConnectorUsageTarget(t.Context(), connectorTaskUsageScope, session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			b, g, c, x := usageConsentFixture(t, db, "usage", connectorTaskUsageScope+" "+connectorFactoryUsageScope, []ConnectorUsageTarget{target})
			if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExchangeConnectorCode(t.Context(), x); err != nil {
				t.Fatal(err)
			}
			now := connectorTestNow
			switch scenario {
			case "target deleted":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM connector_usage_targets WHERE grant_id=?`, g.ID)
			case "session deleted":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE id=?`, session.ID)
			case "session rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET task_ref='github:acme/api#8' WHERE id=?`, session.ID)
			case "repository rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE repos SET owner='other' WHERE id=?`, session.RepoID)
			case "expiry":
				now = now.Add(24 * time.Hour)
			case "revoked":
				err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, now)
			case "owner generation":
				connectorTestOwner(t, db, 99)
				connectorTestOwner(t, db, 42)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, target.RepositoryRef, target.TaskRef, now); !errors.Is(err, ErrConnectorInvalid) {
				t.Fatalf("allowed %v", err)
			}
			if scenario == "target deleted" || scenario == "session deleted" || scenario == "session rebound" || scenario == "repository rebound" {
				if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorFactoryUsageScope, "", "", now); err != nil {
					t.Fatalf("independent factory access lost: %v", err)
				}
			}
		})
	}
}

func TestConnectorUsageMigrationPreservesGrants(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "prior.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "connector_usage"))
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	x := connectorTestFamily(t, db, "client", "old")
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, x.Pair.AccessDigest, true)
	if _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorFactoryUsageScope, "", "", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("old grant gained usage: %v", err)
	}
}

func TestConnectorUsageDiscoveryOmitsInvalidTargetsOnly(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	session := testSession(t, db, 7)
	repository, err := db.ResolveConnectorUsageTarget(t.Context(), connectorRepositoryUsageScope, "github:acme/api")
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.ResolveConnectorUsageTarget(t.Context(), connectorTaskUsageScope, session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	b, g, c, x := usageConsentFixture(t, db, "discovery", connectorRepositoryUsageScope+" "+connectorTaskUsageScope, []ConnectorUsageTarget{repository, task})
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []int{2, 1, 0} {
		grant, err := db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "", connectorTestNow)
		if err != nil || len(grant.UsageTargets) != expected {
			t.Fatalf("discovery targets %+v %v", grant.UsageTargets, err)
		}
		if expected == 2 {
			_, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE id=?`, session.ID)
		}
		if expected == 1 {
			_, err = db.sql.ExecContext(t.Context(), `UPDATE repos SET owner='other' WHERE id=?`, repository.RepoID)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}
