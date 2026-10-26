package store

import (
	"errors"
	"testing"
	"time"
)

func TestLocalConnectorInspectionAndRevocation(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	first := connectorTestFamily(t, db, "a", "first")
	second := connectorTestFamily(t, db, "a", "second")
	rotated := first
	rotated.Pair = ConnectorTokenPair{AccessDigest: "access-descendant", RefreshDigest: "refresh-descendant", AccessExpiresAt: connectorTestNow.Add(10 * time.Minute)}
	if _, err := db.RotateConnectorRefresh(t.Context(), rotated); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListLocalConnectorGrants(t.Context(), connectorTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "first" || rows[0].Status != "active" || rows[0].AccessTokenCount != 2 || !rows[0].EarliestAccessExpiry.Equal(rotated.Pair.AccessExpiresAt) || !rows[0].LatestAccessExpiry.Equal(first.Pair.AccessExpiresAt) {
		t.Fatalf("unexpected inspection: %+v", rows)
	}
	n, err := db.RevokeLocalConnectorGrants(t.Context(), "first", false, connectorTestNow)
	if err != nil || n != 1 {
		t.Fatalf("revoke: %d %v", n, err)
	}
	connectorTestAccess(t, db, first.Pair.AccessDigest, false)
	connectorTestAccess(t, db, rotated.Pair.AccessDigest, false)
	connectorTestAccess(t, db, second.Pair.AccessDigest, true)
	if _, err = db.RotateConnectorRefresh(t.Context(), rotated); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("refresh survived: %v", err)
	}
	n, err = db.RevokeLocalConnectorGrants(t.Context(), "first", false, connectorTestNow)
	if err != nil || n != 0 {
		t.Fatalf("repeat: %d %v", n, err)
	}
	if _, err = db.RevokeLocalConnectorGrants(t.Context(), "fir", false, connectorTestNow); !errors.Is(err, ErrConnectorGrantNotFound) {
		t.Fatalf("prefix accepted: %v", err)
	}
	n, err = db.RevokeLocalConnectorGrants(t.Context(), "", true, connectorTestNow)
	if err != nil || n != 1 {
		t.Fatalf("all: %d %v", n, err)
	}
	n, err = db.RevokeLocalConnectorGrants(t.Context(), "", true, connectorTestNow)
	if err != nil || n != 0 {
		t.Fatalf("repeat all: %d %v", n, err)
	}
	rows, err = db.ListLocalConnectorGrants(t.Context(), connectorTestNow.Add(31*24*time.Hour))
	if err != nil || len(rows) != 2 {
		t.Fatalf("retained inspection: %+v %v", rows, err)
	}
}

func TestLocalConnectorAllGenerationsAndMissingOwner(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	connectorTestFamily(t, db, "a", "old")
	connectorTestOwner(t, db, 43)
	rows, err := db.ListLocalConnectorGrants(t.Context(), connectorTestNow)
	if err != nil || len(rows) != 1 || rows[0].Status != "invalidated" {
		t.Fatalf("old owner: %+v %v", rows, err)
	}
	// The local operation must cover old retained generations even when the remote
	// identity cannot authorize a request. Simulate a retained legacy unrevoked row.
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE connector_grants SET revoked_at=NULL,reason=NULL WHERE id='old'`); err != nil {
		t.Fatal(err)
	}
	n, err := db.RevokeLocalConnectorGrants(t.Context(), "", true, connectorTestNow)
	if err != nil || n != 1 {
		t.Fatalf("old generation all: %d %v", n, err)
	}
}

func TestLocalConnectorRevokeAllSerializesWithConsentAndRefresh(t *testing.T) {
	db := newTestDB(t)
	o := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	existing := connectorTestFamily(t, db, "a", "existing")
	connectorTestFamily(t, db, "a", "legacy")
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE connector_grants SET generation=generation-1 WHERE id='legacy'`); err != nil {
		t.Fatal(err)
	}
	browser := ConnectorBrowser{ID: "pending", ClientID: "a", RedirectURI: "https://client.test/callback", State: "state", Resource: connectorTestResource, Scope: connectorTestScope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: o.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
	if err := db.CreateConnectorBrowser(t.Context(), browser, 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if err := db.VerifyConnectorBrowser(t.Context(), browser.ID, "nonce", "upstream", 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	grant := ConnectorGrant{ID: "concurrent", ClientID: "a", Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: o.Generation, CreatedAt: connectorTestNow, ExpiresAt: connectorTestNow.Add(30 * 24 * time.Hour)}
	code := ConnectorCode{Digest: "concurrent-code", GrantID: grant.ID, ClientID: "a", RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: connectorTestNow.Add(5 * time.Minute)}
	consentDB, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer consentDB.Close()
	refreshDB, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer refreshDB.Close()
	start := make(chan struct{})
	consented := make(chan error, 1)
	refreshed := make(chan error, 1)
	next := existing
	next.Pair = ConnectorTokenPair{AccessDigest: "concurrent-access", RefreshDigest: "concurrent-refresh", AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}
	go func() {
		<-start
		consented <- consentDB.ConsentConnectorBrowser(t.Context(), browser.ID, "nonce", 42, connectorTestNow, grant, code, true)
	}()
	go func() { <-start; _, err := refreshDB.RotateConnectorRefresh(t.Context(), next); refreshed <- err }()
	close(start)
	n, err := db.RevokeLocalConnectorGrants(t.Context(), "", true, connectorTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-consented; err != nil {
		t.Fatal(err)
	}
	if err = <-refreshed; err != nil && !errors.Is(err, ErrConnectorInvalid) {
		t.Fatal(err)
	}
	if n != 2 && n != 3 {
		t.Fatalf("unexpected boundary count: %d", n)
	}
	connectorTestAccess(t, refreshDB, existing.Pair.AccessDigest, false)
	connectorTestAccess(t, refreshDB, next.Pair.AccessDigest, false)
	rows, err := db.ListLocalConnectorGrants(t.Context(), connectorTestNow)
	if err != nil {
		t.Fatal(err)
	}
	var concurrentStatus string
	for _, g := range rows {
		if g.ID == "concurrent" {
			concurrentStatus = g.Status
		}
		if g.ID == "legacy" && g.RevokedAt.IsZero() {
			t.Fatal("old generation escaped revoke-all")
		}
	}
	want := "active"
	if n == 3 {
		want = "revoked"
	}
	if concurrentStatus != want {
		t.Fatalf("consent boundary count %d status %s, want %s", n, concurrentStatus, want)
	}
	// New consent after the committed boundary remains possible.
	later := connectorTestFamily(t, consentDB, "a", "later")
	connectorTestAccess(t, refreshDB, later.Pair.AccessDigest, true)
	var lineage int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM connector_refresh WHERE grant_id='existing'`).Scan(&lineage); err != nil {
		t.Fatal(err)
	}
	if lineage < 1 {
		t.Fatal("revocation removed credential lineage")
	}
}
