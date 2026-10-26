package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestConnectorLocalRevocationMainPath crosses the CLI/store/HTTP boundary.
// Separate database handles model the local command and running daemon.
func TestConnectorLocalRevocationMainPath(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	t.Setenv(config.EnvHome, paths.Home())
	now := time.Now().UTC().Truncate(time.Millisecond)
	open := func() *store.DB {
		db, err := store.Open(t.Context(), paths.DBPath())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "test-app"
	cfg.GitHubSecretRef.Env = "UNUSED_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "UNUSED_COOKIE_SECRET"
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	seed := func(id string) (string, string) {
		client := store.ConnectorClient{ID: id, Name: "Client " + id, AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
		if err := db.RegisterConnectorClient(t.Context(), client, now); err != nil {
			t.Fatal(err)
		}
		b := store.ConnectorBrowser{ID: "browser-" + id, ClientID: id, RedirectURI: client.Redirects[0], Resource: cfg.ResourceURL, Scope: connector.Scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: now.Add(10 * time.Minute)}
		if err := db.CreateConnectorBrowser(t.Context(), b, 42, now); err != nil {
			t.Fatal(err)
		}
		if err := db.VerifyConnectorBrowser(t.Context(), b.ID, b.NonceDigest, b.UpstreamStateDigest, 42, now); err != nil {
			t.Fatal(err)
		}
		grant := store.ConnectorGrant{ID: id, ClientID: id, Resource: cfg.ResourceURL, Scope: connector.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour)}
		code := store.ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: id, RedirectURI: b.RedirectURI, Resource: cfg.ResourceURL, Scope: connector.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: now.Add(time.Minute)}
		if err := db.ConsentConnectorBrowser(t.Context(), b.ID, b.NonceDigest, 42, now, grant, code, true); err != nil {
			t.Fatal(err)
		}
		access, refresh := "access-"+id, "refresh-"+id
		x := store.ConnectorExchange{Digest: code.Digest, ClientID: id, RedirectURI: b.RedirectURI, Resource: cfg.ResourceURL, Scope: connector.Scope, PKCEChallenge: b.PKCEChallenge, OwnerID: 42, Now: now, Pair: store.ConnectorTokenPair{AccessDigest: hash(access), RefreshDigest: hash(refresh), AccessExpiresAt: now.Add(15 * time.Minute)}}
		if _, err := db.ExchangeConnectorCode(t.Context(), x); err != nil {
			t.Fatal(err)
		}
		return access, refresh
	}
	first, refresh := seed("first")
	second, _ := seed("second")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	serve := func(db *store.DB) *httptest.Server {
		local, err := connector.NewLocalControl(t.Context(), db, connector.NewGrantService(db))
		if err != nil {
			t.Fatal(err)
		}
		server, err := connector.New(t.Context(), connector.Options{Config: cfg, DB: db, Logger: logger, LocalControl: local, Now: func() time.Time { return now }, CookieKey: bytes.Repeat([]byte{7}, 32), GitHubSecret: "unused-test-secret"})
		if err != nil {
			t.Fatal(err)
		}
		ts := httptest.NewServer(server.Handler())
		t.Cleanup(ts.Close)
		return ts
	}
	ts := serve(db)
	transport := &http.Transport{MaxIdleConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request := func(path, content, body, token string) (int, []byte, bool) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "cloudvane.at"
		req.Header.Set("Content-Type", content)
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		reused := false
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }}))
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data, reused
	}
	ping := func(token string, want int) bool {
		t.Helper()
		status, data, reused := request("/litefactory/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, token)
		if status != want {
			t.Fatalf("protected request: got %d, want %d: %s", status, want, data)
		}
		return reused
	}
	status, data, _ := request("/litefactory/mcp", "application/json", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"owner-client","version":"1"}}}`, first)
	if status != http.StatusOK || !bytes.Contains(data, []byte(`"protocolVersion":"2025-11-25"`)) {
		t.Fatalf("client initialization: %d %s", status, data)
	}
	values := url.Values{"grant_type": {"refresh_token"}, "client_id": {"first"}, "resource": {cfg.ResourceURL}, "refresh_token": {refresh}}
	status, data, _ = request("/litefactory/oauth/token", "application/x-www-form-urlencoded", values.Encode(), "")
	var rotated struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &rotated) != nil || rotated.Access == "" || rotated.Refresh == "" {
		t.Fatalf("initial rotation: %d %s", status, data)
	}
	ping(rotated.Access, http.StatusOK)
	ping(second, http.StatusOK)
	run := func(args ...string) string {
		t.Helper()
		var out, diagnostic bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &diagnostic, Logger: logger, Now: func() time.Time { return now }}
		if exit := NewRouter("litefactory", env, Commands()).Run(t.Context(), args); exit != ExitOK {
			t.Fatalf("CLI %v: exit %d: %s", args, exit, diagnostic.String())
		}
		return out.String()
	}
	inspection := run("connector", "grants", "inspect", "first", "--json")
	if !strings.Contains(inspection, "first") {
		t.Fatal("inspection omitted selected grant")
	}
	// Run the actual CLI in another process while this daemon and connection stay up.
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestConnectorGrantCLIProcess$")
	child.Env = append(os.Environ(), "LF_TEST_CONNECTOR_REVOKE=first")
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("independent local revoke: %v: %s", err, output)
	}
	if !ping(rotated.Access, http.StatusUnauthorized) {
		t.Fatal("next protected request did not reuse the already connected client")
	}
	ping(first, http.StatusUnauthorized)
	ping(second, http.StatusOK)
	values.Set("refresh_token", rotated.Refresh)
	status, data, _ = request("/litefactory/oauth/token", "application/x-www-form-urlencoded", values.Encode(), "")
	if status != http.StatusBadRequest || !bytes.Contains(data, []byte("invalid_grant")) {
		t.Fatalf("revoked renewal: %d %s", status, data)
	}
	transport.CloseIdleConnections()
	ping(rotated.Access, http.StatusUnauthorized)
	ping(second, http.StatusOK)
	ts.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	ts = serve(db)
	ping(rotated.Access, http.StatusUnauthorized)
	ping(first, http.StatusUnauthorized)
	ping(second, http.StatusOK)
	status, data, _ = request("/litefactory/oauth/token", "application/x-www-form-urlencoded", values.Encode(), "")
	if status != http.StatusBadRequest || !bytes.Contains(data, []byte("invalid_grant")) {
		t.Fatalf("renewal after restart: %d %s", status, data)
	}
}

// TestConnectorGrantCLIProcess is the separate local command process used by the
// main-path test; ordinary package runs skip it without the private test marker.
func TestConnectorGrantCLIProcess(t *testing.T) {
	id := os.Getenv("LF_TEST_CONNECTOR_REVOKE")
	if id == "" {
		return
	}
	env := &Env{Stdout: os.Stdout, Stderr: os.Stderr, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if exit := NewRouter("litefactory", env, Commands()).Run(t.Context(), []string{"connector", "grants", "revoke", id, "--json"}); exit != ExitOK {
		t.Fatalf("local command exit %d", exit)
	}
}
