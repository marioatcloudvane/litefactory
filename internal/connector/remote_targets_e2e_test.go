package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// TestRemoteRetainedTargetReadAndRevocation crosses the public MCP handlers,
// durable consent/token state, retained target lookup and protected release.
func TestRemoteRetainedTargetReadAndRevocation(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, fixture.db)
	foreignRepo, err := fixture.db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "foreign"), Forge: store.ForgeGitHub, Owner: "acme", Name: "other", RemoteURL: "git@github.com:acme/other.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	foreignSession, err := fixture.db.GetOrCreateSession(t.Context(), foreignRepo, 8)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var grantID string
	revokeDuringAcquisition := false
	fixture.options.TaskEvidence = fixtureEvidence(func(ctx context.Context, got store.ConnectorGrantTarget, request EvidenceRequest) (EvidencePage, error) {
		calls++
		if got.TaskRef != target.TaskRef || got.RepoID != target.RepoID || got.RepositoryRef != target.RepositoryRef {
			t.Fatalf("provider received an unbound retained target: %+v", got)
		}
		if revokeDuringAcquisition {
			if err := fixture.db.RevokeConnectorGrant(ctx, 42, grantID, fixture.now); err != nil {
				return EvidencePage{}, err
			}
		}
		return EvidencePage{Revision: "retained-revision", Items: []map[string]any{{"id": "retained-item", "text": "protected-retained-marker"}}}, nil
	})
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "retained reader", "none")
	token := consentRetainedReader(t, fixture, server, client, target)
	grants, err := fixture.db.ListConnectorGrants(t.Context(), 42, fixture.now)
	if err != nil || len(grants) != 1 {
		t.Fatalf("durable consent grant: %v %v", grants, err)
	}
	grantID = grants[0].ID
	call := func(accessToken, task string) *httptest.ResponseRecorder {
		fixture.now = fixture.now.Add(time.Second)
		var headers http.Header
		if accessToken != "" {
			headers = connectorBearer(accessToken)
		}
		return connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": map[string]any{"task_ref": task, "section": "status"}}}, headers)
	}
	authorized := call(token, target.TaskRef)
	var result struct {
		Result struct {
			IsError    bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
		} `json:"result"`
	}
	connectorDecode(t, authorized, &result)
	if authorized.Code != http.StatusOK || result.Result.IsError || !strings.Contains(string(result.Result.Structured), "protected-retained-marker") || calls != 1 {
		t.Fatalf("authorized retained read failed: HTTP %d %s (provider calls %d)", authorized.Code, authorized.Body.String(), calls)
	}
	for _, request := range []struct{ token, task string }{{"", target.TaskRef}, {token, "github:acme/api#999"}, {token, foreignSession.TaskRef}} {
		response := call(request.token, request.task)
		if strings.Contains(response.Body.String(), "protected-retained-marker") || calls != 1 {
			t.Fatalf("unauthorized target reached acquisition or released evidence: HTTP %d %s", response.Code, response.Body.String())
		}
		if request.token == "" && response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous request: HTTP %d", response.Code)
		}
		if request.token != "" && !strings.Contains(response.Body.String(), "Access refused") {
			t.Fatalf("missing retained target did not refuse: %s", response.Body.String())
		}
	}
	revokeDuringAcquisition = true
	revoked := call(token, target.TaskRef)
	if calls != 2 || strings.Contains(revoked.Body.String(), "protected-retained-marker") || !strings.Contains(revoked.Body.String(), "Access unavailable") {
		t.Fatalf("revocation during acquisition released evidence: HTTP %d %s (provider calls %d)", revoked.Code, revoked.Body.String(), calls)
	}
	subsequent := call(token, target.TaskRef)
	if subsequent.Code != http.StatusUnauthorized || calls != 2 || strings.Contains(subsequent.Body.String(), "protected-retained-marker") {
		t.Fatalf("revoked grant reached acquisition: HTTP %d %s", subsequent.Code, subsequent.Body.String())
	}
}

// consentRetainedReader keeps the main path on the browser, consent and token
// routes. Only the fresh upstream identity provider is a controlled fixture.
func consentRetainedReader(t *testing.T, f *connectorFixture, server http.Handler, client connectorTestClient, target store.ConnectorGrantTarget) string {
	t.Helper()
	verifier := strings.Repeat("v", 43)
	values := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "scope": {DiscussionScope}, "resource": {f.options.Config.ResourceURL}, "state": {"retained-reader"}, "code_challenge": {digest(verifier)}, "code_challenge_method": {"S256"}}
	start := connectorRequest(t, server, http.MethodGet, f.issuer+"/oauth/authorize?"+values.Encode(), "", "", nil)
	if start.Code != http.StatusFound {
		t.Fatalf("authorize: HTTP %d %s", start.Code, start.Body.String())
	}
	upstream, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cookies := start.Result().Cookies()
	callback := connectorRequest(t, server, http.MethodGet, f.issuer+"/oauth/github/callback?"+url.Values{"code": {"controlled-identity-code"}, "state": {upstream.Query().Get("state")}}.Encode(), "", "", nil, cookies...)
	if callback.Code != http.StatusOK {
		t.Fatalf("identity callback: HTTP %d %s", callback.Code, callback.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token"[^>]*value="([^"]+)"`).FindStringSubmatch(callback.Body.String())
	if len(csrf) != 2 {
		t.Fatal("missing consent CSRF")
	}
	if next := callback.Result().Cookies(); len(next) > 0 {
		cookies = next
	}
	consent := connectorForm(t, server, f.issuer+"/oauth/consent", url.Values{"csrf_token": {csrf[1]}, "decision": {"allow"}, "scope": {DiscussionScope}, "duration": {"1"}, "repository": {target.RepositoryRef}}, nil, cookies...)
	if consent.Code != http.StatusFound {
		t.Fatalf("consent: HTTP %d %s", consent.Code, consent.Body.String())
	}
	redirect, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	exchange := connectorForm(t, server, f.issuer+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://client.example/callback"}, "resource": {f.options.Config.ResourceURL}, "code": {redirect.Query().Get("code")}, "code_verifier": {verifier}}, nil)
	var tokens connectorTestTokens
	connectorDecode(t, exchange, &tokens)
	if exchange.Code != http.StatusOK || tokens.AccessToken == "" || tokens.Scope != DiscussionScope {
		t.Fatalf("token exchange: HTTP %d %s", exchange.Code, exchange.Body.String())
	}
	return tokens.AccessToken
}
