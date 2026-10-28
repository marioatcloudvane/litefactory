package store

import (
	"errors"
	"testing"
	"time"
)

func TestConnectorActionConsentAndRefreshKeepFiniteCoverage(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	repo := testRepository(t, db, "owner", "action")
	b, g, c, x := coverageConsentFixture(t, db, "actions", "task:discussion repository:actions", &ConnectorCoverage{All: true})
	g.ActionCoverage = &ConnectorCoverage{Repositories: []ConnectorRepository{repo}}
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	got, err := db.ExchangeConnectorCode(t.Context(), x)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Coverage.All || got.ActionCoverage.All || !got.ActionCoverage.Covers(repo.RepoID, repo.RepositoryRef) {
		t.Fatalf("mixed grant lost independent coverage: %+v", got)
	}
	later := testRepository(t, db, "owner", "later")
	if got.ActionCoverage.Covers(later.RepoID, later.RepositoryRef) || !got.Coverage.Covers(later.RepoID, later.RepositoryRef) {
		t.Fatal("new repository changed action coverage")
	}
	x.Digest = x.Pair.RefreshDigest
	x.Pair = ConnectorTokenPair{AccessDigest: "new-access", RefreshDigest: "new-refresh", AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}
	refreshed, err := db.RotateConnectorRefresh(t.Context(), x)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ID != g.ID || !refreshed.ActionCoverage.Covers(repo.RepoID, repo.RepositoryRef) {
		t.Fatal("refresh changed action authority")
	}
	accessed, err := db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, x.Resource, "repository:actions", connectorTestNow)
	if err != nil || !accessed.ActionCoverage.Covers(repo.RepoID, repo.RepositoryRef) {
		t.Fatalf("fresh token access: %+v %v", accessed, err)
	}
	if err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, x.Resource, "repository:actions", connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked action access: %v", err)
	}
}

func TestConnectorActionConsentRefusesAllAndMissingSelections(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	b, g, c, _ := coverageConsentFixture(t, db, "selection", "repository:actions", nil)
	for _, coverage := range []*ConnectorCoverage{nil, {All: true}, {Repositories: []ConnectorRepository{{RepositoryRef: "github:absent/repo"}}}} {
		g.ActionCoverage = coverage
		if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err == nil {
			t.Fatal("non-finite or absent repository accepted")
		}
	}
	repo := testRepository(t, db, "owner", "selected")
	g.ActionCoverage = &ConnectorCoverage{Repositories: []ConnectorRepository{repo}}
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal("failed consent consumed browser:", err)
	}
}
