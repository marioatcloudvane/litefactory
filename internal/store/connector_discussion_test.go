package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// coverageConsentFixture prepares a verified browser and the grant, code and
// exchange a consent for scope with the given repository coverage would use.
func coverageConsentFixture(t *testing.T, db *DB, id, scope string, coverage *ConnectorCoverage) (ConnectorBrowser, ConnectorGrant, ConnectorCode, ConnectorExchange) {
	t.Helper()
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	b := ConnectorBrowser{ID: "browser-" + id, ClientID: "client", RedirectURI: "https://client.test/callback", Resource: connectorTestResource, Scope: scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: connectorTestNow.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), b, 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), b.ID, "nonce", "upstream", 42, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	g := ConnectorGrant{ID: id, ClientID: b.ClientID, Resource: b.Resource, Scope: scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: connectorTestNow, ExpiresAt: connectorTestNow.Add(24 * time.Hour), Coverage: coverage}
	c := ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: connectorTestNow.Add(5 * time.Minute)}
	x := ConnectorExchange{Digest: c.Digest, ClientID: b.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, OwnerID: 42, Now: connectorTestNow, Pair: ConnectorTokenPair{AccessDigest: "access-" + id, RefreshDigest: "refresh-" + id, AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}}
	return b, g, c, x
}

// grantCoverage consents and exchanges, returning the issued exchange.
func grantCoverage(t *testing.T, db *DB, id, scope string, coverage *ConnectorCoverage) (ConnectorGrant, ConnectorExchange) {
	t.Helper()
	b, g, c, x := coverageConsentFixture(t, db, id, scope, coverage)
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	return g, x
}

func testRepository(t *testing.T, db *DB, owner, name string) ConnectorRepository {
	t.Helper()
	repo := testRepo(t, db, owner, name)
	return ConnectorRepository{RepoID: repo.ID, RepositoryRef: "github:" + owner + "/" + name}
}

func testRepoSession(t *testing.T, db *DB, owner, name string, issue int) Session {
	t.Helper()
	session, err := db.GetOrCreateSession(t.Context(), testRepo(t, db, owner, name), issue)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestConnectorConsentRefusesMissingOrUnknownRepositoriesAndRollsBack(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	api := testRepository(t, db, "acme", "api")
	scope := connectorTestScope + " " + connectorDiscussionScope
	b, g, c, x := coverageConsentFixture(t, db, "grant", scope, nil)
	if _, _, err := db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "github:acme/api#7", connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("before Allow: %v", err)
	}
	for _, tc := range []struct {
		coverage *ConnectorCoverage
		reason   string
	}{
		{nil, "choose all repositories or at least one registered repository for task:discussion"},
		{&ConnectorCoverage{}, "choose all repositories or at least one registered repository"},
		{&ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: "github:missing/repo"}}}, `repository not registered: "github:missing/repo"`},
		{&ConnectorCoverage{Repositories: []ConnectorRepository{api, api}}, `repository selected twice: "github:acme/api"`},
		{&ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: api.RepositoryRef, RepoID: api.RepoID + 1}}}, ""},
		{&ConnectorCoverage{All: true, Repositories: []ConnectorRepository{api}}, ""},
	} {
		g.Coverage = tc.coverage
		err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true)
		if !errors.Is(err, ErrConnectorInvalid) || !strings.Contains(ConnectorRefusalReason(err), tc.reason) || (tc.reason == "") != (ConnectorRefusalReason(err) == "") {
			t.Fatalf("%+v: %v (reason %q)", tc.coverage, err, ConnectorRefusalReason(err))
		}
		var count int
		if err = db.sql.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM connector_grants)+(SELECT count(*) FROM connector_codes)+(SELECT count(*) FROM connector_grant_repositories)`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed consent left partial authorization: %d %v", count, err)
		}
	}
	// A grant without task-level or repository-level scopes takes no coverage.
	_, overview, overviewCode, _ := coverageConsentFixture(t, db, "overview", connectorTestScope, &ConnectorCoverage{All: true})
	if err := db.ConsentConnectorBrowser(t.Context(), "browser-overview", "nonce", 42, connectorTestNow, overview, overviewCode, true); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("coverage on overview-only grant: %v", err)
	}
	g.Coverage = &ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: api.RepositoryRef}}}
	if err := db.ConsentConnectorBrowser(t.Context(), b.ID, "nonce", 42, connectorTestNow, g, c, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE connector_grant_repositories SET repository_ref='github:other/repo' WHERE grant_id=?`, g.ID); err == nil {
		t.Fatal("coverage update allowed")
	}
}

func TestConnectorAllRepositoriesCoversTasksInAnyRepositoryIncludingLaterOnes(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	first := testRepoSession(t, db, "acme", "api", 7)
	scope := connectorTestScope + " " + connectorDiscussionScope + " " + connectorTaskUsageScope + " " + connectorRepositoryUsageScope
	g, x := grantCoverage(t, db, "all", scope, &ConnectorCoverage{All: true})
	later := testRepoSession(t, db, "acme", "web", 3)
	for _, session := range []Session{first, later} {
		_, target, err := db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, session.TaskRef, connectorTestNow)
		if err != nil || target.SessionID != session.ID || target.RepoID != session.RepoID {
			t.Fatalf("discussion %s: %+v %v", session.TaskRef, target, err)
		}
		ref := strings.SplitN(session.TaskRef, "#", 2)[0]
		_, usage, err := db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, ref, session.TaskRef, connectorTestNow)
		if err != nil || usage.SessionID != session.ID || usage.RepoID != session.RepoID {
			t.Fatalf("task usage %s: %+v %v", session.TaskRef, usage, err)
		}
		if _, usage, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorRepositoryUsageScope, ref, "", connectorTestNow); err != nil || usage.RepoID != session.RepoID {
			t.Fatalf("repository usage %s: %+v %v", ref, usage, err)
		}
	}
	_, _, err := db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "github:acme/api#999", connectorTestNow)
	if !errors.Is(err, ErrConnectorInvalid) || ConnectorRefusalReason(err) != `task not found: "github:acme/api#999"` {
		t.Fatalf("unknown task: %v", err)
	}
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorRepositoryUsageScope, "github:acme/none", "", connectorTestNow); ConnectorRefusalReason(err) != `repository not registered: "github:acme/none"` {
		t.Fatalf("unknown repository: %v", err)
	}
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, "github:acme/web", first.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("task outside the named repository: %v", err)
	}
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorFactoryUsageScope, "", "", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("task usage gained factory usage: %v", err)
	}
	// Refresh keeps scope and coverage.
	x.Digest = x.Pair.RefreshDigest
	x.Pair = ConnectorTokenPair{AccessDigest: "renewed", RefreshDigest: "renewed-refresh", AccessExpiresAt: connectorTestNow.Add(15 * time.Minute)}
	refreshed, err := db.RotateConnectorRefresh(t.Context(), x)
	if err != nil || refreshed.Scope != scope || !reflect.DeepEqual(refreshed.Coverage, &ConnectorCoverage{All: true}) {
		t.Fatalf("refresh: %+v %v", refreshed, err)
	}
	if err = db.RevokeConnectorGrant(t.Context(), 42, g.ID, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), "renewed", 42, connectorTestResource, first.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("revoked access: %v", err)
	}
}

func TestConnectorListedRepositoryRefusesOtherRepositoriesWithAClearReason(t *testing.T) {
	db := newTestDB(t)
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	covered := testRepoSession(t, db, "acme", "api", 7)
	other := testRepoSession(t, db, "acme", "web", 3)
	scope := connectorDiscussionScope + " " + connectorRepositoryUsageScope + " " + connectorTaskUsageScope
	_, x := grantCoverage(t, db, "one", scope, &ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: "github:acme/api"}}})
	grant, err := db.CheckConnectorAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, "", connectorTestNow)
	if err != nil || grant.Coverage == nil || grant.Coverage.All || len(grant.Coverage.Repositories) != 1 || grant.Coverage.Repositories[0].RepoID != covered.RepoID {
		t.Fatalf("coverage %+v %v", grant.Coverage, err)
	}
	if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, covered.TaskRef, connectorTestNow); err != nil {
		t.Fatal(err)
	}
	_, _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, other.TaskRef, connectorTestNow)
	if !errors.Is(err, ErrConnectorScope) || ConnectorRefusalReason(err) != `repository not covered by this grant: "github:acme/web" (task "github:acme/web#3")` {
		t.Fatalf("other repository: %v", err)
	}
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorRepositoryUsageScope, "github:acme/web", "", connectorTestNow); ConnectorRefusalReason(err) != `repository not covered by this grant: "github:acme/web"` {
		t.Fatalf("other repository usage: %v", err)
	}
	if _, _, err = db.CheckConnectorUsageAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, connectorTaskUsageScope, "github:acme/web", other.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("other task usage: %v", err)
	}
	if _, _, err = db.CheckConnectorCodeAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, covered.TaskRef, connectorTestNow); ConnectorRefusalReason(err) != "this grant does not include task:code-review" {
		t.Fatalf("code without scope: %v", err)
	}
}

func TestConnectorRepositoryCoverageLifecycleFailsClosed(t *testing.T) {
	for _, scenario := range []string{"deleted session", "session rebound", "repository rebound", "repository deleted", "expiry", "owner generation"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			connectorTestOwner(t, db, 42)
			connectorTestClient(t, db, "client")
			session := testSession(t, db, 7)
			_, x := grantCoverage(t, db, "grant", connectorTestScope+" "+connectorDiscussionScope, &ConnectorCoverage{Repositories: []ConnectorRepository{{RepositoryRef: "github:acme/api"}}})
			now := connectorTestNow
			var err error
			switch scenario {
			case "deleted session":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE id=?`, session.ID)
			case "session rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET task_ref='github:acme/api#8' WHERE id=?`, session.ID)
			case "repository rebound":
				_, err = db.sql.ExecContext(t.Context(), `UPDATE repos SET owner='another' WHERE id=?`, session.RepoID)
			case "repository deleted":
				_, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE repo_id=?; DELETE FROM repos WHERE id=?`, session.RepoID, session.RepoID)
			case "expiry":
				now = now.Add(24 * time.Hour)
			case "owner generation":
				connectorTestOwner(t, db, 99)
				connectorTestOwner(t, db, 42)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), x.Pair.AccessDigest, 42, connectorTestResource, session.TaskRef, now); err == nil {
				t.Fatalf("%s allowed", scenario)
			}
			if scenario == "repository rebound" && !errors.Is(err, ErrConnectorScope) {
				t.Fatalf("renamed repository still covered: %v", err)
			}
			// Losing discussion's task does not revoke independently consented overview.
			if scenario != "expiry" && scenario != "owner generation" {
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
	access := legacyConnectorGrant(t, db, "old", connectorTestScope)
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	connectorTestAccess(t, db, access, true)
	if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), access, 42, connectorTestResource, "github:acme/api#7", connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("old grant gained discussion: %v", err)
	}
}

// A grant bound to one task before repository coverage existed now covers
// that task's repository, and nothing else.
func TestConnectorRepositoryMigrationCoversTheBoundTasksRepository(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "prior.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	applyMigrationsThrough(t, db, migrationVersionBefore(t, "connector_repository_grants"))
	connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	bound := testSession(t, db, 7)
	sibling, err := db.GetOrCreateSession(t.Context(), Repo{ID: bound.RepoID, Forge: ForgeGitHub, Owner: "acme", Name: "api"}, 8)
	if err != nil {
		t.Fatal(err)
	}
	other := testRepoSession(t, db, "acme", "web", 3)
	scope := connectorDiscussionScope + " " + connectorRepositoryUsageScope
	legacyConnectorGrant(t, db, "old", scope)
	for _, stmt := range []string{
		`INSERT INTO connector_grant_targets(grant_id,task_ref,session_id,repo_id,repository_ref) VALUES('old','` + bound.TaskRef + `',` + itoa(bound.ID) + `,` + itoa(bound.RepoID) + `,'github:acme/api')`,
		`INSERT INTO connector_usage_targets(grant_id,scope,repository_ref,repo_id) VALUES('old','repository:usage','github:acme/api',` + itoa(bound.RepoID) + `)`,
	} {
		if _, err = db.sql.ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, session := range []Session{bound, sibling} {
		if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), "old-access", 42, connectorTestResource, session.TaskRef, connectorTestNow); err != nil {
			t.Fatalf("migrated grant lost %s: %v", session.TaskRef, err)
		}
	}
	if _, _, err = db.CheckConnectorDiscussionAccess(t.Context(), "old-access", 42, connectorTestResource, other.TaskRef, connectorTestNow); !errors.Is(err, ErrConnectorScope) {
		t.Fatalf("migrated grant widened beyond its repository: %v", err)
	}
	grants, err := db.ListLocalConnectorGrants(t.Context(), connectorTestNow)
	if err != nil || len(grants) != 1 || grants[0].AllRepositories || !reflect.DeepEqual(grants[0].Repositories, []string{"github:acme/api"}) {
		t.Fatalf("migrated listing %+v %v", grants, err)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// legacyConnectorGrant writes a grant and access token with the columns every
// connector schema has, for migration tests that start from an older schema.
func legacyConnectorGrant(t *testing.T, db *DB, id, scope string) string {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) SELECT '` + id + `','client','` + connectorTestResource + `','` + scope + `',42,generation,'` + FormatTime(connectorTestNow) + `','` + FormatTime(connectorTestNow.Add(24*time.Hour)) + `' FROM connector_owner`,
		`INSERT INTO connector_access(digest,grant_id,expires_at) VALUES('` + id + `-access','` + id + `','` + FormatTime(connectorTestNow.Add(15*time.Minute)) + `')`,
	} {
		if _, err := db.sql.ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	return id + "-access"
}
