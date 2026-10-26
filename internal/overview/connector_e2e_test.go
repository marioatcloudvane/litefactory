package overview_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/overview"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// TestOwnerReadsFactoryOverview exercises the actual protected MCP handler,
// durable consent/token state, and local overview provider across repositories.
func TestOwnerReadsFactoryOverview(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "overview.db"))
	mustOverview(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mustOverview(t, db.Migrate(ctx))
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	mustOverview(t, err)
	var sessions []store.Session
	for i, name := range []string{"private-project", "later-registration"} {
		repo, e := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), name), Forge: "github", Owner: "owner", Name: name, RemoteURL: "https://github.com/owner/" + name, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "worktrees")})
		mustOverview(t, e)
		session, e := db.GetOrCreateSession(ctx, repo, 100+i)
		mustOverview(t, e)
		sessions = append(sessions, session)
	}
	started := time.Now().UTC().Add(-time.Hour)
	segment, err := db.GetOrCreateSegment(ctx, sessions[0].ID, 1, "overview-test", "claude")
	mustOverview(t, err)
	_, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: sessions[0].ID, SegmentID: segment.ID, Mode: "research", Agent: "claude", StartedAt: started, LogPath: func(int) string { return filepath.Join(t.TempDir(), "run.jsonl") }})
	mustOverview(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "overview", Mode: "implement", ProfileVersion: 1, Path: "implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: started})
	mustOverview(t, err)
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: sessions[1].ID, DefinitionID: definition.ID, CurrentNode: "waiting", StartedAt: started})
	mustOverview(t, err)
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "waiting", StartedAt: started})
	mustOverview(t, err)
	mustOverview(t, db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", started.Add(time.Minute)))
	mustOverview(t, db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "overview-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "waiting", Kind: "question", Candidate: []byte(`{"comment":"Choose the release window"}`)}))
	destination := "https://github.com/owner/later-registration/issues/101#issuecomment-123"
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "overview-question", SessionID: sessions[1].ID, Task: sessions[1].TaskRef, Destination: "github:owner/later-registration#101", TriggerNamespace: "graph", Purpose: "question", BotUserID: 9, Body: "Choose the release window", Marker: "overview-question-marker"})
	mustOverview(t, err)
	_, err = db.ConfirmReportDelivery(ctx, "overview-question", 123, destination)
	mustOverview(t, err)
	_, err = db.CompleteReportDelivery(ctx, "overview-question")
	mustOverview(t, err)
	now := time.Now().UTC().Add(time.Second)
	mustOverview(t, db.ReplaceHealthSnapshot(ctx, store.ListenerStatus{WrittenAt: now, DaemonStartedAt: started, LastHeartbeatStartedAt: now.Add(-time.Second), RunsInFlight: 1, SlotsUsed: 1, SlotsTotal: 4, RunsHeld: "disk hold"}, nil))
	provider := overview.New(db, redact.New(redact.Options{Environ: []string{}}), func() time.Time { return now })
	handler, token, resource := overviewConnector(t, db, provider, now)
	req := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factory_overview","arguments":{}}}`)).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	var result struct {
		Result connector.ToolResult `json:"result"`
		Error  json.RawMessage      `json:"error"`
	}
	mustOverview(t, json.Unmarshal(response.Body.Bytes(), &result))
	if response.Code != http.StatusOK || result.Result.IsError || len(result.Error) > 0 {
		t.Fatalf("overview failed: %d %s", response.Code, response.Body.String())
	}
	if response.Body.Len() > connector.MaxResponseBytes || len(result.Result.Content) != 1 {
		t.Fatal("overview response bounds")
	}
	var structured, text map[string]any
	mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &structured))
	mustOverview(t, json.Unmarshal([]byte(result.Result.Content[0].Text), &text))
	if !reflect.DeepEqual(structured, text) {
		t.Fatal("text and structured overview facts differ")
	}
	for _, section := range []string{"health", "activity", "owner_waits", "recent_publications", "coverage"} {
		if structured[section] == nil {
			t.Fatalf("missing %s", section)
		}
	}
	for _, fact := range []string{"private-project", "later-registration", "Choose the release window", destination, "disk hold"} {
		if !bytes.Contains(result.Result.StructuredContent, []byte(fact)) {
			t.Fatalf("missing retained fact %q", fact)
		}
	}
	for _, section := range []string{"activity", "owner_waits", "recent_publications"} {
		value, ok := structured[section].(map[string]any)
		if !ok {
			t.Fatalf("%s is not a bounded list", section)
		}
		items, ok := value["items"].([]any)
		if !ok || len(items) == 0 || len(items) > 50 {
			t.Fatalf("missing main-path %s items", section)
		}
		if _, ok := value["has_more"].(bool); !ok {
			t.Fatalf("%s omits completeness", section)
		}
	}
}

func mustOverview(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func overviewDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func overviewConnector(t *testing.T, db *store.DB, provider connector.OverviewProvider, now time.Time) (http.Handler, string, string) {
	t.Helper()
	ctx := t.Context()
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "overview-app"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	control, err := connector.NewLocalControl(ctx, db, connector.NewGrantService(db))
	mustOverview(t, err)
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: db, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Now: func() time.Time { return now }, LocalControl: control, Overview: provider, GitHubSecret: "test-only", CookieKey: bytes.Repeat([]byte{7}, 32)})
	mustOverview(t, err)
	handler := server.Handler()
	client := store.ConnectorClient{ID: "overview-client", Name: "Overview test", AuthMethod: "none", Redirects: []string{"https://client.example/callback"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	mustOverview(t, db.RegisterConnectorClient(ctx, client, now))
	owner, err := db.ConnectorOwner(ctx, 42)
	mustOverview(t, err)
	verifier := strings.Repeat("v", 43)
	browser := store.ConnectorBrowser{ID: "overview-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: cfg.ResourceURL, Scope: connector.Scope, PKCEChallenge: overviewDigest(verifier), NonceDigest: overviewDigest("nonce"), UpstreamStateDigest: overviewDigest("upstream"), OwnerGeneration: owner.Generation, ExpiresAt: now.Add(time.Minute)}
	mustOverview(t, db.CreateConnectorBrowser(ctx, browser, 42, now))
	mustOverview(t, db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, 42, now))
	grant := store.ConnectorGrant{ID: "overview-grant", ClientID: client.ID, Resource: cfg.ResourceURL, Scope: connector.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	code := store.ConnectorCode{Digest: overviewDigest("overview-code"), GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: cfg.ResourceURL, Scope: connector.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: now.Add(time.Minute)}
	mustOverview(t, db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, 42, now, grant, code, true))
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {browser.RedirectURI}, "resource": {cfg.ResourceURL}, "code": {"overview-code"}, "code_verifier": {verifier}}
	request := httptest.NewRequest(http.MethodPost, cfg.IssuerURL+"/oauth/token", strings.NewReader(values.Encode())).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	mustOverview(t, json.Unmarshal(response.Body.Bytes(), &tokens))
	if response.Code != http.StatusOK || tokens.AccessToken == "" {
		t.Fatalf("consented token exchange: %d %s", response.Code, response.Body.String())
	}
	return handler, tokens.AccessToken, cfg.ResourceURL
}
