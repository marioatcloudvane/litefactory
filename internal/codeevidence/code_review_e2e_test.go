package codeevidence_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/codeevidence"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type reviewIdentity struct{}

func (reviewIdentity) AuthorizationURL(state, challenge, redirect string) string {
	return "https://github.example/authorize?" + url.Values{"state": {state}, "code_challenge": {challenge}}.Encode()
}
func (reviewIdentity) Verify(context.Context, string, string, string) (int64, error) { return 42, nil }

type reviewControl struct {
	db  *store.DB
	now func() time.Time
}

func (reviewControl) Ready(context.Context) error { return nil }
func (c reviewControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.db.ListConnectorGrants(ctx, 42, c.now())
}
func (c reviewControl) Revoke(ctx context.Context, id string) error {
	return c.db.RevokeConnectorGrant(ctx, 42, id, c.now())
}
func (c reviewControl) RevokeAll(ctx context.Context) error {
	return c.db.RevokeAllConnectorGrants(ctx, 42, c.now())
}

// TestOwnerCodeStandardsConsentToEvidence is the story's main path through the
// public browser/OAuth/MCP interfaces and actual local Git/published-book reads.
// Identity verification is replaced; no hosted client or production service runs.
func TestOwnerCodeStandardsConsentToEvidence(t *testing.T) {
	repo := gittest.New(t)
	repo.Git("remote", "set-url", "origin", "https://github.com/acme/code.git")
	repo.WriteFile("limits.go", "package review\n\nconst Limit = 1\n")
	base := repo.Commit("Base review limit")
	repo.Git("update-ref", "refs/remotes/origin/main", base)
	repo.Git("checkout", "-b", "review-task")
	repo.WriteFile("limits.go", "package review\n\nconst Limit = 3\n")
	task := repo.Commit("Review: increase positive limit")
	repo.WriteFile("limits.go", "uncommitted secret-marker")
	before := repo.Git("status", "--porcelain=v1")
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "review.db"))
	mustReview(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mustReview(t, db.Migrate(t.Context()))
	_, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	mustReview(t, err)
	target := seedReviewTarget(t, db, repo.Dir)
	publication := seedReviewBook(t, db)
	binary, err := git.Resolve(t.Context())
	mustReview(t, err)
	now := time.Now().UTC().Truncate(time.Millisecond)
	clock := func() time.Time { return now }
	local, err := connector.NewLocalControl(t.Context(), db, reviewControl{db, clock})
	mustReview(t, err)
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "review-app"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	cfg.AllowedOrigins = []string{"https://client.example"}
	server, err := connector.New(t.Context(), connector.Options{Config: cfg, DB: db, Logger: slog.New(slog.DiscardHandler), Now: clock, LocalControl: local, GitHub: reviewIdentity{}, CookieKey: bytes.Repeat([]byte{7}, 32), CodeEvidence: codeevidence.New(db, binary, redact.New(redact.Options{Environ: []string{}}), clock)})
	mustReview(t, err)
	handler := server.Handler()
	request := func(method, path, ctype, body string, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		now = now.Add(time.Second)
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(t.Context())
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", ctype)
		for k, values := range headers {
			for _, value := range values {
				r.Header.Add(k, value)
			}
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	jsonRequest := func(path string, p any, headers http.Header) *httptest.ResponseRecorder {
		t.Helper()
		b, e := json.Marshal(p)
		mustReview(t, e)
		if headers == nil {
			headers = http.Header{}
		}
		headers.Set("Accept", "application/json, text/event-stream")
		return request(http.MethodPost, path, "application/json", string(b), headers)
	}
	form := func(path string, v url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		return request(http.MethodPost, path, "application/x-www-form-urlencoded", v.Encode(), http.Header{"Origin": {"https://client.example"}}, cookies...)
	}
	registered := jsonRequest(cfg.IssuerURL+"/oauth/register", map[string]any{"client_name": "Code review fixture", "redirect_uris": []string{"https://client.example/callback"}, "token_endpoint_auth_method": "none"}, nil)
	var client struct {
		ID string `json:"client_id"`
	}
	decodeReview(t, registered, &client)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register: %s", registered.Body.String())
	}
	verifier := strings.Repeat("v", 43)
	hash := sha256.Sum256([]byte(verifier))
	scope := connector.DiscussionScope + " " + connector.CodeReviewScope
	q := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "resource": {cfg.ResourceURL}, "scope": {scope}, "state": {"review-client-state"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
	start := request(http.MethodGet, cfg.IssuerURL+"/oauth/authorize?"+q.Encode(), "", "", nil)
	if start.Code != http.StatusFound {
		t.Fatalf("authorize: %s", start.Body.String())
	}
	upstream, e := url.Parse(start.Header().Get("Location"))
	mustReview(t, e)
	callback := request(http.MethodGet, cfg.IssuerURL+"/oauth/github/callback?"+url.Values{"code": {"fixture-code"}, "state": {upstream.Query().Get("state")}}.Encode(), "", "", nil, start.Result().Cookies()...)
	if callback.Code != http.StatusOK {
		t.Fatalf("callback: %s", callback.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token"[^>]*value="([^"]+)"`).FindStringSubmatch(callback.Body.String())
	if len(csrf) != 2 {
		t.Fatal("missing consent CSRF")
	}
	consent := form(cfg.IssuerURL+"/oauth/consent", url.Values{"csrf_token": {csrf[1]}, "decision": {"allow"}, "scope": strings.Fields(scope), "duration": {"1"}, "repositories": {"all"}}, callback.Result().Cookies()...)
	if consent.Code != http.StatusFound {
		t.Fatalf("consent: %d %s", consent.Code, consent.Body.String())
	}
	redirect, e := url.Parse(consent.Header().Get("Location"))
	mustReview(t, e)
	exchange := form(cfg.IssuerURL+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "resource": {cfg.ResourceURL}, "code": {redirect.Query().Get("code")}, "code_verifier": {verifier}})
	var tokens struct {
		Access string `json:"access_token"`
	}
	decodeReview(t, exchange, &tokens)
	if exchange.Code != http.StatusOK || tokens.Access == "" {
		t.Fatalf("exchange: %s", exchange.Body.String())
	}
	headers := http.Header{"Authorization": {"Bearer " + tokens.Access}, "MCP-Protocol-Version": {"2025-11-25"}}
	rpc := func(method string, params any) *httptest.ResponseRecorder {
		t.Helper()
		return jsonRequest(cfg.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, headers)
	}
	initialized := rpc("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "review-fixture", "version": "1"}})
	if initialized.Code != http.StatusOK {
		t.Fatalf("initialize: %s", initialized.Body.String())
	}
	listed := rpc("tools/list", nil)
	var tools struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	decodeReview(t, listed, &tools)
	names := []string{}
	for _, tool := range tools.Result.Tools {
		names = append(names, tool.Name)
	}
	for _, name := range []string{"repo_read", "repo_grep", "repo_log", "repo_diff", "standards_read", "standards_role_counts"} {
		if !slices.Contains(names, name) {
			t.Fatalf("missing tool %s in %v", name, names)
		}
	}
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		response := rpc("tools/call", map[string]any{"name": name, "arguments": args})
		if response.Code != http.StatusOK || response.Body.Len() > 128<<10 {
			t.Fatalf("tool %s: HTTP %d %s", name, response.Code, response.Body.String())
		}
		var out struct {
			Result struct {
				Error      bool           `json:"isError"`
				Structured map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		decodeReview(t, response, &out)
		if out.Result.Error {
			t.Fatalf("tool %s refused: %s", name, response.Body.String())
		}
		summary, _ := out.Result.Structured["summary"].(string)
		if utf8.RuneCountInString(summary) > 1000 {
			t.Fatal("summary exceeds Unicode budget")
		}
		if strings.Contains(response.Body.String(), "uncommitted secret-marker") {
			t.Fatal("working tree disclosed")
		}
		return out.Result.Structured
	}
	read := call("repo_read", map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "limits.go", "start_line": 3, "lines": 1})
	assertReviewJSON(t, read, task, "refs/heads/review-task", "Limit = 3")
	firstLine := reviewItem(t, reviewEvidence(t, read))
	if firstLine["line"] != float64(3) || firstLine["url"] != "https://github.com/acme/code/blob/"+task+"/limits.go#L3" {
		t.Fatalf("missing exact line provenance: %#v", firstLine)
	}
	codeView, _ := reviewEvidence(t, read)["view_token"].(string)
	if codeView == "" {
		t.Fatal("missing repository view")
	}
	repo.WriteFile("limits.go", "package review\n\nconst Limit = 9\n")
	laterCommit := repo.Commit("Later local limit change")
	repo.WriteFile("limits.go", "uncommitted secret-marker")
	pinnedRead := call("repo_read", map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "limits.go", "view_token": codeView})
	assertReviewJSON(t, pinnedRead, task, "Limit = 3")
	freshRead := call("repo_read", map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "limits.go"})
	assertReviewJSON(t, freshRead, laterCommit, "Limit = 9")
	baseRead := call("repo_read", map[string]any{"task_ref": target.TaskRef, "branch": "base", "path": "limits.go"})
	assertReviewJSON(t, baseRead, base, "Limit = 1")
	call("repo_grep", map[string]any{"task_ref": target.TaskRef, "branch": "task", "pattern": "Limit", "path": "limits.go"})
	call("repo_log", map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "limits.go"})
	call("repo_diff", map[string]any{"task_ref": target.TaskRef, "mode": "base_to_task", "path": "limits.go"})
	book := call("standards_read", map[string]any{"task_ref": target.TaskRef, "limit": 1})
	assertReviewJSON(t, book, "review-limit", publication.Digest)
	firstBook := reviewEvidence(t, book)
	cursor, _ := firstBook["next_cursor"].(string)
	view, _ := firstBook["view_token"].(string)
	if cursor == "" || view == "" {
		t.Fatal("missing book continuation or shared view")
	}
	laterBook := call("standards_read", map[string]any{"cursor": cursor})
	assertReviewJSON(t, laterBook, "test-evidence", publication.Digest)
	if reviewEvidence(t, laterBook)["has_more"] != false {
		t.Fatal("two-rule book did not finish")
	}
	counts := call("standards_role_counts", map[string]any{"task_ref": target.TaskRef, "categories": []string{"review"}, "view_token": view})
	assertReviewJSON(t, counts, "story-worker", publication.Digest)
	// The same fixture supports the guide's selected-category/whole-book example.
	countEvidence := reviewEvidence(t, counts)
	countItem := reviewItem(t, countEvidence)
	selected := reviewMap(t, countItem["selected_categories"])
	whole := reviewMap(t, countItem["whole_book"])
	if selected["story-worker"] != float64(1) || whole["story-worker"] != float64(2) || selected["validate-architecture"] != float64(1) || whole["validate-architecture"] != float64(1) {
		t.Fatalf("wrong labeled counts: %#v", countItem)
	}
	empty := call("standards_role_counts", map[string]any{"task_ref": target.TaskRef, "categories": []string{"empty"}, "view_token": view})
	emptyItem := reviewItem(t, reviewEvidence(t, empty))
	if reviewMap(t, emptyItem["selected_categories"])["story-worker"] != float64(0) || reviewMap(t, emptyItem["whole_book"])["story-worker"] != float64(2) {
		t.Fatalf("empty category changed count basis: %#v", emptyItem)
	}
	refusal := rpc("tools/call", map[string]any{"name": "repo_read", "arguments": map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "../private"}})
	if refusal.Code != http.StatusOK || !strings.Contains(refusal.Body.String(), `"error"`) || strings.Contains(refusal.Body.String(), "Limit = 3") {
		t.Fatal("unsafe path did not refuse safely")
	}
	t.Logf("illustrative fixture evidence: task=%s base=%s book=%d digest=%s", task, base, publication.ID, publication.Digest)
	grants, err := db.ListConnectorGrants(t.Context(), 42, now)
	mustReview(t, err)
	if len(grants) != 1 {
		t.Fatalf("grants=%d", len(grants))
	}
	mustReview(t, db.RevokeConnectorGrant(t.Context(), 42, grants[0].ID, now))
	denied := rpc("tools/call", map[string]any{"name": "repo_read", "arguments": map[string]any{"task_ref": target.TaskRef, "branch": "task", "path": "limits.go"}})
	if denied.Code != http.StatusUnauthorized {
		t.Fatal("revocation released fresh evidence")
	}
	if after := repo.Git("status", "--porcelain=v1"); after != before {
		t.Fatal("discussion mutated repository")
	}
}
func mustReview(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func decodeReview(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %d: %v %s", w.Code, err, w.Body.String())
	}
}
func assertReviewJSON(t *testing.T, value any, parts ...string) {
	t.Helper()
	b, err := json.Marshal(value)
	mustReview(t, err)
	for _, part := range parts {
		if !strings.Contains(string(b), part) {
			t.Fatalf("missing %q in %s", part, b)
		}
	}
}

func seedReviewTarget(t *testing.T, db *store.DB, clone string) store.ConnectorCodeTarget {
	t.Helper()
	ctx := t.Context()
	binary, err := git.Resolve(ctx)
	mustReview(t, err)
	source, err := binary.Open(ctx, clone)
	mustReview(t, err)
	origin, err := source.RemoteURL(ctx, "origin")
	mustReview(t, err)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: clone, Forge: store.ForgeGitHub, Owner: "acme", Name: "tickets", RemoteURL: "https://github.com/acme/tickets.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	mustReview(t, err)
	v := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "acme/code", CredentialSource: "env", CredentialEnv: "TEST_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: clone, RemoteURL: origin, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: repo.WorktreeRoot}
	code, err := db.SetConnection(ctx, v, 0)
	mustReview(t, err)
	v.Role = store.TicketConnection
	v.Destination = "acme/tickets"
	v.RemoteURL = repo.RemoteURL
	tickets, err := db.SetConnection(ctx, v, 0)
	mustReview(t, err)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "7"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: "https://api.github.com", Container: "acme/tickets", Key: "7"}}
	_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	mustReview(t, err)
	session, err := db.AdmitSession(ctx, repo, ref, code.Revision, tickets.Revision, 42, "bot", "github:acme/tickets#7")
	mustReview(t, err)
	session, err = db.FreezeSessionSlug(ctx, session.ID, "review", "review-task")
	mustReview(t, err)
	target, err := db.ResolveConnectorCodeTarget(ctx, session.TaskRef)
	mustReview(t, err)
	return target
}
func seedReviewBook(t *testing.T, db *store.DB) store.StandardsRevision {
	t.Helper()
	s := standards.Service{Store: db, OwnerID: 42}
	operations := []standards.Operation{}
	for _, id := range []string{"review", "testing", "empty"} {
		operations = append(operations, standards.Operation{Kind: "category.add", Category: &standards.Category{ID: id, Name: id}})
	}
	for _, rule := range []standards.Standard{{ID: "review-limit", Rule: "MUST use a positive limit", Description: "The configured review limit is positive.", Evidence: "Committed limit declaration.", Scope: "Review code.", Check: "manual", CategoryID: "review", AppliesTo: []string{"story-worker", "validate-architecture"}}, {ID: "test-evidence", Rule: "MUST record test evidence", Description: "Record the checks actually run.", Evidence: "Worker result.", Scope: "Story work.", Check: "manual", CategoryID: "testing", AppliesTo: []string{"story-worker"}}} {
		rule := rule
		operations = append(operations, standards.Operation{Kind: "standard.add", Standard: &rule})
	}
	proposal, err := s.Create(t.Context(), "review-e2e", 0, operations)
	mustReview(t, err)
	_, err = s.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustReview(t, err)
	revision, err := s.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest)
	mustReview(t, err)
	return revision
}

func reviewEvidence(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	evidence, ok := value["evidence"].(map[string]any)
	if !ok {
		t.Fatalf("missing evidence: %#v", value)
	}
	return evidence
}

func reviewMap(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %#v", value)
	}
	return out
}
func reviewItem(t *testing.T, evidence map[string]any) map[string]any {
	t.Helper()
	items, ok := evidence["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one item, got %#v", evidence["items"])
	}
	return reviewMap(t, items[0])
}
