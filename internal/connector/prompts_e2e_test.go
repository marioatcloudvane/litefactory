package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
	res := promptsAs[map[string]any](t, init["result"])
	if _, ok := promptsAs[map[string]any](t, res["capabilities"])["prompts"]; !ok || res["instructions"] == "" {
		t.Fatalf("initialize: %v", init)
	}
	_, listed := call(2, "prompts/list", nil)
	names := map[string]bool{}
	for _, p := range promptsAs[[]any](t, promptsAs[map[string]any](t, listed["result"])["prompts"]) {
		names[promptsAs[string](t, promptsAs[map[string]any](t, p)["name"])] = true
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
		text := promptsAs[string](t, promptsAs[map[string]any](t, promptsAs[map[string]any](t, msgs[0])["content"])["text"])
		if name == "brainstorm_feature" {
			guide, err := readBrainstormGuide(guidanceFiles)
			if err != nil || !strings.HasPrefix(text, guide) || len(text) <= len(guide) {
				t.Fatalf("brainstorm prompt must start with the shared guide")
			}
		}
	}
	_, refused := call(4, "prompts/get", map[string]any{"name": "review_task", "arguments": map[string]any{"task": "7"}})
	if promptsAs[float64](t, promptsAs[map[string]any](t, refused["error"])["code"]) != -32602 {
		t.Fatalf("unlisted prompt must be refused: %v", refused)
	}
}

// promptsAs asserts that v has type T and fails the test otherwise.
func promptsAs[T any](t *testing.T, v any) T {
	t.Helper()
	out, ok := v.(T)
	if !ok {
		t.Fatalf("unexpected value %#v (%T)", v, v)
	}
	return out
}
