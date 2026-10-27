package connector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// verifiedConsent runs the browser journey to the consent page and returns the
// page response, its CSRF token and the verified cookie.
func verifiedConsent(t *testing.T) (browserJourney, *httptest.ResponseRecorder, string, *http.Cookie) {
	t.Helper()
	j := beginBrowserJourney(t, browserIdentity{owner: 42})
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", page.Code, page.Body.String())
	}
	match := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(match) != 2 {
		t.Fatal("consent has no CSRF token")
	}
	return j, page, match[1], page.Result().Cookies()[0]
}

func postConsent(t *testing.T, j browserJourney, csrf string, cookie *http.Cookie, extra url.Values) int {
	t.Helper()
	values := url.Values{"csrf_token": {csrf}, "decision": {"allow"}}
	for k, v := range extra {
		values[k] = v
	}
	return connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", values, nil, cookie).Code
}

func TestConsentPageOffersScopesAndDurationsAndAllowsOnlyItsOwnInlineAssets(t *testing.T) {
	t.Parallel()
	_, result, _, _ := verifiedConsent(t)
	page := result.Body.String()
	boxes := consentCheckboxes(t, page)
	if len(boxes) != len(KnownScopes) {
		t.Fatalf("consent offers %d scopes, want all %d known: %v", len(boxes), len(KnownScopes), boxes)
	}
	for i, known := range KnownScopes {
		if boxes[i].value != known.Name || boxes[i].checked != (known.Name == Scope) {
			t.Errorf("scope %d: %+v, want %s checked=%v", i, boxes[i], known.Name, known.Name == Scope)
		}
	}
	if !strings.Contains(page, `name="repositories" value="all"`) {
		t.Error("consent lacks the repository selection for the task-level and repository-level scopes")
	}
	for _, months := range GrantDurations {
		if !strings.Contains(page, `name="duration" value="`+strconv.Itoa(months)+`"`) {
			t.Errorf("consent lacks duration %d", months)
		}
	}
	if !regexp.MustCompile(`name="duration" value="1" checked`).MatchString(page) {
		t.Error("one month is not the default duration")
	}
	csp := result.Header().Get("Content-Security-Policy")
	for _, tag := range []string{"style", "script"} {
		blocks := regexp.MustCompile(`(?s)<`+tag+`>(.*?)</`+tag+`>`).FindAllStringSubmatch(page, -1)
		if len(blocks) != 1 {
			t.Fatalf("%d inline %s blocks", len(blocks), tag)
		}
		sum := sha256.Sum256([]byte(blocks[0][1]))
		if !strings.Contains(csp, tag+"-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'") {
			t.Errorf("CSP %q does not allow the page's inline %s", csp, tag)
		}
	}
	for _, directive := range []string{"default-src 'none'", "form-action 'self' https://client.example;", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP lost %q", directive)
		}
	}
}

func TestConsentRefusesAllowWithoutAValidScope(t *testing.T) {
	t.Parallel()
	j, _, csrf, cookie := verifiedConsent(t)
	for name, scope := range map[string][]string{
		"no scope":           nil,
		"empty scope":        {""},
		"unknown scope":      {"factory:write"},
		"two scopes in one":  {Scope + " factory:write"},
		"repeated scope":     {Scope, Scope},
		"known plus unknown": {Scope, "factory:write"},
	} {
		extra := url.Values{"duration": {"1"}}
		if scope != nil {
			extra["scope"] = scope
		}
		if code := postConsent(t, j, csrf, cookie, extra); code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d", name, code)
		}
	}
	browserNoGrants(t, j.fixture)
	if !strings.Contains(j.fixture.logs.String(), `"reason":"consent:invalid_scope"`) {
		t.Errorf("scope refusal not named in audit: %s", j.fixture.logs.String())
	}
	// A refused allow leaves the pending consent usable.
	if code := postConsent(t, j, csrf, cookie, url.Values{"scope": {Scope}, "duration": {"1"}}); code != http.StatusFound {
		t.Fatalf("valid allow after refusals: HTTP %d", code)
	}
}

type consentCheckbox struct {
	value   string
	checked bool
}

// consentCheckboxes returns the page's scope checkboxes in order.
func consentCheckboxes(t *testing.T, page string) []consentCheckbox {
	t.Helper()
	var boxes []consentCheckbox
	for _, input := range regexp.MustCompile(`<input [^>]*name="scope"[^>]*>`).FindAllString(page, -1) {
		value := regexp.MustCompile(`value="([^"]*)"`).FindStringSubmatch(input)
		if len(value) != 2 || !strings.Contains(input, `type="checkbox"`) {
			t.Fatalf("malformed scope input %q", input)
		}
		boxes = append(boxes, consentCheckbox{value: value[1], checked: regexp.MustCompile(`\schecked[\s>]`).MatchString(input)})
	}
	return boxes
}

func TestConsentPagePreTicksEveryRequestedScope(t *testing.T) {
	t.Parallel()
	requested := DiscussionScope + " " + FactoryUsageScope
	j := beginBrowserJourneyScopes(t, browserIdentity{owner: 42}, requested)
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", page.Code, page.Body.String())
	}
	boxes := consentCheckboxes(t, page.Body.String())
	if len(boxes) != len(KnownScopes) {
		t.Fatalf("consent offers %v, want all known scopes", boxes)
	}
	for i, known := range KnownScopes {
		want := store.ConnectorScopeHas(requested, known.Name)
		if boxes[i].value != known.Name || boxes[i].checked != want {
			t.Errorf("scope %d: %+v, want %s checked=%v", i, boxes[i], known.Name, want)
		}
	}
}

func TestConsentScopeAcceptsAnyKnownScope(t *testing.T) {
	t.Parallel()
	if _, ok := consentScope([]string{"factory:other"}); ok {
		t.Fatal("granted an unknown scope")
	}
	if _, ok := consentScope(nil); ok {
		t.Fatal("granted no scope")
	}
	if got, ok := consentScope([]string{FactoryUsageScope, Scope}); !ok || got != Scope+" "+FactoryUsageScope {
		t.Fatalf("known scopes refused or out of catalogue order: %q %v", got, ok)
	}
}

// The store must accept exactly the scopes the connector can offer.
func TestStoreKnowsEveryConnectorScope(t *testing.T) {
	t.Parallel()
	if got := store.ConnectorKnownScopes(); !slices.Equal(got, scopeNames()) {
		t.Fatalf("store scopes %v, connector scopes %v", got, scopeNames())
	}
}

// RFC 6749 §3.3: the owner may grant a known scope the client did not
// request. The grant, the token response and the tools follow the owner's
// choice, also when the client repeats its narrower request at the token
// endpoint and on refresh.
func TestOwnerGrantsKnownScopeTheClientDidNotRequest(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	f.options.Usage = &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) { return usageTestProjection(1), nil }}
	j := beginBrowserJourneyFixture(t, f, browserIdentity{owner: 42}, Scope)
	page := j.callback(t, j.state)
	if page.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", page.Code, page.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page.Body.String())[1]
	cookie := page.Result().Cookies()[0]
	values := url.Values{"csrf_token": {csrf}, "decision": {"allow"}, "scope": {Scope, FactoryUsageScope}, "duration": {"1"}}
	consent := connectorForm(t, j.handler, f.issuer+"/oauth/consent", values, nil, cookie)
	if consent.Code != http.StatusFound {
		t.Fatalf("allow: HTTP %d %s", consent.Code, consent.Body.String())
	}
	granted := Scope + " " + FactoryUsageScope
	grants, err := f.db.ListConnectorGrants(t.Context(), 42, f.now)
	if err != nil || len(grants) != 1 || grants[0].Scope != granted {
		t.Fatalf("grants %+v %v, want one %q grant", grants, err, granted)
	}
	location, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	clientID := grants[0].ClientID
	exchange := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {Scope}, "code": {location.Query().Get("code")}, "code_verifier": {strings.Repeat("v", 43)}}
	response := connectorForm(t, j.handler, f.issuer+"/oauth/token", exchange, nil)
	var tokens connectorTestTokens
	connectorDecode(t, response, &tokens)
	if response.Code != http.StatusOK || tokens.Scope != granted {
		t.Fatalf("token: HTTP %d scope %q, want %q", response.Code, tokens.Scope, granted)
	}
	call := connectorJSON(t, j.handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "usage_read", "arguments": map[string]any{"scope": "factory"}}}, connectorBearer(tokens.AccessToken))
	var result struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	connectorDecode(t, call, &result)
	if call.Code != http.StatusOK || result.Error != nil || result.Result.IsError {
		t.Fatalf("usage_read under an owner-added scope: HTTP %d %s", call.Code, call.Body.String())
	}
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "scope": {Scope}, "refresh_token": {tokens.RefreshToken}}
	response = connectorForm(t, j.handler, f.issuer+"/oauth/token", refresh, nil)
	var successor connectorTestTokens
	connectorDecode(t, response, &successor)
	if response.Code != http.StatusOK || successor.Scope != granted {
		t.Fatalf("refresh: HTTP %d scope %q, want %q", response.Code, successor.Scope, granted)
	}
}

// Selections belong to their scopes: an unrequested scope the owner ticks
// still needs its selection, and an unticked one needs none.
func TestConsentSelectionsRequiredOnlyWhenTicked(t *testing.T) {
	t.Parallel()
	j, _, csrf, cookie := verifiedConsent(t)
	for name, extra := range map[string]url.Values{
		"repository usage without repository": {"scope": {Scope, RepositoryUsageScope}},
		"task usage without repository":       {"scope": {TaskUsageScope}},
		"discussion without repository":       {"scope": {DiscussionScope}},
		"code review without discussion":      {"scope": {CodeReviewScope}, "repositories": {"all"}},
	} {
		extra.Set("duration", "1")
		if code := postConsent(t, j, csrf, cookie, extra); code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d", name, code)
		}
	}
	browserNoGrants(t, j.fixture)
	// A repository selection does not block Allow for scopes that take none.
	if code := postConsent(t, j, csrf, cookie, url.Values{"scope": {Scope}, "duration": {"1"}, "repository": {"github:missing/repo"}}); code != http.StatusFound {
		t.Fatalf("allow with empty unticked selections: HTTP %d", code)
	}
}

func TestChallengeNamesEveryKnownScope(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	server := f.server(t)
	response := connectorJSON(t, server, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}, nil)
	header := response.Header().Get("WWW-Authenticate")
	if response.Code != http.StatusUnauthorized || !strings.HasPrefix(header, "Bearer ") || !strings.Contains(header, `resource_metadata="`) || !strings.Contains(header, `scope="`+strings.Join(scopeNames(), " ")+`"`) {
		t.Fatalf("challenge: HTTP %d %q", response.Code, header)
	}
}

func TestConsentDurationSetsGrantExpiry(t *testing.T) {
	t.Parallel()
	for _, months := range GrantDurations {
		t.Run(strconv.Itoa(months), func(t *testing.T) {
			t.Parallel()
			j, _, csrf, cookie := verifiedConsent(t)
			if code := postConsent(t, j, csrf, cookie, url.Values{"scope": {Scope}, "duration": {strconv.Itoa(months)}}); code != http.StatusFound {
				t.Fatalf("allow: HTTP %d", code)
			}
			grants, err := j.fixture.db.ListConnectorGrants(t.Context(), 42, j.fixture.now)
			if err != nil {
				t.Fatal(err)
			}
			want := j.fixture.now.UTC().AddDate(0, months, 0)
			if len(grants) != 1 || !grants[0].ExpiresAt.Equal(want) || grants[0].Scope != Scope {
				t.Fatalf("grants %+v, want one %s grant expiring %s", grants, Scope, want)
			}
		})
	}
}

// Each journey stays under the per-source burst of ten requests.
func TestConsentRefusesOtherDurations(t *testing.T) {
	t.Parallel()
	j, _, csrf, cookie := verifiedConsent(t)
	for _, duration := range []string{"", "0", "2", "01", "36", "720h"} {
		extra := url.Values{"scope": {Scope}}
		if duration != "" {
			extra.Set("duration", duration)
		}
		if code := postConsent(t, j, csrf, cookie, extra); code != http.StatusBadRequest {
			t.Errorf("duration %q: HTTP %d", duration, code)
		}
	}
	browserNoGrants(t, j.fixture)
}

func TestConsentDenyNeedsNoScopeOrDuration(t *testing.T) {
	t.Parallel()
	j, _, csrf, cookie := verifiedConsent(t)
	response := connectorForm(t, j.handler, j.fixture.issuer+"/oauth/consent", url.Values{"csrf_token": {csrf}, "decision": {"deny"}}, nil, cookie)
	if response.Code != http.StatusFound || !strings.Contains(response.Header().Get("Location"), "error=access_denied") {
		t.Fatalf("deny: HTTP %d %s", response.Code, response.Header().Get("Location"))
	}
	browserNoGrants(t, j.fixture)
}

func TestTokenRefusesUnknownScope(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	server := f.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	code, verifier, _ := f.issueCode(t, client)
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {"factory:write"}, "code": {code}, "code_verifier": {verifier}}
	if response := connectorForm(t, server, f.issuer+"/oauth/token", values, nil); response.Code != http.StatusBadRequest || !strings.Contains(f.logs.String(), `"reason":"token:invalid_scope"`) {
		t.Fatalf("unknown token scope: HTTP %d", response.Code)
	}
	values.Set("scope", Scope)
	if response := connectorForm(t, server, f.issuer+"/oauth/token", values, nil); response.Code != http.StatusOK {
		t.Fatalf("exact grant scope refused: HTTP %d %s", response.Code, response.Body.String())
	}
}

// A grant whose scope lacks factory:overview cannot reach the overview tool,
// even though every other binding is valid.
func TestGrantWithoutOverviewScopeCannotCallOverview(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, connectorFixtureOverview(nil))
	server := f.server(t)
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	const other = FactoryUsageScope
	id, access, refresh := "no-overview", "access-credential-no-overview", "refresh-credential-no-overview"
	client := registerConnectorClient(t, server, "Client", "none")
	browser := store.ConnectorBrowser{ID: id, ClientID: client.ClientID, RedirectURI: "https://client.example/callback", Resource: f.options.Config.ResourceURL, Scope: other, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: f.now.Add(10 * time.Minute)}
	if err := f.db.CreateConnectorBrowser(t.Context(), browser, 42, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.db.VerifyConnectorBrowser(t.Context(), id, "nonce", "upstream", 42, f.now); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: id, ClientID: client.ClientID, Resource: browser.Resource, Scope: other, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: f.now, ExpiresAt: store.ConnectorGrantExpiry(f.now, 1)}
	code := store.ConnectorCode{Digest: "code", GrantID: id, ClientID: client.ClientID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: other, PKCEChallenge: "challenge", ExpiresAt: f.now.Add(time.Minute)}
	if err := f.db.ConsentConnectorBrowser(t.Context(), id, "nonce", 42, f.now, grant, code, true); err != nil {
		t.Fatal(err)
	}
	x := store.ConnectorExchange{Digest: "code", ClientID: client.ClientID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, PKCEChallenge: "challenge", OwnerID: 42, Now: f.now, Pair: store.ConnectorTokenPair{AccessDigest: digest(access), RefreshDigest: digest(refresh), AccessExpiresAt: f.now.Add(15 * time.Minute)}}
	if _, err := f.db.ExchangeConnectorCode(t.Context(), x); err != nil {
		t.Fatal(err)
	}
	call := connectorJSON(t, server, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "factory_overview", "arguments": map[string]any{}}}, connectorBearer(access))
	if call.Code != http.StatusForbidden || !strings.Contains(call.Header().Get("WWW-Authenticate"), "insufficient_scope") {
		t.Fatalf("grant without %s called the overview: HTTP %d", Scope, call.Code)
	}
}
