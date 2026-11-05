package connector

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/connectorstate"
)

// The main path: a connected client initializes, lists prompts and opens each one.
func TestPromptsMainPath(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }))
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	call := func(id int, method string, params any) (int, map[string]any) {
		body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
		if params != nil {
			body["params"] = params
		}
		r := connectorJSON(t, server, fixture.options.Config.ResourceURL, body, connectorBearer(tokens.AccessToken))
		var out map[string]any
		connectorDecode(t, r, &out)
		return r.Code, out
	}
	_, init := call(1, "initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	res, _ := init["result"].(map[string]any)
	capabilities, _ := res["capabilities"].(map[string]any)
	if _, ok := capabilities["prompts"]; !ok || res["instructions"] == "" {
		t.Fatalf("initialize: %v", init)
	}
	_, listed := call(2, "prompts/list", nil)
	names := map[string]bool{}
	listedResult, _ := listed["result"].(map[string]any)
	prompts, _ := listedResult["prompts"].([]any)
	for _, p := range prompts {
		prompt, _ := p.(map[string]any)
		name, _ := prompt["name"].(string)
		names[name] = true
	}
	if !names["brainstorm_feature"] || !names["whats_waiting"] || names["review_task"] {
		t.Fatalf("prompts must follow the grant's tools: %v", names)
	}
	for _, name := range []string{"brainstorm_feature", "whats_waiting"} {
		code, got := call(3, "prompts/get", map[string]any{"name": name})
		msgs, _ := got["result"].(map[string]any)["messages"].([]any)
		if code != http.StatusOK || len(msgs) != 1 {
			t.Fatalf("%s: %v", name, got)
		}
		msg, _ := msgs[0].(map[string]any)
		content, _ := msg["content"].(map[string]any)
		text, _ := content["text"].(string)
		if name == "brainstorm_feature" {
			guide, err := readBrainstormGuide(guidanceFiles)
			if err != nil || !strings.HasPrefix(text, guide) || len(text) <= len(guide) {
				t.Fatalf("brainstorm prompt must start with the shared guide")
			}
		}
	}
	_, refused := call(4, "prompts/get", map[string]any{"name": "review_task", "arguments": map[string]any{"task": "7"}})
	refusal, _ := refused["error"].(map[string]any)
	if code, _ := refusal["code"].(float64); code != -32602 {
		t.Fatalf("unlisted prompt must be refused: %v", refused)
	}
}

// missingScopeServer builds a connector with or without the brainstorm
// service and returns it with a fixture that can issue tokens and send RPCs.
func missingScopeServer(t *testing.T, withBrainstorm bool) (*Server, brainstormFixture) {
	t.Helper()
	f := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, f.db)
	server, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	if withBrainstorm {
		service, err := brainstorm.New(brainstorm.Options{DB: f.db, Now: func() time.Time { return f.now }, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
		if err != nil {
			t.Fatal(err)
		}
		server.SetBrainstorm(service)
	}
	return server, brainstormFixture{f: f, handler: server.Handler(), repo: target, source: &atomic.Int64{}}
}

func initializeInstructions(t *testing.T, b brainstormFixture, token string) string {
	t.Helper()
	response := b.rpc(t, token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": protocolLatest}})
	var out struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	connectorDecode(t, response, &out)
	if response.Code != http.StatusOK || out.Result.Instructions == "" {
		t.Fatalf("initialize: HTTP %d %s", response.Code, response.Body.String())
	}
	return out.Result.Instructions
}

// missingNotice returns the missing-scope notice part of the instructions, or "".
func missingNotice(instructions string) string {
	const heading = "# Consent this connection lacks"
	if i := strings.Index(instructions, heading); i >= 0 {
		return instructions[i:]
	}
	return ""
}

func grantScope(t *testing.T, b brainstormFixture, token string) string {
	t.Helper()
	grant, err := b.f.db.CheckConnectorAccess(t.Context(), digest(token), 42, b.f.options.Config.ResourceURL, "", b.f.now)
	if err != nil {
		t.Fatal(err)
	}
	return grant.Scope
}

// An old grant without repository:brainstorm is told the scope exists and how
// to get it; nothing is granted and the tools stay hidden.
func TestOldGrantToldAboutMissingBrainstormScope(t *testing.T) {
	t.Parallel()
	_, b := missingScopeServer(t, true)
	token := b.token(t, DiscussionScope+" "+AdvisoryScope)
	before := grantScope(t, b, token)
	first := initializeInstructions(t, b, token)
	notice := missingNotice(first)
	if !strings.Contains(notice, BrainstormScope) || !strings.Contains(strings.ToLower(notice), "reconnect") || !strings.Contains(strings.ToLower(notice), "tick") {
		t.Fatalf("instructions do not name the missing scope and a reconnect: %q", first)
	}
	if second := initializeInstructions(t, b, token); second != first {
		t.Fatal("repeated initialize changed the instructions")
	}
	for _, name := range b.list(t, token) {
		if isBrainstormTool(name) {
			t.Fatalf("old grant lists %s", name)
		}
	}
	if after := grantScope(t, b, token); after != before || connectorstate.ConnectorScopeHas(after, BrainstormScope) {
		t.Fatalf("grant scope changed: %q -> %q", before, after)
	}
}

func TestGrantWithBrainstormNotToldItIsMissing(t *testing.T) {
	t.Parallel()
	_, b := missingScopeServer(t, true)
	token := b.token(t, BrainstormScope)
	if notice := missingNotice(initializeInstructions(t, b, token)); strings.Contains(notice, BrainstormScope) {
		t.Fatalf("brainstorm grant told repository:brainstorm is missing: %q", notice)
	}
	if !slices.Contains(b.list(t, token), "brainstorm_open") {
		t.Fatal("brainstorm grant lost its tools")
	}
}

func TestUnservedScopeNeverAdvertised(t *testing.T) {
	t.Parallel()
	_, b := missingScopeServer(t, false)
	token := b.token(t, DiscussionScope)
	if ins := initializeInstructions(t, b, token); strings.Contains(ins, BrainstormScope) {
		t.Fatalf("server without brainstorms advertises repository:brainstorm: %q", ins)
	}
}

func TestFullGrantGetsNoNotice(t *testing.T) {
	t.Parallel()
	server, _ := missingScopeServer(t, true)
	grant := connectorstate.ConnectorGrant{Scope: strings.Join(scopeNames(), " ")}
	if missing := server.missingScopes(grant); len(missing) != 0 {
		t.Fatalf("full grant missing %v", missing)
	}
	ins, err := composeInstructions(server.guidance, server.availableTools(grant), server.missingScopes(grant))
	if err != nil || missingNotice(ins) != "" {
		t.Fatalf("full grant instructions: %v %q", err, missingNotice(ins))
	}
}

func TestMissingScopeGuidanceFailsClosed(t *testing.T) {
	t.Parallel()
	server, b := missingScopeServer(t, true)
	files := fstest.MapFS{}
	if err := fs.WalkDir(guidanceFiles, "guidance", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "guidance/overview/missing_scopes.md" {
			return err
		}
		data, err := fs.ReadFile(guidanceFiles, path)
		files[path] = &fstest.MapFile{Data: data}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	server.guidance = files
	token := b.token(t, DiscussionScope+" "+AdvisoryScope)
	before := grantScope(t, b, token)
	response := b.rpc(t, token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": protocolLatest}})
	var out struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	connectorDecode(t, response, &out)
	if out.Error == nil || out.Error.Code != -32603 || out.Error.Message != "Tool guidance unavailable" || len(out.Result) != 0 {
		t.Fatalf("initialize without the notice text: %s", response.Body.String())
	}
	if after := grantScope(t, b, token); after != before {
		t.Fatalf("grant scope changed: %q -> %q", before, after)
	}
}
