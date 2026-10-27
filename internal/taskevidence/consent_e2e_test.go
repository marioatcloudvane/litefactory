package taskevidence_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskevidence"
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

// TestOwnerConsentTaskEvidence uses the real consent, token, MCP and projection
// handlers with a migrated temporary database; only GitHub identity is replaced.
func TestOwnerConsentTaskEvidence(t *testing.T) {
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
	binding := seedEvidence(t, db, session, now)
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
	service := taskevidence.New(db, redact.New(redact.Options{Environ: []string{}}), func() time.Time { return now })
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now }, LocalControl: local, TaskEvidence: service, GitHub: ownerIdentity{}, CookieKey: bytes.Repeat([]byte{8}, 32)})
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
	registration := request(http.MethodPost, issuer+"/oauth/register", "application/json", `{"client_name":"evidence test","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`, "")
	var client struct {
		ID string `json:"client_id"`
	}
	if err = json.Unmarshal(registration.Body.Bytes(), &client); err != nil || registration.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", registration.Code, registration.Body.String())
	}
	verifier := strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	values := url.Values{"response_type": {"code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "scope": {connector.DiscussionScope}, "resource": {cfg.ResourceURL}, "state": {"client-state"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
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
	consent := request(http.MethodPost, issuer+"/oauth/consent", "application/x-www-form-urlencoded", url.Values{"csrf_token": {csrf[1]}, "decision": {"allow"}, "scope": {connector.DiscussionScope}, "duration": {"1"}, "task_ref": {session.TaskRef}}.Encode(), "", cookies...)
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
	if !strings.Contains(listed.Body.String(), "task_evidence_read") || strings.Contains(listed.Body.String(), "factory_overview") {
		t.Fatalf("discussion discovery: %s", listed.Body.String())
	}
	for i, section := range []string{"status", "results", "questions", "research", "plan", "approvals"} {
		response := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 10 + i, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": map[string]any{"task_ref": session.TaskRef, "section": section}}}, tokens.Access)
		var envelope struct {
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
				IsError    bool            `json:"isError"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != http.StatusOK || len(envelope.Error) > 0 || envelope.Result.IsError {
			t.Fatalf("%s: %d %s", section, response.Code, response.Body.String())
		}
		if len(envelope.Result.Structured) == 0 || !strings.Contains(string(envelope.Result.Structured), section) {
			t.Fatalf("%s evidence missing: %s", section, response.Body.String())
		}
		var facts struct {
			Summary  string `json:"summary"`
			Evidence struct {
				TaskRef string           `json:"task_ref"`
				Section string           `json:"section"`
				Items   []map[string]any `json:"items"`
				Sources []map[string]any `json:"sources"`
			} `json:"evidence"`
		}
		if err := json.Unmarshal(envelope.Result.Structured, &facts); err != nil {
			t.Fatal(err)
		}
		if facts.Evidence.TaskRef != session.TaskRef || facts.Evidence.Section != section || len(facts.Evidence.Items) == 0 || len(facts.Evidence.Sources) == 0 {
			t.Fatalf("%s lost recorded items or provenance: %s", section, envelope.Result.Structured)
		}
		expected := map[string]string{"status": "idle", "results": "Recorded research result", "questions": "recorded-wait", "research": "Recorded research finding", "plan": "Recorded task plan", "approvals": revisionForApproval(t, db, session.TaskRef)}
		encodedItems, err := json.Marshal(facts.Evidence.Items)
		if err != nil || !strings.Contains(string(encodedItems), expected[section]) {
			t.Fatalf("%s differs from stored fixture facts: %s (%v)", section, encodedItems, err)
		}
		if len([]rune(facts.Summary)) > 1000 {
			t.Fatalf("%s summary exceeds Unicode character bound", section)
		}
		if len(response.Body.Bytes()) > 128<<10 {
			t.Fatalf("%s response exceeds complete response limit", section)
		}
	}
	// Exceed both the 50-record source limit and the byte-page limit. The
	// oversized second revision must not hide the readable records after it.
	expectedResults := map[string]string{fmt.Sprintf("%d:1", binding.ResultID): "Recorded research result"}
	oversizedID := fmt.Sprintf("%d:2", binding.ResultID)
	for revision := int64(2); revision <= 119; revision++ {
		comment := fmt.Sprintf("record-%03d %s", revision, strings.Repeat("界\"\\\n", 600))
		if revision == 2 {
			comment = strings.Repeat("oversized-source-marker", 14000)
		} else {
			expectedResults[fmt.Sprintf("%d:%d", binding.ResultID, revision)] = comment
		}
		snapshot, err := json.Marshal(map[string]any{"comment": comment})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: fmt.Sprintf("page-record-%d", revision), ExpectedRevision: revision - 1, Payload: []byte(`{"comment":"paging fixture"}`), Diagnostics: []byte(`{"valid":true}`), Snapshot: snapshot, Retired: []byte(`{}`), Accepted: true}); err != nil {
			t.Fatal(err)
		}
	}
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	pages, unavailable := 0, 0
	for {
		pages++
		if pages > 120 {
			t.Fatal("paging did not terminate within the finite fixture bound")
		}
		response := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 100 + pages, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": map[string]any{"task_ref": session.TaskRef, "section": "results", "limit": 50, "cursor": cursor}}}, tokens.Access)
		var out struct {
			Result struct {
				IsError    bool `json:"isError"`
				Structured struct {
					Summary  string `json:"summary"`
					Evidence struct {
						Items []struct {
							ID          string `json:"id"`
							Unavailable bool   `json:"unavailable"`
							Recorded    struct {
								Comment string `json:"comment"`
							} `json:"recorded"`
						} `json:"items"`
						HasMore bool   `json:"has_more"`
						Cursor  string `json:"next_cursor"`
					} `json:"evidence"`
				} `json:"structuredContent"`
			} `json:"result"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil || response.Code != http.StatusOK || out.Result.IsError || len(out.Error) > 0 {
			t.Fatalf("paging response: HTTP %d %s (%v)", response.Code, response.Body.String(), err)
		}
		if response.Body.Len() > 128<<10 || len([]rune(out.Result.Structured.Summary)) > 1000 {
			t.Fatal("paging exceeded full response or summary bounds")
		}
		items := out.Result.Structured.Evidence.Items
		if len(items) == 0 || len(items) > 50 {
			t.Fatalf("page failed to advance or exceeded record bound: %d items", len(items))
		}
		for _, item := range items {
			if item.ID == "" || seenIDs[item.ID] {
				t.Fatalf("missing or duplicate permitted ID %q", item.ID)
			}
			seenIDs[item.ID] = true
			if item.ID == oversizedID {
				if !item.Unavailable || item.Recorded.Comment != "" {
					t.Fatal("oversized source was released")
				}
				unavailable++
				continue
			}
			want, ok := expectedResults[item.ID]
			if !ok || item.Unavailable || item.Recorded.Comment != want {
				t.Fatalf("record %s differs from stored permitted content", item.ID)
			}
		}
		if !out.Result.Structured.Evidence.HasMore {
			if out.Result.Structured.Evidence.Cursor != "" {
				t.Fatal("terminal page retained continuation")
			}
			break
		}
		cursor = out.Result.Structured.Evidence.Cursor
		if cursor == "" || seenCursors[cursor] {
			t.Fatal("continuation failed to progress")
		}
		seenCursors[cursor] = true
	}
	if len(seenIDs) != 119 || unavailable != 1 || pages < 4 {
		t.Fatalf("incomplete byte-bounded traversal: records=%d unavailable=%d pages=%d", len(seenIDs), unavailable, pages)
	}
	if err = db.RevokeAllConnectorGrants(ctx, 42, now); err != nil {
		t.Fatal(err)
	}
	denied := jsonCall(map[string]any{"jsonrpc": "2.0", "id": 30, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": map[string]any{"task_ref": session.TaskRef, "section": "status"}}}, tokens.Access)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("revoked grant: %d %s", denied.Code, denied.Body.String())
	}
	after, err := db.GetSession(ctx, session.ID)
	if err != nil || after.State != session.State {
		t.Fatalf("discussion changed task state: %+v %v", after, err)
	}
}

func seedEvidence(t *testing.T, db *store.DB, session store.Session, now time.Time) store.ResultBinding {
	t.Helper()
	ctx := t.Context()
	state := planbrief.PlanState{SharedRevision: "shared-current", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: "technical", Candidate: "candidate-current", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Recorded task plan", Questions: []string{"Which option should proceed?"}}, Approvals: map[string]string{}, Effect: "applied"}}}
	revision, err := state.ReviewRevision("story-001", "technical")
	if err != nil {
		t.Fatal(err)
	}
	state.Stories[0].Wait = &planbrief.PlanWait{ID: "recorded-wait", Stage: "technical", Candidate: "candidate-current", Revision: revision, Kind: "review", IssueID: 17}
	state.Stories[0].Approvals["technical"] = revision
	if _, err = db.SavePlanWorkflowState(ctx, session.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "evidence", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return "/private/test/run" }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindResultAttempt(ctx, store.ResultAttempt{Task: session.TaskRef, Mode: "research", WorkContext: "evidence-context", Attempt: "evidence-attempt", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "recorded-result", ExpectedRevision: 0, Payload: []byte(`{"comment":"Recorded research result"}`), Diagnostics: []byte(`{"valid":true}`), Snapshot: []byte(`{"comment":"Recorded research result"}`), Retired: []byte(`{}`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "research", ProfileVersion: 1, Path: "workflow/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "research", StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "research", FactoryStep: true, StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: []byte(`{"result":{"comment":"Recorded research finding"}}`), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return binding
}

func revisionForApproval(t *testing.T, db *store.DB, task string) string {
	t.Helper()
	state, _, err := db.GetPlanWorkflowState(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	return state.Stories[0].Approvals["technical"]
}
