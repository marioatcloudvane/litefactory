package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestConnectorUsageScopesAreIndependent(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	session := testSession(t, db, 7)
	_, x := grantCoverage(t, db, "usage", connectorFactoryUsageScope+" "+connectorRepositoryUsageScope, &ConnectorCoverage{All: true})
	for _, selection := range []struct{ scope, repo, task string }{{connectorFactoryUsageScope, "", ""}, {connectorRepositoryUsageScope, "github:acme/api", ""}} {
		if _, _, err := db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, selection.scope, selection.repo, selection.task, connectorTestNow); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, "github:acme/api", session.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("repository usage gained task usage: %v", err)
	}
	if _, _, err := db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, session.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("usage gained discussion: %v", err)
	}
	// factory:usage alone needs no repository coverage.
	grantCoverage(t, db, "factory", connectorFactoryUsageScope, nil)
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
	access := legacyConnectorGrant(t, db, "old", connectorTestScope)
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, access, true)
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), access, 42, connectorTestResource, connectorFactoryUsageScope, "", "", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("old grant gained usage: %v", err)
	}
}
