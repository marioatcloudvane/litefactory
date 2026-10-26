package connector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type browserIdentity struct {
	owner int64
	err   error
}

func (browserIdentity) AuthorizationURL(state, challenge, redirect string) string {
	return "https://github.example/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}, "redirect_uri": {redirect}}.Encode()
}
func (p browserIdentity) Verify(context.Context, string, string, string) (int64, error) {
	return p.owner, p.err
}

type browserJourney struct {
	fixture *connectorFixture
	handler http.Handler
	cookie  *http.Cookie
	state   string
}

func beginBrowserJourney(t *testing.T, identity GitHubIdentityProvider) browserJourney {
	t.Helper()
	f := newConnectorFixture(t, nil)
	f.options.GitHub = identity
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "Named client", "none")
	q := url.Values{"client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "resource": {f.options.Config.ResourceURL}, "scope": {Scope}, "state": {"downstream-state"}, "code_challenge": {digest(strings.Repeat("v", 43))}, "code_challenge_method": {"S256"}}
	r := connectorRequest(t, handler, http.MethodGet, f.issuer+"/oauth/authorize?"+q.Encode(), "", "", nil)
	if r.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", r.Code, r.Body.String())
	}
	u, err := url.Parse(r.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cookies := r.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %v", cookies)
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("insecure browser cookie: %+v", cookie)
	}
	return browserJourney{fixture: f, handler: handler, cookie: cookie, state: u.Query().Get("state")}
}

func (j browserJourney) callback(t *testing.T, state string) *httptest.ResponseRecorder {
	t.Helper()
	return connectorRequest(t, j.handler, http.MethodGet, j.fixture.issuer+"/oauth/github/callback?"+url.Values{"code": {"github-code"}, "state": {state}}.Encode(), "", "", nil, j.cookie)
}

func browserNoGrants(t *testing.T, f *connectorFixture) {
	t.Helper()
	grants, err := f.db.ListConnectorGrants(t.Context(), 42, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 0 {
		t.Fatalf("failed authorization created %d grants", len(grants))
	}
}

func TestBrowserRejectsFailedIdentityAndConsumedState(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		identity   browserIdentity
		wrongState bool
	}{
		{name: "different numeric owner", identity: browserIdentity{owner: 99}},
		{name: "provider unavailable", identity: browserIdentity{err: errors.New("seeded-secret-provider-error")}},
		{name: "unbound upstream state", identity: browserIdentity{owner: 42}, wrongState: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			j := beginBrowserJourney(t, test.identity)
			state := j.state
			if test.wrongState {
				state = "different-state"
			}
			response := j.callback(t, state)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("failed verification: %d %s", response.Code, response.Body.String())
			}
			// Even a retained original cookie cannot resume a failed transaction.
			if response := j.callback(t, j.state); response.Code != http.StatusBadRequest {
				t.Fatalf("reused failed browser transaction: %d", response.Code)
			}
			browserNoGrants(t, j.fixture)
			if strings.Contains(j.fixture.logs.String(), "seeded-secret-provider-error") {
				t.Fatal("provider error escaped into diagnostics")
			}
		})
	}
}

func TestBrowserConsentRequiresVerifiedCookieAndBoundCSRF(t *testing.T) {
	t.Parallel()
	j := beginBrowserJourney(t, browserIdentity{owner: 42})
	before := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {"fake"}, "decision": {"allow"}}, nil, j.cookie)
	if before.Code != http.StatusBadRequest {
		t.Fatalf("unverified consent: %d", before.Code)
	}
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", page.Code, page.Body.String())
	}
	match := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(match) != 2 {
		t.Fatal("consent has no CSRF token")
	}
	verifiedCookie := page.Result().Cookies()[0]
	wrong := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {"wrong"}, "decision": {"allow"}}, nil, verifiedCookie)
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("unbound CSRF: %d", wrong.Code)
	}
	wrongCookie := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {match[1]}, "decision": {"allow"}}, nil, j.cookie)
	if wrongCookie.Code != http.StatusBadRequest {
		t.Fatalf("stale unverified cookie: %d", wrongCookie.Code)
	}
	denied := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {match[1]}, "decision": {"deny"}}, nil, verifiedCookie)
	if denied.Code != http.StatusFound {
		t.Fatalf("deny: %d %s", denied.Code, denied.Body.String())
	}
	location, err := url.Parse(denied.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Get("error") != "access_denied" || location.Query().Get("code") != "" || location.Query().Get("state") != "downstream-state" {
		t.Fatalf("denial redirect: %v", location)
	}
	replayed := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {match[1]}, "decision": {"allow"}}, nil, verifiedCookie)
	if replayed.Code != http.StatusBadRequest {
		t.Fatalf("denial replay: %d", replayed.Code)
	}
	browserNoGrants(t, j.fixture)
}

func TestBrowserExpiryAndOwnerGenerationInvalidatePendingConsent(t *testing.T) {
	t.Parallel()
	for _, test := range []string{"expiry", "owner away and back"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			j := beginBrowserJourney(t, browserIdentity{owner: 42})
			if test == "expiry" {
				j.fixture.now = j.fixture.now.Add(10 * time.Minute)
			} else {
				for _, owner := range []int64{99, 42} {
					if _, err := j.fixture.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: owner, Handle: "owner", Source: store.SourceGH}); err != nil {
						t.Fatal(err)
					}
				}
			}
			response := j.callback(t, j.state)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid pending transaction: %d %s", response.Code, response.Body.String())
			}
			browserNoGrants(t, j.fixture)
		})
	}
}

func TestAuthorizeRejectsUnboundRequestsWithoutRedirect(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, key, value string }{
		{"unknown client", "client_id", "unknown"}, {"unregistered redirect", "redirect_uri", "https://attacker.example/callback"}, {"wrong resource", "resource", "https://attacker.example/mcp"}, {"wrong scope", "scope", "factory:write"}, {"plain PKCE", "code_challenge_method", "plain"}, {"missing PKCE", "code_challenge", ""}, {"metadata URL client", "client_id", "https://client.example/metadata.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newConnectorFixture(t, nil)
			handler := f.server(t)
			client := registerConnectorClient(t, handler, "Client", "none")
			q := url.Values{"client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "resource": {f.options.Config.ResourceURL}, "scope": {Scope}, "code_challenge": {digest(strings.Repeat("v", 43))}, "code_challenge_method": {"S256"}}
			q.Set(test.key, test.value)
			response := connectorRequest(t, handler, http.MethodGet, f.issuer+"/oauth/authorize?"+q.Encode(), "", "", nil)
			if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" || len(response.Result().Cookies()) != 0 {
				t.Fatalf("unsafe authorize response: %d %v", response.Code, response.Header())
			}
			browserNoGrants(t, f)
		})
	}
}

func TestAuthorizeIgnoresUnknownParametersAndRefusesDuplicates(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "Client", "none")
	q := url.Values{"client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "resource": {f.options.Config.ResourceURL}, "scope": {Scope}, "state": {"client-state"}, "code_challenge": {digest(strings.Repeat("v", 43))}, "code_challenge_method": {"S256"}, "ui_locales": {"en-US"}, "prompt": {"consent"}}
	response := connectorRequest(t, handler, http.MethodGet, f.issuer+"/oauth/authorize?"+q.Encode(), "", "", nil)
	if response.Code != http.StatusFound || response.Header().Get("Location") == "" {
		t.Fatalf("authorize with unknown optional parameters: %d %s", response.Code, response.Body.String())
	}
	q.Add("scope", Scope)
	response = connectorRequest(t, handler, http.MethodGet, f.issuer+"/oauth/authorize?"+q.Encode(), "", "", nil)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("authorize with a duplicate parameter: %d %v", response.Code, response.Header())
	}
}

func TestRedirectRegistrationRequiresUnambiguousHTTPS(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://client.example/callback", "https://client.example/callback#", "https://client.example/callback#fragment", "https://user:secret@client.example/callback", "https://client.example:0/callback", "https://client.example:65536/callback", "https://client.example:/callback", "https://*.example/callback", "https://client.example/callback\\other"} {
		if validRedirect(raw) {
			t.Errorf("accepted ambiguous redirect %q", raw)
		}
	}
	for _, raw := range []string{"https://client.example/callback", "https://client.example:8443/callback?registered=value", "https://[::1]:8443/callback"} {
		if !validRedirect(raw) {
			t.Errorf("refused HTTPS redirect %q", raw)
		}
	}
}

func TestTokenResourceIsOptionalAndRefusalsNameTheCheck(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	server := f.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	code, verifier, _ := f.issueCode(t, client)
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "resource": {"https://attacker.example/mcp"}, "code": {code}, "code_verifier": {verifier}}
	response := connectorForm(t, server, f.issuer+"/oauth/token", values, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(f.logs.String(), `"reason":"token:invalid_target"`) {
		t.Fatalf("wrong resource: HTTP %d, audit %s", response.Code, f.logs.String())
	}
	values.Del("resource")
	response = connectorForm(t, server, f.issuer+"/oauth/token", values, nil)
	var tokens connectorTestTokens
	connectorDecode(t, response, &tokens)
	if response.Code != http.StatusOK || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("token without resource: HTTP %d %s", response.Code, response.Body.String())
	}
	if f.ping(t, server, tokens.AccessToken).Code != http.StatusOK {
		t.Fatal("token issued without resource does not reach the configured resource")
	}
}
