package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/store"
)

var grantCLINow = time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)

func grantCLIHome(t *testing.T) (config.Paths, *store.DB) {
	t.Helper()
	paths := config.NewPaths(t.TempDir())
	t.Setenv(config.EnvHome, paths.Home())
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return paths, db
}

func seedCLIGrant(t *testing.T, db *store.DB, id string) {
	t.Helper()
	now := grantCLINow
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	client := store.ConnectorClient{ID: id, Name: "Client\x1b[31m\n" + strings.Repeat("x", 180), AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if err = db.RegisterConnectorClient(t.Context(), client, now); err != nil {
		t.Fatal(err)
	}
	b := store.ConnectorBrowser{ID: "browser-" + id, ClientID: id, RedirectURI: client.Redirects[0], Resource: "https://factory.test/litefactory/mcp", Scope: connector.Scope, PKCEChallenge: "challenge", NonceDigest: "nonce-secret-" + id, UpstreamStateDigest: "upstream-secret-" + id, OwnerGeneration: owner.Generation, ExpiresAt: now.Add(time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), b, 42, now); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), b.ID, b.NonceDigest, b.UpstreamStateDigest, 42, now); err != nil {
		t.Fatal(err)
	}
	g := store.ConnectorGrant{ID: id, ClientID: id, Resource: b.Resource, Scope: b.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	c := store.ConnectorCode{Digest: "code-secret-" + id, GrantID: id, ClientID: id, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: now.Add(time.Minute)}
	if err = db.ConsentConnectorBrowser(t.Context(), b.ID, b.NonceDigest, 42, now, g, c, true); err != nil {
		t.Fatal(err)
	}
	x := store.ConnectorExchange{Digest: c.Digest, ClientID: id, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, OwnerID: 42, Now: now, Pair: store.ConnectorTokenPair{AccessDigest: "access-secret-" + id, RefreshDigest: "refresh-secret-" + id, AccessExpiresAt: now.Add(15 * time.Minute)}}
	if _, err = db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
}

func runGrantCLI(ctx context.Context, now time.Time, output io.Writer, args ...string) (int, string, string) {
	var out, diagnostic bytes.Buffer
	if output == nil {
		output = &out
	}
	env := &Env{Stdout: output, Stderr: &diagnostic, Now: func() time.Time { return now }}
	code := NewRouter("litefactory", env, Commands()).Run(ctx, append([]string{"connector", "grants"}, args...))
	return code, out.String(), diagnostic.String()
}

func TestConnectorGrantLocalInspectionAndRevocation(t *testing.T) {
	paths, db := grantCLIHome(t)
	// Invalid configuration must not affect local recovery.
	if err := os.WriteFile(paths.ConfigPath(), []byte("remote_connector: [broken"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, diag := runGrantCLI(t.Context(), grantCLINow, nil, "list", "--json")
	if code != ExitOK || !strings.Contains(out, `"grants": []`) {
		t.Fatalf("empty: %d %s %s", code, out, diag)
	}
	seedCLIGrant(t, db, "first")
	seedCLIGrant(t, db, "second")
	for _, format := range []bool{false, true} {
		args := []string{"list"}
		if format {
			args = append(args, "--json")
		}
		code, out, diag = runGrantCLI(t.Context(), grantCLINow, nil, args...)
		if code != ExitOK {
			t.Fatalf("list: %d %s", code, diag)
		}
		for _, secret := range []string{"nonce-secret", "upstream-secret", "code-secret", "access-secret", "refresh-secret", "\x1b"} {
			if strings.Contains(out, secret) {
				t.Fatalf("unsafe output contains %q", secret)
			}
		}
		if len(out) > 6000 {
			t.Fatal("unbounded client label")
		}
	}
	code, out, diag = runGrantCLI(t.Context(), grantCLINow, nil, "inspect", "first", "--json")
	var inspect struct {
		FactoryHome string                   `json:"factory_home"`
		SnapshotAt  time.Time                `json:"snapshot_at"`
		Grants      []connector.GrantSummary `json:"grants"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &inspect) != nil || len(inspect.Grants) != 1 {
		t.Fatalf("inspect: %d %s %s", code, out, diag)
	}
	g := inspect.Grants[0]
	if inspect.FactoryHome != paths.Home() || !inspect.SnapshotAt.Equal(grantCLINow) || g.ID != "first" || !g.Renewable || !g.Coverage.PrivateRepositories || !g.Coverage.FutureRepositories || g.AccessTokens.Count != 1 || g.AccessTokens.EarliestExpiry == nil || !g.ExpiresAt.After(*g.AccessTokens.EarliestExpiry) {
		t.Fatalf("summary: %+v", inspect)
	}

	// Revoking only an issued access token leaves the grant renewable while
	// inspection distinguishes that retained token from usable credentials.
	if err := db.RevokeConnectorToken(t.Context(), "first", "access-secret-first", grantCLINow); err != nil {
		t.Fatal(err)
	}
	code, out, diag = runGrantCLI(t.Context(), grantCLINow, nil, "inspect", "first", "--json")
	if code != ExitOK || json.Unmarshal([]byte(out), &inspect) != nil {
		t.Fatalf("access-only revoke inspection: %d %s %s", code, out, diag)
	}
	g = inspect.Grants[0]
	if !g.Renewable || g.AccessTokens.Count != 1 || g.AccessTokens.UsableCount != 0 || g.AccessTokens.Usable {
		t.Fatalf("access-only revoke summary: %+v", g)
	}
	code, out, diag = runGrantCLI(t.Context(), grantCLINow, nil, "inspect", "first")
	if code != ExitOK || !strings.Contains(out, "usable: 0; unusable: 1") {
		t.Fatalf("access-only revoke text: %d %s %s", code, out, diag)
	}
	for _, target := range []string{"fir", "missing"} {
		for _, action := range []string{"inspect", "revoke"} {
			code, _, _ = runGrantCLI(t.Context(), grantCLINow, nil, action, target)
			if code != ExitPrecondition {
				t.Fatalf("%s unknown: %d", action, code)
			}
		}
	}
	for index, want := range []int64{1, 0} {
		code, out, diag = runGrantCLI(t.Context(), grantCLINow, nil, "revoke", "first", "--json")
		var mutation connector.GrantMutation
		if code != ExitOK || json.Unmarshal([]byte(out), &mutation) != nil || mutation.ChangedCount != want {
			t.Fatalf("revoke %d: %d %s %s", index, code, out, diag)
		}
	}
	// Retained expired grants remain inspectable and revocable.
	later := grantCLINow.Add(31 * 24 * time.Hour)
	code, out, diag = runGrantCLI(t.Context(), later, nil, "inspect", "second", "--json")
	if code != ExitOK || !strings.Contains(out, `"status": "expired"`) {
		t.Fatalf("expired inspection: %d %s %s", code, out, diag)
	}
	code, _, diag = runGrantCLI(t.Context(), later, nil, "revoke", "second")
	if code != ExitOK {
		t.Fatalf("expired revoke: %d %s", code, diag)
	}
	code, out, diag = runGrantCLI(t.Context(), later, nil, "revoke-all", "--json")
	if code != ExitOK || !strings.Contains(out, `"changed_count": 0`) {
		t.Fatalf("repeat all: %d %s %s", code, out, diag)
	}
}

func TestConnectorGrantCommandFailures(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	t.Setenv(config.EnvHome, paths.Home())
	for _, args := range [][]string{{"list", "extra"}, {"inspect"}, {"revoke"}, {"revoke-all", "first"}, {"revoke", "first", "extra"}, {"revoke", " first"}, {"inspect", "x\n"}} {
		code, _, _ := runGrantCLI(t.Context(), grantCLINow, nil, args...)
		if code != ExitUsage {
			t.Fatalf("args %q: %d", args, code)
		}
	}
	code, _, _ := runGrantCLI(t.Context(), grantCLINow, nil, "list")
	if code != ExitPrecondition {
		t.Fatalf("missing: %d", code)
	}
	if _, err := os.Stat(paths.DBPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manufactured database: %v", err)
	}
	_, db := grantCLIHome(t)
	seedCLIGrant(t, db, "first")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	code, _, _ = runGrantCLI(ctx, grantCLINow, nil, "revoke", "first")
	if code != ExitFailure {
		t.Fatalf("cancel: %d", code)
	}
	svc := connector.NewGrantService(db)
	grant, err := svc.InspectGrant(t.Context(), "first", grantCLINow)
	if err != nil || grant.Status != "active" {
		t.Fatalf("cancel mutated: %+v %v", grant, err)
	}

	for _, args := range [][]string{{"list"}, {"inspect", "first", "--json"}} {
		code, _, diag := runGrantCLI(t.Context(), grantCLINow, grantFailWriter{}, args...)
		if code != ExitFailure || strings.Contains(diag, "sensitive-writer-secret") {
			t.Fatalf("inspection output diagnostic: %d %s", code, diag)
		}
	}
	code, _, diag := runGrantCLI(t.Context(), grantCLINow, grantFailWriter{}, "revoke", "first", "--json")
	if code != ExitFailure || !strings.Contains(diag, "committed") {
		t.Fatalf("output error: %d %s", code, diag)
	}
	grant, err = svc.InspectGrant(t.Context(), "first", grantCLINow)
	if err != nil || grant.Status != "revoked" {
		t.Fatalf("output failure rolled back: %+v %v", grant, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	err = runConnectorGrantCommand(t.Context(), &Env{Stdout: io.Discard}, svc, "factory", "list", nil, false, grantCLINow)
	if ExitCode(err) != ExitFailure {
		t.Fatalf("closed store: %v", err)
	}
}

type grantFailWriter struct{}

func (grantFailWriter) Write([]byte) (int, error) { return 0, errors.New("sensitive-writer-secret") }
