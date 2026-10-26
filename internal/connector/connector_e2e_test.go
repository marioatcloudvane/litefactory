package connector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// TestOwnerConsentConnection exercises the owner journey with the actual browser,
// OAuth and MCP handlers. GitHub is the only replaced external service.
func TestOwnerConsentConnection(t *testing.T) {
	t.Parallel()
	var upstreamChallenge atomic.Value
	upstreamChallenge.Store("")
	var identityCalls atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			expectedChallenge, challengeOK := upstreamChallenge.Load().(string)
			if !challengeOK || r.Form.Get("code") != "fresh-github-code" || r.Form.Get("client_secret") != "github-secret-marker" || digest(r.Form.Get("code_verifier")) != expectedChallenge {
				t.Error("GitHub exchange lost its code, secret or PKCE binding")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"access_token":"upstream-token-marker"}`)
		case "/user":
			if r.Header.Get("Authorization") != "Bearer upstream-token-marker" {
				t.Error("fresh GitHub identity was not authenticated")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			identityCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":42,"login":"display-name-is-not-authority"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	fixture := newConnectorFixture(t, nil)
	fixture.options.GitHub = &GitHubProvider{ClientID: "github-app", ClientSecret: "github-secret-marker", AuthorizeURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", UserURL: upstream.URL + "/user", Client: upstream.Client()}
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Owner's client <script>alert(1)</script>", "none")
	verifier := strings.Repeat("a", 43)
	values := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {Scope}, "resource": {fixture.options.Config.ResourceURL}, "state": {"downstream-state"}, "code_challenge": {digest(verifier)}, "code_challenge_method": {"S256"}}
	start := connectorRequest(t, server, http.MethodGet, fixture.issuer+"/oauth/authorize?"+values.Encode(), "", "", nil)
	if start.Code != http.StatusFound {
		t.Fatalf("authorize: HTTP %d %s", start.Code, start.Body.String())
	}
	login, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	upstreamChallenge.Store(login.Query().Get("code_challenge"))
	if login.Query().Get("code_challenge") == "" || login.Query().Get("scope") != "" || login.Query().Get("state") == "" {
		t.Fatal("GitHub redirect lacks state/PKCE or requests repository permissions")
	}
	cookies := start.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("browser cookie lacks security attributes")
	}
	callback := connectorRequest(t, server, http.MethodGet, fixture.issuer+"/oauth/github/callback?"+url.Values{"code": {"fresh-github-code"}, "state": {login.Query().Get("state")}}.Encode(), "", "", nil, cookies...)
	if callback.Code != http.StatusOK {
		t.Fatalf("callback: HTTP %d %s", callback.Code, callback.Body.String())
	}
	page := callback.Body.String()
	if strings.Contains(page, "<script>alert(1)</script>") || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("consent page did not escape the client name")
	}
	for _, coverage := range []string{client.ClientID, Scope, fixture.options.Config.ResourceURL, "private", "future"} {
		if !strings.Contains(page, coverage) {
			t.Fatalf("consent omits %q", coverage)
		}
	}
	csrf := regexp.MustCompile(`name="csrf_token"[^>]*value="([^"]+)"`).FindStringSubmatch(page)
	if len(csrf) != 2 {
		t.Fatal("consent has no transaction-bound CSRF token")
	}
	if next := callback.Result().Cookies(); len(next) > 0 {
		cookies = next
	}
	consent := connectorForm(t, server, fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {csrf[1]}, "decision": {"allow"}}, nil, cookies...)
	if consent.Code != http.StatusFound {
		t.Fatalf("consent: HTTP %d %s", consent.Code, consent.Body.String())
	}
	redirect, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := redirect.Query().Get("code")
	if redirect.Scheme != "https" || redirect.Host != "client.example" || code == "" || redirect.Query().Get("state") != "downstream-state" || identityCalls.Load() != 1 {
		t.Fatal("owner consent did not produce the bound client code after fresh identity verification")
	}
	exchange := connectorForm(t, server, fixture.issuer+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "resource": {fixture.options.Config.ResourceURL}, "code": {code}, "code_verifier": {verifier}}, nil)
	var tokens connectorTestTokens
	connectorDecode(t, exchange, &tokens)
	if exchange.Code != http.StatusOK || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.ExpiresIn != 900 || tokens.Scope != Scope || tokens.TokenType != "Bearer" {
		t.Fatalf("token exchange: HTTP %d %s", exchange.Code, exchange.Body.String())
	}
	headers := http.Header{"Authorization": {"Bearer " + tokens.AccessToken}}
	initialize := connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}}, headers)
	var initialized struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	connectorDecode(t, initialize, &initialized)
	if initialize.Code != http.StatusOK || initialized.Result.ProtocolVersion != "2025-11-25" {
		t.Fatalf("initialize: HTTP %d %s", initialize.Code, initialize.Body.String())
	}
	headers.Set("MCP-Protocol-Version", "2025-11-25")
	listed := connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, headers)
	var tools struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	connectorDecode(t, listed, &tools)
	if listed.Code != http.StatusOK || tools.Result.Tools == nil || len(tools.Result.Tools) != 0 {
		t.Fatalf("absent overview provider must list no tools: HTTP %d %s", listed.Code, listed.Body.String())
	}
	refreshed := connectorForm(t, server, fixture.issuer+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ClientID}, "resource": {fixture.options.Config.ResourceURL}, "refresh_token": {tokens.RefreshToken}}, nil)
	var successor connectorTestTokens
	connectorDecode(t, refreshed, &successor)
	if refreshed.Code != http.StatusOK || successor.AccessToken == "" || successor.RefreshToken == "" || successor.AccessToken == tokens.AccessToken || successor.RefreshToken == tokens.RefreshToken || successor.ExpiresIn != 900 {
		t.Fatalf("refresh did not rotate bounded credentials: HTTP %d %s", refreshed.Code, refreshed.Body.String())
	}
	if response := fixture.ping(t, server, successor.AccessToken); response.Code != http.StatusOK {
		t.Fatalf("refreshed access unusable: HTTP %d", response.Code)
	}
	refused := connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "factory_overview", "arguments": map[string]any{}}}, nil)
	if refused.Code != http.StatusUnauthorized || !strings.Contains(refused.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("anonymous tool call was not challenged: HTTP %d", refused.Code)
	}
}
