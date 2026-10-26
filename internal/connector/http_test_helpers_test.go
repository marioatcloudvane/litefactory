package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

func connectorTestDB(t *testing.T) (*store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connector.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func connectorRequest(t *testing.T, handler http.Handler, method, target, contentType, body string, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(t.Context())
	req.RemoteAddr = "127.0.0.1:12345"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func connectorForm(t *testing.T, handler http.Handler, target string, values url.Values, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return connectorRequest(t, handler, http.MethodPost, target, "application/x-www-form-urlencoded", values.Encode(), headers, cookies...)
}

func connectorJSON(t *testing.T, handler http.Handler, target string, body any, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Accept", "application/json, text/event-stream")
	return connectorRequest(t, handler, http.MethodPost, target, "application/json", string(encoded), headers)
}

func connectorDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(bytes.NewReader(response.Body.Bytes())).Decode(target); err != nil {
		t.Fatalf("decode HTTP %d: %v (%s)", response.Code, err, response.Body.String())
	}
}

type connectorTestTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

type connectorTestClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AuthMethod   string `json:"token_endpoint_auth_method"`
}

func registerConnectorClient(t *testing.T, server http.Handler, name, authMethod string) connectorTestClient {
	t.Helper()
	response := connectorJSON(t, server, "https://cloudvane.at/litefactory/oauth/register", map[string]any{"client_name": name, "redirect_uris": []string{"https://client.example/callback"}, "token_endpoint_auth_method": authMethod}, nil)
	var client connectorTestClient
	connectorDecode(t, response, &client)
	if response.Code != http.StatusCreated || client.ClientID == "" {
		t.Fatalf("register: HTTP %d %s", response.Code, response.Body.String())
	}
	return client
}

type connectorLogBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *connectorLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *connectorLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

type connectorFixture struct {
	db           *store.DB
	path, issuer string
	options      Options
	logs         *connectorLogBuffer
	control      *connectorFixtureControl
	now          time.Time
}

type connectorFixtureControl struct {
	fixture     *connectorFixture
	unavailable atomic.Bool
}

func (c *connectorFixtureControl) Ready(context.Context) error {
	if c.unavailable.Load() {
		return errors.New("local command host stopped")
	}
	return nil
}
func (c *connectorFixtureControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.fixture.db.ListConnectorGrants(ctx, 42, c.fixture.now)
}
func (c *connectorFixtureControl) Revoke(ctx context.Context, id string) error {
	return c.fixture.db.RevokeConnectorGrant(ctx, 42, id, c.fixture.now)
}
func (c *connectorFixtureControl) RevokeAll(ctx context.Context) error {
	return c.fixture.db.RevokeAllConnectorGrants(ctx, 42, c.fixture.now)
}

type connectorFixtureIdentity struct{}

func (connectorFixtureIdentity) AuthorizationURL(state, challenge, redirect string) string {
	return "https://github.example/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}, "redirect_uri": {redirect}}.Encode()
}
func (connectorFixtureIdentity) Verify(context.Context, string, string, string) (int64, error) {
	return 42, nil
}

type connectorFixtureOverview func(context.Context, json.RawMessage) (ToolResult, error)

func (f connectorFixtureOverview) Overview(ctx context.Context, args json.RawMessage) (ToolResult, error) {
	return f(ctx, args)
}

func newConnectorFixture(t *testing.T, provider OverviewProvider) *connectorFixture {
	t.Helper()
	db, path := connectorTestDB(t)
	fixture := &connectorFixture{db: db, path: path, issuer: "https://cloudvane.at/litefactory", logs: &connectorLogBuffer{}, now: time.Now().UTC().Truncate(time.Millisecond)}
	fixture.control = &connectorFixtureControl{fixture: fixture}
	local, err := NewLocalControl(t.Context(), db, fixture.control)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "github-app"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	cfg.AllowedOrigins = []string{"https://client.example"}
	fixture.options = Options{Config: cfg, DB: db, Logger: slog.New(slog.NewJSONHandler(fixture.logs, nil)), Now: func() time.Time { return fixture.now }, LocalControl: local, Overview: provider, GitHub: connectorFixtureIdentity{}, CookieKey: bytes.Repeat([]byte{7}, 32)}
	return fixture
}
func (f *connectorFixture) server(t *testing.T) http.Handler {
	t.Helper()
	server, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

// issueCode uses the real durable consent transition, keeping HTTP regressions
// focused on the boundary under test rather than repeating the browser journey.
func (f *connectorFixture) issueCode(t *testing.T, client connectorTestClient) (string, string, string) {
	t.Helper()
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomCredential()
	if err != nil {
		t.Fatal(err)
	}
	code, err := randomCredential()
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	browser := store.ConnectorBrowser{ID: id, ClientID: client.ClientID, RedirectURI: "https://client.example/callback", Resource: f.options.Config.ResourceURL, Scope: Scope, PKCEChallenge: digest(verifier), NonceDigest: digest(id + "nonce"), UpstreamStateDigest: digest(id + "upstream"), OwnerGeneration: owner.Generation, ExpiresAt: f.now.Add(10 * time.Minute)}
	if err := f.db.CreateConnectorBrowser(t.Context(), browser, 42, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.db.VerifyConnectorBrowser(t.Context(), id, browser.NonceDigest, browser.UpstreamStateDigest, 42, f.now); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: id, ClientID: client.ClientID, Resource: browser.Resource, Scope: Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: f.now, ExpiresAt: f.now.Add(30 * 24 * time.Hour)}
	bound := store.ConnectorCode{Digest: digest(code), GrantID: id, ClientID: client.ClientID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: f.now.Add(5 * time.Minute)}
	if err := f.db.ConsentConnectorBrowser(t.Context(), id, browser.NonceDigest, 42, f.now, grant, bound, true); err != nil {
		t.Fatal(err)
	}
	return code, verifier, id
}

func (f *connectorFixture) issueTokens(t *testing.T, server http.Handler, client connectorTestClient) (connectorTestTokens, string) {
	t.Helper()
	code, verifier, id := f.issueCode(t, client)
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "resource": {f.options.Config.ResourceURL}, "code": {code}, "code_verifier": {verifier}}
	headers := clientCredentials(client, values)
	response := connectorForm(t, server, f.issuer+"/oauth/token", values, headers)
	var tokens connectorTestTokens
	connectorDecode(t, response, &tokens)
	if response.Code != http.StatusOK || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("issue tokens: HTTP %d %s", response.Code, response.Body.String())
	}
	return tokens, id
}

func clientCredentials(client connectorTestClient, values url.Values) http.Header {
	headers := http.Header{}
	switch client.AuthMethod {
	case "client_secret_basic":
		req := &http.Request{Header: headers}
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
		values.Del("client_id")
	case "client_secret_post":
		values.Set("client_secret", client.ClientSecret)
	}
	return headers
}

func connectorBearer(token string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("MCP-Protocol-Version", "2025-11-25")
	return headers
}

func (f *connectorFixture) ping(t *testing.T, server http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	return connectorJSON(t, server, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, connectorBearer(token))
}

func (f *connectorFixture) restart(t *testing.T) http.Handler {
	t.Helper()
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.db = db
	f.options.DB = db
	local, err := NewLocalControl(t.Context(), db, f.control)
	if err != nil {
		t.Fatal(err)
	}
	f.options.LocalControl = local
	return f.server(t)
}
