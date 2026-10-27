package usageview_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/usageview"
)

type ownerIdentity struct{}

func (ownerIdentity) AuthorizationURL(state, challenge, redirect string) string {
	return "https://github.example/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}, "redirect_uri": {redirect}}.Encode()
}
func (ownerIdentity) Verify(context.Context, string, string, string) (int64, error) { return 42, nil }

type grantControl struct {
	db  *store.DB
	now time.Time
}

func (g grantControl) Ready(context.Context) error { return nil }
func (g grantControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return g.db.ListConnectorGrants(ctx, 42, g.now)
}
func (g grantControl) Revoke(ctx context.Context, id string) error {
	return g.db.RevokeConnectorGrant(ctx, 42, id, g.now)
}
func (g grantControl) RevokeAll(ctx context.Context) error {
	return g.db.RevokeAllConnectorGrants(ctx, 42, g.now)
}

// TestOwnerConsentRecordedUsage is the automated owner path through browser
// consent, OAuth, MCP and the recorded meter. Only GitHub identity is replaced.
func TestOwnerConsentRecordedUsage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond)
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "consent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "clone"), Forge: "github", Owner: "owner", Name: "project", BaseBranch: "main", FeaturePrefix: "lf"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 17)
	if err != nil {
		t.Fatal(err)
	}
	seedUsage(t, db, session, 1)
	other, err := db.GetOrCreateSession(ctx, repo, 18)
	if err != nil {
		t.Fatal(err)
	}
	seedUsage(t, db, other, 2)
	otherRepo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "other"), Forge: "github", Owner: "owner", Name: "other", BaseBranch: "main", FeaturePrefix: "lf"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := db.GetOrCreateSession(ctx, otherRepo, 17)
	if err != nil {
		t.Fatal(err)
	}
	seedUsage(t, db, unrelated, 3)
	historical, err := db.ListMeterPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetMeterPrices(ctx, "codex", "usage-model", usageRates("99")); err != nil {
		t.Fatal(err)
	}
	before := usageState(t, db, session, other, unrelated)
	local, err := connector.NewLocalControl(ctx, db, grantControl{db, now})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "fixture-client"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	service := usageview.New(db, redact.New(redact.Options{Environ: []string{}}))
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now }, LocalControl: local, Usage: service, GitHub: ownerIdentity{}, CookieKey: bytes.Repeat([]byte{8}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	handler := server.Handler()
	issuer := strings.TrimSuffix(cfg.ResourceURL, "/mcp")
	request := func(method, target, contentType, body, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		now = now.Add(time.Second) // Model a paced owner conversation without wall-clock sleeps.
		r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", contentType)
		r.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("MCP-Protocol-Version", "2025-11-25")
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	jsonCall := func(body any, token string) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		return request(http.MethodPost, cfg.ResourceURL, "application/json", string(raw), token)
	}
	registration := request(http.MethodPost, issuer+"/oauth/register", "application/json", `{"client_name":"usage test","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`, "")
	var client struct {
		ID string `json:"client_id"`
	}
	if err = json.Unmarshal(registration.Body.Bytes(), &client); err != nil || registration.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", registration.Code, registration.Body.String())
	}
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	values := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "scope": {"factory:usage repository:usage task:usage"}, "resource": {cfg.ResourceURL}, "state": {"client-state"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	start := request(http.MethodGet, issuer+"/oauth/authorize?"+values.Encode(), "", "", "")
	if start.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", start.Code, start.Body.String())
	}
	login, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cookies := start.Result().Cookies()
	callback := request(http.MethodGet, issuer+"/oauth/github/callback?"+url.Values{"code": {"test-github-code"}, "state": {login.Query().Get("state")}}.Encode(), "", "", "", cookies...)
	if callback.Code != http.StatusOK {
		t.Fatalf("callback: %d %s", callback.Code, callback.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token"[^>]*value="([^"]+)"`).FindStringSubmatch(callback.Body.String())
	if len(csrf) != 2 {
		t.Fatal("consent lacks CSRF")
	}
	if next := callback.Result().Cookies(); len(next) > 0 {
		cookies = next
	}
	grants, err := db.ListConnectorGrants(ctx, 42, now)
	if err != nil || len(grants) != 0 {
		t.Fatalf("grant before Allow: %v %v", grants, err)
	}
	consent := request(http.MethodPost, issuer+"/oauth/consent", "application/x-www-form-urlencoded", url.Values{"csrf_token": {csrf[1]}, "decision": {"allow"}, "scope": {"factory:usage", "repository:usage", "task:usage"}, "duration": {"1"}, "usage_repository_ref": {"github:owner/project"}, "usage_task_ref": {session.TaskRef}}.Encode(), "", cookies...)
	if consent.Code != http.StatusFound {
		t.Fatalf("consent: %d %s", consent.Code, consent.Body.String())
	}
	redirect, err := url.Parse(consent.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	tokenResponse := request(http.MethodPost, issuer+"/oauth/token", "application/x-www-form-urlencoded", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "resource": {cfg.ResourceURL}, "code": {redirect.Query().Get("code")}, "code_verifier": {verifier}}.Encode(), "")
	var tokens struct {
		Access string `json:"access_token"`
	}
	if err = json.Unmarshal(tokenResponse.Body.Bytes(), &tokens); err != nil || tokenResponse.Code != http.StatusOK || tokens.Access == "" {
		t.Fatalf("token: %d %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	initialize := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "owner-e2e", "version": "1"}}}, tokens.Access)
	if initialize.Code != http.StatusOK {
		t.Fatalf("initialize: %d %s", initialize.Code, initialize.Body.String())
	}
	listed := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, tokens.Access)
	if !strings.Contains(listed.Body.String(), "usage_read") || strings.Contains(listed.Body.String(), "factory_overview") {
		t.Fatalf("usage discovery: %s", listed.Body.String())
	}
	for i, tc := range []struct {
		scope, amount string
		issue         int
	}{{"factory", "0.000006000000", 0}, {"repository", "0.000003000000", 0}, {"task", "0.000001", 17}} {
		args := map[string]any{"scope": tc.scope, "period": "all_history", "view": "details"}
		if tc.scope != "factory" {
			args["repository"] = "github:owner/project"
		}
		if tc.issue > 0 {
			args["issue"] = tc.issue
		}
		response := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 10 + i, "method": "tools/call", "params": map[string]any{"name": "usage_read", "arguments": args}}, tokens.Access)
		var out struct {
			Result struct {
				IsError    bool `json:"isError"`
				Structured struct {
					Summary  string `json:"summary"`
					Evidence struct {
						Totals     report.MeterSummary `json:"totals"`
						PriceBasis []json.RawMessage   `json:"price_basis"`
						Items      []json.RawMessage   `json:"items"`
					} `json:"evidence"`
				} `json:"structuredContent"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &out); err != nil || response.Code != http.StatusOK || len(out.Error) > 0 || out.Result.IsError {
			t.Fatalf("%s read: %d %s (%v)", tc.scope, response.Code, response.Body.String(), err)
		}
		facts := out.Result.Structured
		if facts.Evidence.Totals.EstimatedUSD == nil || *facts.Evidence.Totals.EstimatedUSD != tc.amount {
			t.Fatalf("%s frozen estimate: %s", tc.scope, response.Body.String())
		}
		if len(facts.Evidence.PriceBasis) == 0 || len(facts.Evidence.Items) == 0 {
			t.Fatalf("%s lacks supporting evidence: %s", tc.scope, response.Body.String())
		}
		items, err := json.Marshal(facts.Evidence.Items)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(items), "github:owner/project") || strings.Contains(string(items), "github:owner/other") != (tc.scope == "factory") {
			t.Fatalf("%s lost repository attribution or scope isolation: %s", tc.scope, items)
		}
		basis, _ := json.Marshal(facts.Evidence.PriceBasis)
		if !strings.Contains(string(basis), `"usd_per_million":"1"`) || strings.Contains(string(basis), `"usd_per_million":"99"`) {
			t.Fatalf("%s used current instead of historical rates: %s", tc.scope, basis)
		}
		if len([]rune(facts.Summary)) > 1000 || response.Body.Len() > 128<<10 {
			t.Fatal("summary or complete response exceeded budget")
		}
		if len(facts.Evidence.Totals.PriceRevisions) != 1 || facts.Evidence.Totals.PriceRevisions[0] != historical.RevisionID {
			t.Fatalf("wrong pinned revision: %+v", facts.Evidence.Totals.PriceRevisions)
		}
	}
	// Exercise the existing MCP client contract for every period and a follow-up.
	// Each first page captures new bounds; navigation keeps that same observation.
	for _, period := range []string{"today", "rolling_7_days", "rolling_30_days", "all_history", "", "explicit"} {
		args := map[string]any{"scope": "factory", "view": "details", "limit": 1}
		observed := now.Add(time.Second)
		var from, to *time.Time
		switch period {
		case "all_history":
			args["period"] = period
		case "explicit":
			start, end := observed.Add(-time.Hour), observed.Add(time.Hour)
			from, to = &start, &end
			zone := time.FixedZone("fixture", 2*60*60)
			args["from"], args["to"] = start.In(zone).Format(time.RFC3339Nano), end.In(zone).Format(time.RFC3339Nano)
		default:
			end := observed
			start := end.Add(-720 * time.Hour)
			if period == "today" {
				start = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
			}
			if period == "rolling_7_days" {
				start = end.Add(-168 * time.Hour)
			}
			from, to = &start, &end
			if period != "" {
				args["period"] = period
			}
		}
		type page struct {
			Result struct {
				IsError    bool `json:"isError"`
				Structured struct {
					Evidence struct {
						Bounds struct {
							From, To      *time.Time
							Zone          string
							FromInclusive bool `json:"from_inclusive"`
							ToExclusive   bool `json:"to_exclusive"`
						} `json:"bounds"`
						Totals   report.MeterSummary `json:"totals"`
						Revision string              `json:"revision"`
						Items    []json.RawMessage   `json:"items"`
						Cursor   string              `json:"next_cursor"`
						HasMore  bool                `json:"has_more"`
					} `json:"evidence"`
				} `json:"structuredContent"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		read := func() page {
			t.Helper()
			response := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 50, "method": "tools/call", "params": map[string]any{"name": "usage_read", "arguments": args}}, tokens.Access)
			var out page
			if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil || response.Code != http.StatusOK || out.Result.IsError || len(out.Error) > 0 {
				t.Fatalf("period %q: %d %s (%v)", period, response.Code, response.Body.String(), err)
			}
			return out
		}
		first := read().Result.Structured.Evidence
		sameTime := func(a, b *time.Time) bool { return a == nil && b == nil || a != nil && b != nil && a.Equal(*b) }
		if !sameTime(first.Bounds.From, from) || !sameTime(first.Bounds.To, to) || first.Bounds.Zone != "UTC" || !first.Bounds.FromInclusive || !first.Bounds.ToExclusive {
			t.Fatalf("period %q wrong resolved UTC bounds: %+v", period, first.Bounds)
		}
		if first.Cursor == "" || !first.HasMore || len(first.Items) != 1 {
			t.Fatalf("period %q lacks bounded supporting continuation", period)
		}
		args["cursor"] = first.Cursor
		second := read().Result.Structured.Evidence
		replay := read().Result.Structured.Evidence
		secondEvidence, replayEvidence := second, replay
		secondEvidence.Cursor, replayEvidence.Cursor = "", "" // Opaque encrypted continuations use independent nonces.
		secondRaw, _ := json.Marshal(secondEvidence)
		replayRaw, _ := json.Marshal(replayEvidence)
		if !bytes.Equal(secondRaw, replayRaw) {
			t.Fatalf("period %q live cursor replay changed evidence", period)
		}
		if !sameTime(first.Bounds.From, second.Bounds.From) || !sameTime(first.Bounds.To, second.Bounds.To) || first.Revision != second.Revision || second.Totals.EstimatedUSD == nil || first.Totals.EstimatedUSD == nil || *first.Totals.EstimatedUSD != *second.Totals.EstimatedUSD {
			t.Fatalf("period %q continuation changed snapshot", period)
		}
		args["cursor"] = second.Cursor
		last := read().Result.Structured.Evidence
		if last.HasMore || last.Cursor != "" || len(last.Items) != 1 {
			t.Fatalf("period %q final page did not complete", period)
		}
	}
	if after := usageState(t, db, session, other, unrelated); !bytes.Equal(before, after) {
		t.Fatal("usage read changed recorded usage, current prices, sessions or runs")
	}

	// A follow-up asks why this task's full cost is unknown. Add a retained
	// attempt with unavailable usage, then verify the client sees both the
	// already known subtotal and the gap rather than a fabricated full total.
	runs, err := db.ListRuns(ctx, session.ID)
	if err != nil || len(runs) == 0 {
		t.Fatalf("fixture runs: %v", err)
	}
	missing, err := db.StartMeterAttempt(ctx, runs[0].ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishMeterAttempt(ctx, missing.ID, "success", "unknown", "fixture", "fixture", []string{"usage_unavailable"}); err != nil {
		t.Fatal(err)
	}
	uncertainBefore := usageState(t, db, session, other, unrelated)
	uncertain := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 88, "method": "tools/call", "params": map[string]any{"name": "usage_read", "arguments": map[string]any{"scope": "task", "repository": "github:owner/project", "issue": 17, "period": "all_history"}}}, tokens.Access)
	var uncertainAnswer struct {
		Result struct {
			IsError    bool `json:"isError"`
			Structured struct {
				Summary  string `json:"summary"`
				Evidence struct {
					Totals report.MeterSummary `json:"totals"`
					Gaps   []string            `json:"gaps"`
				} `json:"evidence"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err = json.Unmarshal(uncertain.Body.Bytes(), &uncertainAnswer); err != nil {
		t.Fatal(err)
	}
	answer := uncertainAnswer.Result.Structured
	if uncertain.Code != http.StatusOK || uncertainAnswer.Result.IsError || answer.Evidence.Totals.EstimatedUSD != nil || answer.Evidence.Totals.PricedSubtotalUSD == nil || *answer.Evidence.Totals.PricedSubtotalUSD != "0.000001000000" || len(answer.Evidence.Gaps) == 0 || answer.Summary == "" {
		t.Fatalf("unknown total explanation: %s", uncertain.Body.String())
	}
	if after := usageState(t, db, session, other, unrelated); !bytes.Equal(uncertainBefore, after) {
		t.Fatal("uncertainty explanation changed retained records")
	}
	if err = db.RevokeAllConnectorGrants(ctx, 42, now); err != nil {
		t.Fatal(err)
	}
	denied := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 99, "method": "tools/call", "params": map[string]any{"name": "usage_read", "arguments": map[string]any{"scope": "factory"}}}, tokens.Access)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("revoked usage grant: %d %s", denied.Code, denied.Body.String())
	}
}

func usageRates(price string) []store.MeterPriceRate {
	rates := []store.MeterPriceRate{}
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, store.MeterPriceRate{CodingAgent: "codex", Model: "usage-model", Dimension: dimension, USDPerMillion: price})
	}
	return rates
}

func seedUsage(t *testing.T, db *store.DB, session store.Session, output int64) {
	t.Helper()
	ctx := t.Context()
	// Publish the fixture rate before the first attempt; later attempts retain it.
	catalog, err := db.ListMeterPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range catalog.Rates {
		if r.Model == "usage-model" {
			found = true
		}
	}
	if !found {
		if _, err = db.SetMeterPrices(ctx, "codex", "usage-model", usageRates("1")); err != nil {
			t.Fatal(err)
		}
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "usage", store.AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(int) string { return "/private/fixture/usage" }})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ID: "root", SourceID: "session", SourceInvocationID: "root", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	model := "usage-model"
	observation := agent.UsageObservation{AttemptID: attempt.ID, SourceID: "session", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &zero, Output: &output, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete, SourceFormat: "fixture"}}
	if err = db.RecordMeterEvent(ctx, attempt.ID, "codex", agent.UsageEvent{Observation: &observation}); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishMeterAttempt(ctx, attempt.ID, "success", "complete", "fixture", "fixture", nil); err != nil {
		t.Fatal(err)
	}
}

func usageState(t *testing.T, db *store.DB, sessions ...store.Session) []byte {
	t.Helper()
	ctx := t.Context()
	meter, err := meterview.Build(ctx, db, store.MeterFilter{}, true)
	if err != nil {
		t.Fatal(err)
	}
	prices, err := db.ListMeterPrices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state := []any{meter, prices}
	for _, s := range sessions {
		session, err := db.GetSession(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		runs, err := db.ListRuns(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		state = append(state, session, runs)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
