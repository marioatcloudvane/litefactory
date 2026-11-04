package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestBrainstormArgumentBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, raw string }{
		{"brainstorm_open", `{"repository":"github:acme/api"}`},
		{"brainstorm_get", `{"brainstorm":"b-1"}`},
		{"brainstorm_entries", `{"brainstorm":"b-1","kind":"exchange","cursor":"c","limit":50}`},
		{"brainstorm_scans", `{"brainstorm":"b-1","limit":1}`},
		{"brainstorm_list", `{"repository":"github:acme/api","cursor":"c"}`},
		{"brainstorm_add", `{"brainstorm":"b-1","kind":"idea","text":"Voice first","request_key":"k1","expected_revision":0}`},
		{"brainstorm_add", `{"brainstorm":"b-1","kind":"document_note","text":"` + strings.Repeat("x", 8000) + `","document_name":"spec.pdf","request_key":"k2","expected_revision":3}`},
		{"brainstorm_add", `{"brainstorm":"b-1","kind":"related_issue","text":"Same idea","issue_reference":"github:acme/api#12","request_key":"k3","expected_revision":1}`},
		{"brainstorm_close", `{"brainstorm":"b-1","request_key":"k4","expected_revision":2}`},
		{"brainstorm_ask", `{"brainstorm":"b-1","question":"Where is login handled?","request_key":"q1"}`},
		{"brainstorm_scan_get", `{"scan":"s-1","cursor":"c","limit":10}`},
		{"brainstorm_scan_cancel", `{"scan":"s-1"}`},
		{"issue_search", `{"repository":"github:acme/api","text":"dark mode","limit":50}`},
	} {
		if _, err := parseBrainstormArguments(tc.name, json.RawMessage(tc.raw)); err != nil {
			t.Errorf("%s rejected valid arguments %s: %v", tc.name, tc.raw, err)
		}
	}
	for _, tc := range []struct{ name, raw string }{
		{"brainstorm_open", `{}`},
		{"brainstorm_open", `{"repository":""}`},
		{"brainstorm_open", `{"repository":"a","repository":"b"}`},
		{"brainstorm_open", `{"repository":"a","brainstorm":"b"}`},
		{"brainstorm_open", `{"repository":null}`},
		{"brainstorm_open", `{"repository":"a"} {}`},
		{"brainstorm_get", `{"brainstorm":"b","owner":"42"}`},
		{"brainstorm_entries", `{"brainstorm":"b","kind":"secret"}`},
		{"brainstorm_entries", `{"brainstorm":"b","limit":0}`},
		{"brainstorm_scans", `{"brainstorm":"b","limit":51}`},
		{"brainstorm_scans", `{"brainstorm":"b","state":"waiting"}`},
		{"brainstorm_list", `{"repository":"a","cursor":"` + strings.Repeat("c", 4097) + `"}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","request_key":"k"}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","request_key":"k","expected_revision":-1}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"exchange","text":"t","request_key":"k","expected_revision":0}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"document_note","text":"t","request_key":"k","expected_revision":0}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","document_name":"d","request_key":"k","expected_revision":0}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"` + strings.Repeat("x", brainstorm.MaxEntryBytes+1) + `","request_key":"k","expected_revision":0}`},
		{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","request_key":"` + strings.Repeat("k", 129) + `","expected_revision":0}`},
		{"brainstorm_close", `{"brainstorm":"b","request_key":"k"}`},
		{"brainstorm_ask", `{"brainstorm":"b","question":"q"}`},
		{"brainstorm_ask", `{"brainstorm":"b","question":"` + strings.Repeat("q", brainstorm.MaxQuestionBytes+1) + `","request_key":"k"}`},
		{"brainstorm_ask", `{"brainstorm":"b","question":"q","request_key":"k","confirm":true}`},
		{"brainstorm_scan_cancel", `{"scan":"s","cursor":"c"}`},
		{"issue_search", `{"repository":"a"}`},
		{"issue_search", `{"repository":"a","text":"` + strings.Repeat("界", brainstorm.MaxIssueSearchText+1) + `"}`},
		{"issue_search", `{"repository":"a","text":"t","limit":51}`},
		{"factory_overview", `{}`},
	} {
		if _, err := parseBrainstormArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted invalid arguments: %.200s", tc.name, tc.raw)
		}
	}
}

func TestBrainstormSchemasMatchArgumentFields(t *testing.T) {
	t.Parallel()
	for _, name := range brainstormToolNames {
		allowed, required := brainstormFields(name)
		schema := brainstormInputSchema(name)
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema properties are %T", name, schema["properties"])
		}
		requiredFields, ok := schema["required"].([]string)
		if !ok {
			t.Fatalf("%s schema required is %T", name, schema["required"])
		}
		if len(props) != len(allowed) || !slices.Equal(requiredFields, required) {
			t.Errorf("%s schema %v does not match fields %v %v", name, schema, allowed, required)
		}
		if brainstormDescription(name) == "" {
			t.Errorf("%s has no description", name)
		}
	}
}

func TestBrainstormConsentRefusesAllRepositories(t *testing.T) {
	t.Parallel()
	f := newConnectorFixture(t, nil)
	registerTestRepo(t, f.db, "acme", "api")
	j, page, csrf, cookie := consentPageFor(t, f, BrainstormScope)
	if !strings.Contains(page, `value="`+BrainstormScope+`"`) {
		t.Fatal("consent page does not offer the brainstorm scope")
	}
	for _, scopes := range [][]string{{BrainstormScope}, {DiscussionScope, BrainstormScope}} {
		response := postConsentResponse(t, j, csrf, cookie, url.Values{"scope": scopes, "repositories": {"all"}, "duration": {"1"}})
		if response.Code != http.StatusBadRequest || strings.Contains(html.UnescapeString(response.Body.String()), "invalid_grant") {
			t.Fatalf("%v on all repositories: HTTP %d %s", scopes, response.Code, response.Body.String())
		}
		if reason, detail := lastConsentAudit(t, f); reason != "consent:invalid_request" || detail != "all_repositories_not_allowed" {
			t.Fatalf("%v audit %q %q", scopes, reason, detail)
		}
	}
	browserNoGrants(t, f)
	response := postConsentResponse(t, j, csrf, cookie, url.Values{"scope": {BrainstormScope}, "repository": {"github:acme/api"}, "duration": {"1"}})
	if response.Code != http.StatusFound {
		t.Fatalf("explicit repository refused: HTTP %d %s", response.Code, response.Body.String())
	}
}

type brainstormToolReply struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		IsError    bool `json:"isError"`
		Content    []TextContent
		Structured struct {
			Summary  string          `json:"summary"`
			Evidence json.RawMessage `json:"evidence"`
		} `json:"structuredContent"`
	} `json:"result"`
}

type brainstormFixture struct {
	f       *connectorFixture
	handler http.Handler
	repo    store.ConnectorGrantTarget
	other   store.Repo
	source  *atomic.Int64
}

func newBrainstormFixture(t *testing.T) brainstormFixture {
	t.Helper()
	f := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, f.db)
	other := registerTestRepo(t, f.db, "acme", "web")
	server, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	service, err := brainstorm.New(brainstorm.Options{DB: f.db, Now: func() time.Time { return f.now }, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
	if err != nil {
		t.Fatal(err)
	}
	server.SetBrainstorm(service)
	return brainstormFixture{f: f, handler: server.Handler(), repo: target, other: other, source: &atomic.Int64{}}
}

func (b brainstormFixture) token(t *testing.T, scope string) string {
	t.Helper()
	client := registerConnectorClient(t, b.handler, "brainstorm client", "none")
	token, _ := issueDiscussionTokens(t, b.f, client, b.repo, scope)
	return token
}

// rpc sends one MCP request from its own source address, so a long journey
// stays under the per-source request rate.
func (b brainstormFixture) rpc(t *testing.T, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, b.f.options.Config.ResourceURL, bytes.NewReader(raw)).WithContext(t.Context())
	n := b.source.Add(1)
	req.RemoteAddr = fmt.Sprintf("127.0.%d.%d:12345", n/250, n%250+1)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	if body["method"] != "initialize" {
		req.Header.Set("MCP-Protocol-Version", protocolLatest)
	}
	recorder := httptest.NewRecorder()
	b.handler.ServeHTTP(recorder, req)
	return recorder
}

func (b brainstormFixture) list(t *testing.T, token string) []string {
	t.Helper()
	response := b.rpc(t, token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	connectorDecode(t, response, &out)
	names := []string{}
	for _, tool := range out.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func (b brainstormFixture) call(t *testing.T, token, name string, args map[string]any) (*httptest.ResponseRecorder, brainstormToolReply) {
	t.Helper()
	response := b.rpc(t, token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	var out brainstormToolReply
	if response.Code == http.StatusOK {
		connectorDecode(t, response, &out)
	}
	return response, out
}

func TestBrainstormToolsHiddenAndRefusedForOldGrant(t *testing.T) {
	t.Parallel()
	b := newBrainstormFixture(t)
	token := b.token(t, DiscussionScope+" "+AdvisoryScope)
	for _, name := range b.list(t, token) {
		if isBrainstormTool(name) {
			t.Fatalf("old grant lists %s", name)
		}
	}
	for _, name := range brainstormToolNames {
		response, _ := b.call(t, token, name, map[string]any{"repository": b.repo.RepositoryRef})
		if response.Code != http.StatusForbidden || !strings.Contains(response.Header().Get("WWW-Authenticate"), `scope="`+BrainstormScope+`"`) {
			t.Fatalf("%s for old grant: HTTP %d %q", name, response.Code, response.Header().Get("WWW-Authenticate"))
		}
	}
}

func TestBrainstormToolsCallThroughToService(t *testing.T) {
	t.Parallel()
	b := newBrainstormFixture(t)
	token := b.token(t, BrainstormScope)
	listed := b.list(t, token)
	for _, name := range brainstormToolNames {
		if !slices.Contains(listed, name) {
			t.Fatalf("brainstorm grant does not list %s: %v", name, listed)
		}
	}
	if slices.Contains(listed, "task_evidence_read") || slices.Contains(listed, "ask_task_agent") {
		t.Fatalf("brainstorm grant lists other scopes' tools: %v", listed)
	}
	initialized := b.rpc(t, token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": protocolLatest}})
	if initialized.Code != http.StatusOK || strings.Contains(initialized.Body.String(), `"error"`) {
		t.Fatalf("initialize with brainstorm guidance: HTTP %d %s", initialized.Code, initialized.Body.String())
	}

	if _, out := b.call(t, token, "brainstorm_open", map[string]any{"repository": b.other.Forge + ":" + b.other.Owner + "/" + b.other.Name}); !out.Result.IsError {
		t.Fatalf("uncovered repository opened: %+v", out)
	}
	if response, out := b.call(t, token, "brainstorm_open", map[string]any{"repository": b.repo.RepositoryRef, "limit": 1}); response.Code != http.StatusOK || out.Error == nil {
		t.Fatalf("unknown argument accepted: HTTP %d %+v", response.Code, out)
	}
	_, opened := b.call(t, token, "brainstorm_open", map[string]any{"repository": b.repo.RepositoryRef})
	if opened.Error != nil || opened.Result.IsError {
		t.Fatalf("open: %+v", opened)
	}
	var view brainstorm.View
	if err := json.Unmarshal(opened.Result.Structured.Evidence, &view); err != nil || view.Brainstorm.ID == "" || view.Brainstorm.RepositoryRef != b.repo.RepositoryRef {
		t.Fatalf("open view %s %v", opened.Result.Structured.Evidence, err)
	}
	_, added := b.call(t, token, "brainstorm_add", map[string]any{"brainstorm": view.Brainstorm.ID, "kind": "document_note", "text": "Login must stay under one second.", "document_name": "goals.md", "request_key": "note-1", "expected_revision": view.Brainstorm.Revision})
	if added.Error != nil || added.Result.IsError {
		t.Fatalf("add: %+v", added)
	}
	_, stale := b.call(t, token, "brainstorm_add", map[string]any{"brainstorm": view.Brainstorm.ID, "kind": "idea", "text": "Second", "request_key": "idea-1", "expected_revision": view.Brainstorm.Revision})
	if !stale.Result.IsError || !strings.Contains(stale.Result.Content[0].Text, brainstorm.ErrStaleRevision.Error()) {
		t.Fatalf("stale revision: %+v", stale)
	}
	_, entries := b.call(t, token, "brainstorm_entries", map[string]any{"brainstorm": view.Brainstorm.ID, "kind": "document_note"})
	var page brainstorm.EntryPage
	if err := json.Unmarshal(entries.Result.Structured.Evidence, &page); err != nil || len(page.Entries) != 1 || page.Entries[0].DocumentName != "goals.md" {
		t.Fatalf("entries %s %v", entries.Result.Structured.Evidence, err)
	}
	_, scans := b.call(t, token, "brainstorm_scans", map[string]any{"brainstorm": view.Brainstorm.ID})
	if scans.Error != nil || scans.Result.IsError || !strings.Contains(string(scans.Result.Structured.Evidence), `"scans":[]`) {
		t.Fatalf("scans: %+v", scans)
	}
	_, missing := b.call(t, token, "brainstorm_get", map[string]any{"brainstorm": "brainstorm-missing"})
	if !missing.Result.IsError || !strings.Contains(missing.Result.Content[0].Text, brainstorm.ErrNotFound.Error()) {
		t.Fatalf("missing brainstorm: %+v", missing)
	}
	_, listedBrainstorms := b.call(t, token, "brainstorm_list", map[string]any{"repository": b.repo.RepositoryRef})
	var list brainstorm.ListPage
	if err := json.Unmarshal(listedBrainstorms.Result.Structured.Evidence, &list); err != nil || len(list.Brainstorms) != 1 {
		t.Fatalf("list %s %v", listedBrainstorms.Result.Structured.Evidence, err)
	}

	// A grant for another repository cannot reach this brainstorm by reference.
	client := registerConnectorClient(t, b.handler, "other repository client", "none")
	foreign, _ := issueDiscussionTokens(t, b.f, client, store.ConnectorGrantTarget{RepositoryRef: "github:acme/web"}, BrainstormScope)
	for _, name := range []string{"brainstorm_get", "brainstorm_entries", "brainstorm_scans"} {
		if _, out := b.call(t, foreign, name, map[string]any{"brainstorm": view.Brainstorm.ID}); !out.Result.IsError || out.Result.Structured.Evidence != nil {
			t.Fatalf("%s crossed the repository boundary: %+v", name, out)
		}
	}
	if _, out := b.call(t, foreign, "brainstorm_add", map[string]any{"brainstorm": view.Brainstorm.ID, "kind": "idea", "text": "Cross", "request_key": "cross-1", "expected_revision": 1}); !out.Result.IsError {
		t.Fatalf("add crossed the repository boundary: %+v", out)
	}
}
