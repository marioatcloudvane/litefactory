package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDiscussionAuthorizedCapabilities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scope    string
		overview, task bool
		want           []string
	}{
		{"overview", Scope, true, true, []string{"factory_overview"}},
		{"discussion", DiscussionScope, true, true, []string{"task_evidence_read"}},
		{"combined", Scope + " " + DiscussionScope, true, true, []string{"factory_overview", "task_evidence_read"}},
		{"missing overview", Scope, false, true, []string{}},
		{"missing discussion", DiscussionScope, true, false, []string{}},
		{"no providers", Scope + " " + DiscussionScope, false, false, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newConnectorFixture(t, nil)
			target := seedDiscussionTarget(t, fixture.db)
			overviewCalls, taskCalls := 0, 0
			if tc.overview {
				fixture.options.Overview = connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) {
					overviewCalls++
					return ToolResult{Content: []TextContent{{Type: "text", Text: "overview marker"}}}, nil
				})
			}
			if tc.task {
				fixture.options.TaskEvidence = fixtureEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
					taskCalls++
					return EvidencePage{Revision: "r1", Items: []map[string]any{{"id": "task marker"}}}, nil
				})
			}
			server, err := New(t.Context(), fixture.options)
			if err != nil {
				t.Fatal(err)
			}
			server.guidance = fstest.MapFS{"guidance/factory_overview.md": {Data: []byte("factory_overview")}, "guidance/task_evidence_read.md": {Data: []byte("task_evidence_read")}}
			handler := server.Handler()
			client := registerConnectorClient(t, handler, "boundary", "none")
			token, _ := issueDiscussionTokens(t, fixture, client, target, tc.scope)
			call := func(method string, params any) *httptest.ResponseRecorder {
				t.Helper()
				fixture.now = fixture.now.Add(time.Second)
				return connectorJSON(t, handler, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, connectorBearer(token))
			}
			initialized := call("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "boundary", "version": "1"}})
			var init struct {
				Result struct {
					Instructions string `json:"instructions"`
				} `json:"result"`
			}
			connectorDecode(t, initialized, &init)
			if initialized.Code != http.StatusOK {
				t.Fatalf("init: %s", initialized.Body.String())
			}
			listed := call("tools/list", nil)
			var list struct {
				Result struct {
					Tools []struct {
						Name string `json:"name"`
					} `json:"tools"`
				} `json:"result"`
			}
			connectorDecode(t, listed, &list)
			names := []string{}
			for _, tool := range list.Result.Tools {
				names = append(names, tool.Name)
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("tools=%v want %v", names, tc.want)
			}
			for _, name := range []string{"factory_overview", "task_evidence_read"} {
				authorized := slices.Contains(tc.want, name)
				if strings.Contains(init.Result.Instructions, name) != authorized {
					t.Fatalf("instructions name unauthorized capability or omit available capability %q: %s", name, init.Result.Instructions)
				}
				args := map[string]any{}
				if name == "task_evidence_read" {
					args = map[string]any{"task_ref": target.TaskRef, "section": "status"}
				}
				before := overviewCalls + taskCalls
				response := call("tools/call", map[string]any{"name": name, "arguments": args})
				if authorized && overviewCalls+taskCalls != before+1 {
					t.Fatalf("authorized %s not dispatched: %s", name, response.Body.String())
				}
				if !authorized && overviewCalls+taskCalls != before {
					t.Fatalf("unauthorized %s reached provider: %s", name, response.Body.String())
				}
			}
		})
	}
}

func TestDiscussionInitializationDoesNotCacheGrantAuthority(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }))
	target := seedDiscussionTarget(t, fixture.db)
	fixture.options.TaskEvidence = fixtureEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
		return EvidencePage{}, nil
	})
	server, err := New(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	server.guidance = fstest.MapFS{"guidance/factory_overview.md": {Data: []byte("factory_overview")}, "guidance/task_evidence_read.md": {Data: []byte("task_evidence_read")}}
	handler := server.Handler()
	client := registerConnectorClient(t, handler, "client", "none")
	overview, _ := issueDiscussionTokens(t, fixture, client, target, Scope)
	discussion, _ := issueDiscussionTokens(t, fixture, client, target, DiscussionScope)
	combined, _ := issueDiscussionTokens(t, fixture, client, target, Scope+" "+DiscussionScope)
	for _, tc := range []struct {
		token          string
		overview, task bool
	}{{overview, true, false}, {discussion, false, true}, {combined, true, true}, {overview, true, false}, {discussion, false, true}} {
		fixture.now = fixture.now.Add(time.Second)
		response := connectorJSON(t, handler, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "same-client", "version": "1"}}}, connectorBearer(tc.token))
		var out struct {
			Result struct {
				Instructions string `json:"instructions"`
			} `json:"result"`
		}
		connectorDecode(t, response, &out)
		if response.Code != http.StatusOK || strings.Contains(out.Result.Instructions, "factory_overview") != tc.overview || strings.Contains(out.Result.Instructions, "task_evidence_read") != tc.task {
			t.Fatalf("cached another grant's authority: %s", response.Body.String())
		}
	}
}

func TestDiscussionContinuationGrantRevisionAndExpiry(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, fixture.db)
	revision := "r1"
	calls := 0
	fixture.options.TaskEvidence = fixtureEvidence(func(_ context.Context, _ store.ConnectorGrantTarget, r EvidenceRequest) (EvidencePage, error) {
		calls++
		return EvidencePage{Revision: revision, Items: []map[string]any{{"id": "only permitted marker"}}, HasMore: true, NextOffset: r.Offset + 1}, nil
	})
	server, err := New(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	server.guidance = fstest.MapFS{"guidance/factory_overview.md": {Data: []byte("factory_overview")}, "guidance/task_evidence_read.md": {Data: []byte("task_evidence_read")}}
	handler := server.Handler()
	client := registerConnectorClient(t, handler, "client", "none")
	firstToken, _ := issueDiscussionTokens(t, fixture, client, target, DiscussionScope)
	secondToken, _ := issueDiscussionTokens(t, fixture, client, target, DiscussionScope)
	read := func(token, cursor string) *httptest.ResponseRecorder {
		t.Helper()
		fixture.now = fixture.now.Add(time.Second)
		return connectorJSON(t, handler, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": map[string]any{"task_ref": target.TaskRef, "section": "status", "cursor": cursor}}}, connectorBearer(token))
	}
	first := read(firstToken, "")
	var out struct {
		Result struct {
			Structured struct {
				Evidence struct {
					Cursor string `json:"next_cursor"`
				} `json:"evidence"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	connectorDecode(t, first, &out)
	cursor := out.Result.Structured.Evidence.Cursor
	if cursor == "" {
		t.Fatalf("missing continuation: %s", first.Body.String())
	}
	cross := read(secondToken, cursor)
	if calls != 1 || strings.Contains(cross.Body.String(), "only permitted marker") {
		t.Fatalf("cross-grant continuation released evidence: %s", cross.Body.String())
	}
	revision = "r2"
	changed := read(firstToken, cursor)
	if calls != 2 || strings.Contains(changed.Body.String(), "only permitted marker") {
		t.Fatalf("revision change released continuation: %s", changed.Body.String())
	}
	fresh := read(firstToken, "")
	connectorDecode(t, fresh, &out)
	cursor = out.Result.Structured.Evidence.Cursor
	before := calls
	claims, err := server.openCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	claims.Expires = fixture.now.Add(-time.Second).Unix()
	cursor, err = server.sealCursor(claims)
	if err != nil {
		t.Fatal(err)
	}
	expired := read(firstToken, cursor)
	if calls != before || strings.Contains(expired.Body.String(), "only permitted marker") {
		t.Fatalf("expired cursor reached provider: %s", expired.Body.String())
	}
}
