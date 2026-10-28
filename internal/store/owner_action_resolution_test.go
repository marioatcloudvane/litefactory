package store

import (
	"errors"
	"testing"
	"time"
)

func TestOwnerActionResolutionRequiresCurrentSelectedRepository(t *testing.T) {
	db := newTestDB(t)
	owner := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	selected := testRepository(t, db, "owner", "selected")
	other := testRepository(t, db, "owner", "other")
	b, g, c, _ := coverageConsentFixture(t, db, "resolution", "repository:actions", nil)
	g.ActionCoverage = &ConnectorCoverage{Repositories: []ConnectorRepository{selected}}
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	p := OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: g.ID}
	at := connectorTestNow
	ctx := WithOwnerActionResolution(t.Context(), p, "repository:actions", func() time.Time { return at })
	if err := db.CheckOwnerActionResolution(t.Context(), other.RepoID, other.RepositoryRef); err != nil {
		t.Fatalf("legacy resolution unexpectedly changed: %v", err)
	}
	if err := db.CheckOwnerActionResolution(ctx, selected.RepoID, selected.RepositoryRef); err != nil {
		t.Fatalf("selected repository refused: %v", err)
	}
	if err := db.CheckOwnerActionResolution(ctx, other.RepoID, other.RepositoryRef); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("uncovered repository content authorized: %v", err)
	}
	if err := db.CheckOwnerActionResolution(ctx, selected.RepoID, "github:owner/replacement"); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("replacement repository identity authorized: %v", err)
	}
	at = g.ExpiresAt
	if err := db.CheckOwnerActionResolution(ctx, selected.RepoID, selected.RepositoryRef); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("stale resolution clock authorized expired grant: %v", err)
	}
	at = connectorTestNow
	if err := db.RevokeConnectorGrant(t.Context(), 42, g.ID, at); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckOwnerActionResolution(ctx, selected.RepoID, selected.RepositoryRef); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked grant authorized repository read: %v", err)
	}
}

func TestOwnerActionResolutionRefusesMissingClockOrScope(t *testing.T) {
	db := newTestDB(t)
	for _, ctx := range []struct {
		scope string
		clock func() time.Time
	}{{scope: "repository:actions"}, {clock: func() time.Time { return connectorTestNow }}} {
		if err := db.CheckOwnerActionResolution(WithOwnerActionResolution(t.Context(), OwnerActionPrincipal{}, ctx.scope, ctx.clock), 0, ""); err == nil {
			t.Fatal("incomplete resolution authority accepted")
		}
	}
}

func TestOwnerActionResolutionRepositoryListingFiltersConsent(t *testing.T) {
	db := newTestDB(t)
	owner := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	selected := testRepository(t, db, "owner", "selected")
	_ = testRepository(t, db, "owner", "other")
	b, g, c, _ := coverageConsentFixture(t, db, "listing", "repository:actions", nil)
	g.ActionCoverage = &ConnectorCoverage{Repositories: []ConnectorRepository{selected}}
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	p := OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: g.ID}
	ctx := WithOwnerActionResolution(t.Context(), p, "repository:actions", func() time.Time { return connectorTestNow })
	listed, err := db.ListOwnerActionResolutionRepositories(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != selected.RepoID {
		t.Fatalf("inference disclosed uncovered repositories: %+v %v", listed, err)
	}
	legacy, err := db.ListOwnerActionResolutionRepositories(t.Context())
	if err != nil || len(legacy) != 2 {
		t.Fatalf("legacy repository listing changed: %+v %v", legacy, err)
	}
	if err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ListOwnerActionResolutionRepositories(ctx); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked inference authority accepted: %v", err)
	}
}

func TestOwnerActionResolutionStandardListingRequiresReadCoverage(t *testing.T) {
	db := newTestDB(t)
	owner := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	selected := testRepository(t, db, "owner", "selected")
	_ = testRepository(t, db, "owner", "other")
	b, g, c, _ := coverageConsentFixture(t, db, "standards-listing", "standards:management task:discussion", &ConnectorCoverage{Repositories: []ConnectorRepository{selected}})
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	p := OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: g.ID}
	listed, err := db.ListOwnerActionResolutionRepositories(WithOwnerActionResolution(t.Context(), p, "standards:management", func() time.Time { return connectorTestNow }))
	if err != nil || len(listed) != 1 || listed[0].ID != selected.RepoID {
		t.Fatalf("standards management broadened context reads: %+v %v", listed, err)
	}
	b, g, c, _ = coverageConsentFixture(t, db, "standards-only", "standards:management", nil)
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	p.GrantID = g.ID
	listed, err = db.ListOwnerActionResolutionRepositories(WithOwnerActionResolution(t.Context(), p, "standards:management", func() time.Time { return connectorTestNow }))
	if err != nil || len(listed) != 0 {
		t.Fatalf("global standards consent implied repository reads: %+v %v", listed, err)
	}
}

func TestOwnerActionResolutionEmptyListingStillChecksAuthority(t *testing.T) {
	db := newTestDB(t)
	owner := connectorTestOwner(t, db, 42)
	p := OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "absent", GrantID: "absent"}
	if _, err := db.ListOwnerActionResolutionRepositories(WithOwnerActionResolution(t.Context(), p, "repository:actions", func() time.Time { return connectorTestNow })); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("empty database bypassed authority: %v", err)
	}
}
