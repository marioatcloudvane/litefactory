package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var connectorTestNow = time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)

const connectorTestResource = "https://example.test/litefactory/mcp"
const connectorTestScope = "factory:overview"

func connectorTestOwner(t *testing.T, db *DB, id int64) ConnectorOwner {
	t.Helper()
	if _, err := db.UpsertIdentity(t.Context(), Identity{Forge: ForgeGitHub, Role: RoleOwner, UserID: id, Handle: "owner", Source: SourceGH}); err != nil {
		t.Fatal(err)
	}
	o, err := db.ConnectorOwner(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func connectorTestClient(t *testing.T, db *DB, id string) {
	t.Helper()
	err := db.RegisterConnectorClient(t.Context(), ConnectorClient{ID: id, Name: "Client " + id, AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: connectorTestNow, ExpiresAt: connectorTestNow.Add(24 * time.Hour)}, connectorTestNow)
	if err != nil {
		t.Fatal(err)
	}
}
func connectorTestCode(t *testing.T, db *DB, client, id string) ConnectorExchange {
	t.Helper()
	o, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	b := ConnectorBrowser{ID: "browser-" + id, ClientID: client, RedirectURI: "https://client.test/callback", State: "state", Resource: connectorTestResource, Scope: connectorTestScope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: o.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), b, 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	g := ConnectorGrant{ID: id, ClientID: client, Resource: b.Resource, Scope: b.Scope, OwnerID: 42, OwnerGeneration: o.Generation, CreatedAt: connectorTestNow, ExpiresAt: connectorTestNow.Add(30 * 24 * time.Hour)}
	c := ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: client, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: connectorTestNow.Add(5 * time.Minute)}
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	return ConnectorExchange{Digest: c.Digest, ClientID: client, RedirectURI: c.RedirectURI, Resource: c.Resource, Scope: c.Scope, PKCEChallenge: c.PKCEChallenge, OwnerID: 42, Now: connectorTestNow, Pair: ConnectorTokenPair{AccessDigest: "access-" + id, RefreshDigest: "refresh-" + id, AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}}
}
func connectorTestFamily(t *testing.T, db *DB, client, id string) ConnectorExchange {
	t.Helper()
	x := connectorTestCode(t, db, client, id)
	if _, err := db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	x.Digest = x.Pair.RefreshDigest
	return x
}
func connectorTestAccess(t *testing.T, db *DB, digest string, valid bool) {
	t.Helper()
	_, err := db.CheckConnectorAccess(t.Context(), digest, 42, connectorTestResource, connectorTestScope, connectorTestNow)
	if valid && err != nil {
		t.Fatalf("access %s refused: %v", digest, err)
	}
	if !valid && !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("access %s expected invalid, got %v", digest, err)
	}
}

func TestConnectorCodeBindingAndExpiry(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	x := connectorTestCode(t, db, "a", "one")
	for _, field := range []string{"client", "redirect", "resource", "scope", "pkce", "owner", "expiry"} {
		t.Run(field, func(t *testing.T) {
			bad := x
			switch field {
			case "client":
				bad.ClientID = "other"
			case "redirect":
				bad.RedirectURI += "/wrong"
			case "resource":
				bad.Resource += "/wrong"
			case "scope":
				bad.Scope = "other"
			case "pkce":
				bad.PKCEChallenge = "wrong"
			case "owner":
				bad.OwnerID = 43
			case "expiry":
				bad.Now = bad.Now.Add(5 * time.Minute)
			}
			if _, err := db.ExchangeConnectorCode(t.Context(), bad); err == nil {
				t.Fatal("bad binding accepted")
			}
			var consumed any
			if err := db.sql.QueryRowContext(t.Context(), `SELECT consumed_at FROM connector_codes WHERE digest=?`, x.Digest).Scan(&consumed); err != nil {
				t.Fatal(err)
			}
			if consumed != nil {
				t.Fatal("bad exchange consumed code")
			}
		})
	}
	if _, err := db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeConnectorCode(t.Context(), x); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("code replay: %v", err)
	}
	connectorTestAccess(t, db, x.Pair.AccessDigest, true)
	if _, err := db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, x.Resource, x.Scope, x.Now.Add(15*time.Minute)); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("expiry boundary: %v", err)
	}
}

func TestConnectorRevocationIsolationAndReplay(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	connectorTestClient(t, db, "b")
	a := connectorTestFamily(t, db, "a", "one")
	other := connectorTestFamily(t, db, "a", "two")
	b := connectorTestFamily(t, db, "b", "three")
	for _, digest := range []string{a.Pair.AccessDigest, a.Pair.RefreshDigest} {
		if err := db.RevokeConnectorToken(t.Context(), "b", digest, a.Now); !errors.Is(err, ErrConnectorInvalid) {
			t.Fatalf("cross-client: %v", err)
		}
	}
	connectorTestAccess(t, db, a.Pair.AccessDigest, true)
	rotate := a
	rotate.Pair = ConnectorTokenPair{AccessDigest: "access-successor", RefreshDigest: "refresh-successor", AccessExpiresAt: a.Now.Add(15 * time.Minute)}
	if _, err := db.RotateConnectorRefresh(t.Context(), rotate); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeConnectorToken(t.Context(), "a", a.Pair.AccessDigest, a.Now); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, a.Pair.AccessDigest, false)
	connectorTestAccess(t, db, rotate.Pair.AccessDigest, true)
	if _, err := db.RotateConnectorRefresh(t.Context(), a); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("spent replay: %v", err)
	}
	connectorTestAccess(t, db, rotate.Pair.AccessDigest, false)
	connectorTestAccess(t, db, other.Pair.AccessDigest, true)
	connectorTestAccess(t, db, b.Pair.AccessDigest, true)
	if err := db.RevokeConnectorToken(t.Context(), "a", a.Pair.RefreshDigest, a.Now); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeConnectorToken(t.Context(), "a", "unknown", a.Now); err != nil {
		t.Fatal(err)
	}
	var ancestors int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM connector_refresh WHERE grant_id='one'`).Scan(&ancestors); err != nil {
		t.Fatal(err)
	}
	if ancestors != 2 {
		t.Fatalf("lost lineage: %d", ancestors)
	}
}

func TestConnectorOwnerAwayAndBackAndLocalControl(t *testing.T) {
	db := newTestDB(t)
	first := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	a := connectorTestFamily(t, db, "a", "one")
	summaries, err := db.ListConnectorGrants(t.Context(), 42, a.Now)
	if err != nil || len(summaries) != 1 || summaries[0].ClientName != "Client a" {
		t.Fatalf("summaries: %v %v", summaries, err)
	}
	connectorTestOwner(t, db, 43)
	last := connectorTestOwner(t, db, 42)
	if last.Generation <= first.Generation+1 {
		t.Fatal("owner generation did not advance twice")
	}
	connectorTestAccess(t, db, a.Pair.AccessDigest, false)
	if _, err = db.RotateConnectorRefresh(t.Context(), a); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("old refresh revived: %v", err)
	}
	fresh := connectorTestFamily(t, db, "a", "fresh")
	if err = db.RevokeConnectorGrant(t.Context(), 43, "fresh", a.Now); !errors.Is(err, ErrConnectorOwner) {
		t.Fatalf("wrong local owner: %v", err)
	}
	connectorTestAccess(t, db, fresh.Pair.AccessDigest, true)
	if err = db.RevokeConnectorGrant(t.Context(), 42, "fresh", a.Now); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, fresh.Pair.AccessDigest, false)
	fresh = connectorTestFamily(t, db, "a", "last")
	if err = db.RevokeAllConnectorGrants(t.Context(), 42, a.Now); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, fresh.Pair.AccessDigest, false)
}

func TestConnectorConcurrentRotationAndRevocationPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connector.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	a := connectorTestFamily(t, db, "a", "one")
	other := connectorTestFamily(t, db, "a", "other")
	second, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, d := range []*DB{db, second} {
		wg.Add(1)
		go func(i int, d *DB) {
			defer wg.Done()
			<-start
			x := a
			x.Pair = ConnectorTokenPair{AccessDigest: fmt.Sprintf("access-race-%d", i), RefreshDigest: fmt.Sprintf("refresh-race-%d", i), AccessExpiresAt: a.Now.Add(15 * time.Minute)}
			_, err := d.RotateConnectorRefresh(t.Context(), x)
			results <- err
		}(i, d)
	}
	close(start)
	wg.Wait()
	close(results)
	success, invalid := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrConnectorInvalid):
			invalid++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || invalid != 1 {
		t.Fatalf("rotation outcomes: %d %d", success, invalid)
	}
	for i := range 2 {
		connectorTestAccess(t, second, fmt.Sprintf("access-race-%d", i), false)
	}
	connectorTestAccess(t, second, other.Pair.AccessDigest, true)
	if err = db.RevokeConnectorToken(t.Context(), "a", other.Pair.RefreshDigest, a.Now); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	connectorTestAccess(t, reopened, other.Pair.AccessDigest, false)
}

func TestConnectorBrowserConsumptionAndCleanup(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	x := connectorTestFamily(t, db, "a", "one")
	if err := db.VerifyConnectorBrowser(t.Context(), "browser-one", "nonce", "upstream", 42, x.Now); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("consent replay: %v", err)
	}
	rotate := x
	rotate.Pair = ConnectorTokenPair{AccessDigest: "successor", RefreshDigest: "successor-refresh", AccessExpiresAt: x.Now.Add(15 * time.Minute)}
	if _, err := db.RotateConnectorRefresh(t.Context(), rotate); err != nil {
		t.Fatal(err)
	}
	if err := db.CleanupConnectorState(t.Context(), x.Now.Add(24*time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	var lineage int
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM connector_refresh`).Scan(&lineage); err != nil {
		t.Fatal(err)
	}
	if lineage != 2 {
		t.Fatalf("premature lineage cleanup: %d", lineage)
	}
	if err := db.CleanupConnectorState(t.Context(), x.Now.Add(31*24*time.Hour), 10); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRowContext(t.Context(), `SELECT count(*) FROM connector_refresh`).Scan(&lineage); err != nil {
		t.Fatal(err)
	}
	if lineage != 0 {
		t.Fatalf("expired lineage retained: %d", lineage)
	}
}

func TestConnectorSpentAncestorRevocationSerializesWithRotation(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	a := connectorTestFamily(t, db, "a", "one")
	other := connectorTestFamily(t, db, "a", "other")
	next := a
	next.Pair = ConnectorTokenPair{AccessDigest: "next-access", RefreshDigest: "next-refresh", AccessExpiresAt: a.Now.Add(15 * time.Minute)}
	if _, err := db.RotateConnectorRefresh(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeConnectorToken(t.Context(), "a", a.Pair.AccessDigest, a.Now); err != nil {
		t.Fatal(err)
	}
	// Rotating the successor after token-only revocation proves the family survived.
	last := a
	last.Digest = next.Pair.RefreshDigest
	last.Pair = ConnectorTokenPair{AccessDigest: "last-access", RefreshDigest: "last-refresh", AccessExpiresAt: a.Now.Add(15 * time.Minute)}
	if _, err := db.RotateConnectorRefresh(t.Context(), last); err != nil {
		t.Fatalf("access revocation affected family: %v", err)
	}
	second, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- second.RevokeConnectorToken(t.Context(), "a", a.Pair.RefreshDigest, a.Now) }()
	go func() {
		<-start
		x := last
		x.Digest = last.Pair.RefreshDigest
		x.Pair = ConnectorTokenPair{AccessDigest: "racing-access", RefreshDigest: "racing-refresh", AccessExpiresAt: a.Now.Add(15 * time.Minute)}
		_, err := db.RotateConnectorRefresh(t.Context(), x)
		results <- err
	}()
	close(start)
	for range 2 {
		if err := <-results; err != nil && !errors.Is(err, ErrConnectorInvalid) {
			t.Fatal(err)
		}
	}
	for _, digest := range []string{next.Pair.AccessDigest, last.Pair.AccessDigest, "racing-access"} {
		connectorTestAccess(t, db, digest, false)
	}
	connectorTestAccess(t, db, other.Pair.AccessDigest, true)
	x := last
	x.Digest = last.Pair.RefreshDigest
	if _, err = db.RotateConnectorRefresh(t.Context(), x); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("family successor survived: %v", err)
	}
}

func TestConnectorBrowserDenialAndFailureConsumeState(t *testing.T) {
	db := newTestDB(t)
	o := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	for _, mode := range []string{"deny", "failure"} {
		b := ConnectorBrowser{ID: mode, ClientID: "a", RedirectURI: "https://client.test/callback", Resource: connectorTestResource, Scope: connectorTestScope, NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: o.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
		if err := db.CreateConnectorBrowser(t.Context(), b, 42, connectorTestNow); err != nil {
			t.Fatal(err)
		}
		if err := db.AbandonConnectorBrowser(t.Context(), b.ID, "wrong", 42, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
			t.Fatalf("wrong nonce: %v", err)
		}
		if mode == "deny" {
			if err := db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, connectorTestNow); err != nil {
				t.Fatal(err)
			}
			if err := db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
				t.Fatalf("reused upstream: %v", err)
			}
			if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, ConnectorGrant{}, ConnectorCode{}, false); err != nil {
				t.Fatal(err)
			}
		} else if err := db.AbandonConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow); err != nil {
			t.Fatal(err)
		}
		if _, err := db.GetConnectorBrowser(t.Context(), b.ID, 42, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
			t.Fatalf("consumed browser revived: %v", err)
		}
	}
	summaries, err := db.ListConnectorGrants(t.Context(), 42, connectorTestNow)
	if err != nil || len(summaries) != 0 {
		t.Fatalf("denial/failure issued grant: %v %v", summaries, err)
	}
}

func TestConnectorStartupPinMismatchNeverRevivesAndNearExpiryRefresh(t *testing.T) {
	db := newTestDB(t)
	before := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	a := connectorTestFamily(t, db, "a", "one")
	if _, err := db.ReconcileConnectorOwner(t.Context(), 43, a.Now); !errors.Is(err, ErrConnectorOwner) {
		t.Fatalf("pin mismatch: %v", err)
	}
	after, err := db.ReconcileConnectorOwner(t.Context(), 42, a.Now)
	if err != nil {
		t.Fatal(err)
	}
	if after.Generation <= before.Generation {
		t.Fatal("pin mismatch did not advance generation")
	}
	connectorTestAccess(t, db, a.Pair.AccessDigest, false)
	if _, err = db.RotateConnectorRefresh(t.Context(), a); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("pin restore revived refresh: %v", err)
	}
	fresh := connectorTestFamily(t, db, "a", "fresh")
	fresh.Now = fresh.Now.Add(30*24*time.Hour - time.Minute)
	fresh.Pair = ConnectorTokenPair{AccessDigest: "near-expiry-access", RefreshDigest: "near-expiry-refresh", AccessExpiresAt: fresh.Now.Add(15 * time.Minute)}
	g, err := db.RotateConnectorRefresh(t.Context(), fresh)
	if err != nil {
		t.Fatalf("valid near-expiry refresh refused: %v", err)
	}
	if _, err = db.CheckConnectorAccess(t.Context(), fresh.Pair.AccessDigest, 42, fresh.Resource, fresh.Scope, g.ExpiresAt.Add(-time.Millisecond)); err != nil {
		t.Fatalf("clamped access prematurely expired: %v", err)
	}
	if _, err = db.CheckConnectorAccess(t.Context(), fresh.Pair.AccessDigest, 42, fresh.Resource, fresh.Scope, g.ExpiresAt); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("access outlived grant: %v", err)
	}
	if _, err = db.CheckConnectorAccess(t.Context(), fresh.Pair.AccessDigest, 42, fresh.Resource, "other", fresh.Now); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("insufficient scope: %v", err)
	}
	if _, err = db.CheckConnectorAccess(t.Context(), fresh.Pair.AccessDigest, 42, "https://wrong.test", fresh.Scope, fresh.Now); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("audience mismatch: %v", err)
	}
}

// Consent stores the owner's chosen known scopes (requested or not) and
// calendar-month expiry, up to ConnectorMaxGrantMonths, and refuses anything
// outside those bounds.
func TestConnectorConsentScopeAndLifetimeBounds(t *testing.T) {
	db := newTestDB(t)
	o := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "a")
	for i, test := range []struct {
		name, browserScope, grantScope string
		expires                        time.Time
		ok                             bool
	}{
		{"one month", connectorTestScope, connectorTestScope, ConnectorGrantExpiry(connectorTestNow, 1), true},
		{"maximum", connectorTestScope, connectorTestScope, ConnectorMaxGrantExpiry(connectorTestNow), true},
		{"beyond maximum", connectorTestScope, connectorTestScope, ConnectorMaxGrantExpiry(connectorTestNow).Add(time.Second), false},
		{"subset of requested", connectorTestScope + " other:read", connectorTestScope, ConnectorGrantExpiry(connectorTestNow, 1), true},
		{"unrequested known scope", connectorTestScope, connectorTestScope + " " + connectorFactoryUsageScope, ConnectorGrantExpiry(connectorTestNow, 1), true},
		{"only unrequested known scope", connectorTestScope, connectorFactoryUsageScope, ConnectorGrantExpiry(connectorTestNow, 1), true},
		{"unknown scope", connectorTestScope + " other:read", connectorTestScope + " other:read", ConnectorGrantExpiry(connectorTestNow, 1), false},
		{"repeated scope", connectorTestScope, connectorTestScope + " " + connectorTestScope, ConnectorGrantExpiry(connectorTestNow, 1), false},
		{"empty scope", connectorTestScope, "", ConnectorGrantExpiry(connectorTestNow, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := fmt.Sprintf("bounds-%d", i)
			b := ConnectorBrowser{ID: id, ClientID: "a", RedirectURI: "https://client.test/callback", Resource: connectorTestResource, Scope: test.browserScope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: o.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
			if err := db.CreateConnectorBrowser(t.Context(), b, 42, connectorTestNow); err != nil {
				t.Fatal(err)
			}
			if err := db.VerifyConnectorBrowser(t.Context(), id, "nonce", "upstream", 42, connectorTestNow); err != nil {
				t.Fatal(err)
			}
			g := ConnectorGrant{ID: id, ClientID: "a", Resource: b.Resource, Scope: test.grantScope, OwnerID: 42, OwnerGeneration: o.Generation, CreatedAt: connectorTestNow, ExpiresAt: test.expires}
			c := ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: "a", RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: test.grantScope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: connectorTestNow.Add(5 * time.Minute)}
			err := db.ConsentConnectorBrowser(t.Context(), id, "nonce", 42, connectorTestNow, g, c, true)
			if test.ok != (err == nil) {
				t.Fatalf("consent: %v, want ok=%v", err, test.ok)
			}
			if test.ok {
				x := ConnectorExchange{Digest: c.Digest, ClientID: "a", RedirectURI: b.RedirectURI, Resource: b.Resource, PKCEChallenge: b.PKCEChallenge, OwnerID: 42, Now: connectorTestNow, Pair: ConnectorTokenPair{AccessDigest: "access-" + id, RefreshDigest: "refresh-" + id, AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}}
				// A client repeating its narrower original request still
				// receives the whole grant the owner consented to.
				if ConnectorScopeSubset(test.browserScope, test.grantScope) {
					x.Scope = test.browserScope
				}
				got, err := db.ExchangeConnectorCode(t.Context(), x)
				if err != nil || got.Scope != test.grantScope || !got.ExpiresAt.Equal(test.expires) {
					t.Fatalf("exchange: %+v %v", got, err)
				}
			}
		})
	}
	if !ConnectorGrantExpiry(time.Date(2026, 1, 31, 12, 0, 0, 0, time.FixedZone("x", 3600)), 1).Equal(time.Date(2026, 3, 3, 11, 0, 0, 0, time.UTC)) {
		t.Fatal("a month is not AddDate(0, 1, 0) in UTC")
	}
}
