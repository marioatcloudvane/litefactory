package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMCPProtocolAndBrowserBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, method, body string
		headers            http.Header
		want               int
		code               int
	}{
		{name: "get authenticated", method: http.MethodGet, want: 405},
		{name: "unsupported version", body: `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers: http.Header{"MCP-Protocol-Version": {"1999-01-01"}}, want: 400},
		{name: "duplicate version", body: `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers: http.Header{"MCP-Protocol-Version": {"2025-06-18", "2025-11-25"}}, want: 400},
		{name: "server session forbidden", body: `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers: http.Header{"Mcp-Session-Id": {"invented"}}, want: 400},
		{name: "mutation unavailable", body: `{"jsonrpc":"2.0","id":1,"method":"factory/approve"}`, want: 200, code: -32601},
		{name: "batch forbidden", body: `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, want: 200, code: -32600},
		{name: "invalid identifier", body: `{"jsonrpc":"2.0","id":{},"method":"ping"}`, want: 200, code: -32600},
		{name: "unknown tool", body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mutation_tool"}}`, want: 200, code: -32602},
		{name: "notification", body: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, want: 202},
		{name: "cross origin", body: `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers: http.Header{"Origin": {"https://evil.example"}}, want: 403},
		{name: "duplicate origin", body: `{"jsonrpc":"2.0","id":1,"method":"ping"}`, headers: http.Header{"Origin": {"https://client.example", "https://evil.example"}}, want: 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newConnectorFixture(t, nil)
			server := fixture.server(t)
			client := registerConnectorClient(t, server, "Client", "none")
			tokens, _ := fixture.issueTokens(t, server, client)
			headers := connectorBearer(tokens.AccessToken)
			headers.Set("Accept", "application/json")
			for key, values := range tc.headers {
				headers.Del(key)
				for _, value := range values {
					headers.Add(key, value)
				}
			}
			method := tc.method
			if method == "" {
				method = http.MethodPost
			}
			response := connectorRequest(t, server, method, fixture.options.Config.ResourceURL, "application/json", tc.body, headers)
			if response.Code != tc.want {
				t.Fatalf("HTTP %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
			if response.Header().Get("Mcp-Session-Id") != "" {
				t.Fatal("server issued a protocol session")
			}
			if tc.code != 0 {
				var result struct {
					Error struct {
						Code int `json:"code"`
					} `json:"error"`
				}
				connectorDecode(t, response, &result)
				if result.Error.Code != tc.code {
					t.Fatalf("RPC error %d, want %d", result.Error.Code, tc.code)
				}
			}
		})
	}
}

func TestMCPRejectsCookiesAndUntrustedProxy(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	server := fixture.server(t)
	cookie := &http.Cookie{Name: "__Host-lf_connector", Value: "browser-cookie-marker"}
	response := connectorRequest(t, server, http.MethodPost, fixture.options.Config.ResourceURL, "application/json", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, http.Header{"Accept": {"application/json"}}, cookie)
	if response.Code != 401 {
		t.Fatalf("browser cookie authorized MCP: HTTP %d", response.Code)
	}
	for _, tc := range []struct{ host, remote, source string }{
		{"evil.example", "127.0.0.1:1234", ""},
		{"cloudvane.at", "192.0.2.1:1234", ""},
		{"cloudvane.at", "127.0.0.1:1234", "spoofed, 1.2.3.4"},
	} {
		req := httptest.NewRequest(http.MethodGet, fixture.options.Config.ResourceURL, nil).WithContext(t.Context())
		req.Host = tc.host
		req.RemoteAddr = tc.remote
		if tc.source != "" {
			req.Header.Set("X-Real-IP", tc.source)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("untrusted host/proxy accepted: HTTP %d", response.Code)
		}
	}
}

func TestRequestSizeAndRateLimits(t *testing.T) {
	t.Parallel()
	t.Run("known and streamed oversize", func(t *testing.T) {
		fixture := newConnectorFixture(t, nil)
		server := fixture.server(t)
		client := registerConnectorClient(t, server, "Client", "none")
		tokens, _ := fixture.issueTokens(t, server, client)
		for _, length := range []int64{MaxRequestBytes + 1, -1} {
			req := httptest.NewRequest(http.MethodPost, fixture.options.Config.ResourceURL, strings.NewReader(strings.Repeat("x", MaxRequestBytes+1))).WithContext(t.Context())
			req.RemoteAddr = "127.0.0.1:1234"
			req.ContentLength = length
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("length %d: HTTP %d", length, response.Code)
			}
		}
	})
	t.Run("source burst and refill", func(t *testing.T) {
		fixture := newConnectorFixture(t, nil)
		server := fixture.server(t)
		for i := range 11 {
			response := connectorRequest(t, server, http.MethodGet, "https://cloudvane.at/.well-known/oauth-protected-resource/litefactory/mcp", "", "", nil)
			want := 200
			if i == 10 {
				want = 429
			}
			if response.Code != want {
				t.Fatalf("request %d: HTTP %d, want %d", i, response.Code, want)
			}
			if want == 429 && response.Header().Get("Retry-After") == "" {
				t.Fatal("overload omitted retry guidance")
			}
		}
		fixture.now = fixture.now.Add(time.Second)
		response := connectorRequest(t, server, http.MethodGet, "https://cloudvane.at/.well-known/oauth-protected-resource/litefactory/mcp", "", "", nil)
		if response.Code != 200 {
			t.Fatalf("rate did not refill: HTTP %d", response.Code)
		}
	})
	t.Run("global burst", func(t *testing.T) {
		fixture := newConnectorFixture(t, nil)
		server := fixture.server(t)
		for i := range 31 {
			headers := http.Header{"X-Real-IP": {fmt.Sprintf("192.0.2.%d", i+1)}}
			response := connectorRequest(t, server, http.MethodGet, "https://cloudvane.at/.well-known/oauth-protected-resource/litefactory/mcp", "", "", headers)
			want := 200
			if i == 30 {
				want = 429
			}
			if response.Code != want {
				t.Fatalf("global request %d: HTTP %d, want %d", i, response.Code, want)
			}
		}
	})
}

func TestToolConcurrencyCancellationAndBounds(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, MaxToolReads)
	release := make(chan struct{})
	var calls atomic.Int32
	provider := connectorFixtureOverview(func(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
		calls.Add(1)
		entered <- struct{}{}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > ToolTimeout {
			t.Error("tool deadline missing")
		}
		select {
		case <-release:
			return ToolResult{Content: []TextContent{{Type: "text", Text: "done"}}}, nil
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	})
	fixture := newConnectorFixture(t, provider)
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	requestBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factory_overview","arguments":{}}}`
	done := make(chan *httptest.ResponseRecorder, MaxToolReads)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range MaxToolReads {
		go func() {
			req := httptest.NewRequest(http.MethodPost, fixture.options.Config.ResourceURL, strings.NewReader(requestBody)).WithContext(ctx)
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			req.Header.Set("MCP-Protocol-Version", "2025-11-25")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			done <- response
		}()
	}
	for range MaxToolReads {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("provider did not enter")
		}
	}
	overflow := connectorRequest(t, server, http.MethodPost, fixture.options.Config.ResourceURL, "application/json", requestBody, http.Header{"Authorization": {"Bearer " + tokens.AccessToken}, "MCP-Protocol-Version": {"2025-11-25"}, "Accept": {"application/json"}})
	if overflow.Code != 429 || calls.Load() != MaxToolReads {
		t.Fatalf("fifth tool was not refused before execution: HTTP %d, calls %d", overflow.Code, calls.Load())
	}
	cancel()
	for range MaxToolReads {
		select {
		case response := <-done:
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"isError":true`) {
				t.Fatalf("cancelled provider did not return sanitized tool error: HTTP %d %s", response.Code, response.Body.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("provider ignored request cancellation")
		}
	}
	close(release)
}

func TestOversizeToolOutputIsRejectedBeforeWrite(t *testing.T) {
	t.Parallel()
	marker := strings.Repeat("private-overview-marker", MaxResponseBytes/10)
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) {
		return ToolResult{Content: []TextContent{{Type: "text", Text: marker}}}, nil
	}))
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	response := connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "factory_overview", "arguments": map[string]any{}}}, connectorBearer(tokens.AccessToken))
	if response.Code != 500 || response.Body.Len() > MaxResponseBytes || strings.Contains(response.Body.String(), "private-overview-marker") {
		t.Fatalf("oversize output escaped: HTTP %d, bytes %d", response.Code, response.Body.Len())
	}
	if strings.Contains(fixture.logs.String(), "private-overview-marker") {
		t.Fatal("oversize overview leaked into logs")
	}
}

type connectorBlockingBody struct {
	entered chan<- struct{}
	release <-chan struct{}
	read    bool
}

func (b *connectorBlockingBody) Read([]byte) (int, error) {
	if !b.read {
		b.read = true
		b.entered <- struct{}{}
	}
	<-b.release
	return 0, io.EOF
}
func (*connectorBlockingBody) Close() error { return nil }

func TestHTTPHandlerCapacityHasNoQueue(t *testing.T) {
	t.Parallel()
	fixture := newConnectorFixture(t, nil)
	var tick atomic.Int64
	fixture.options.Now = func() time.Time { return fixture.now.Add(time.Duration(tick.Load()) * time.Second) }
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	entered := make(chan struct{}, MaxHandlers)
	release := make(chan struct{})
	done := make(chan struct{}, MaxHandlers)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	for i := range MaxHandlers {
		tick.Store(int64(i + 1))
		go func() {
			req := httptest.NewRequest(http.MethodPost, fixture.options.Config.ResourceURL, nil).WithContext(t.Context())
			req.RemoteAddr = "127.0.0.1:1234"
			req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
			req.Header.Set("MCP-Protocol-Version", "2025-11-25")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Body = &connectorBlockingBody{entered: entered, release: release}
			server.ServeHTTP(httptest.NewRecorder(), req)
			done <- struct{}{}
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("handler did not enter body read")
		}
	}
	tick.Add(1)
	response := connectorRequest(t, server, http.MethodGet, "https://cloudvane.at/.well-known/oauth-protected-resource/litefactory/mcp", "", "", nil)
	if response.Code != 429 || response.Header().Get("Retry-After") == "" {
		t.Fatalf("33rd handler queued or accepted: HTTP %d", response.Code)
	}
	// Release before joining; the deferred closure handles early failures too.
	close(release)
	for range MaxHandlers {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("handler failed to finish after body release")
		}
	}
}

func TestToolDiagnosticsExcludeSecretsAndOverview(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) {
		if count.Add(1) == 1 {
			return ToolResult{Content: []TextContent{{Type: "text", Text: "overview-content-marker"}}}, nil
		}
		return ToolResult{}, errors.New("provider-secret-marker")
	}))
	server := fixture.server(t)
	client := registerConnectorClient(t, server, "Client", "none")
	tokens, _ := fixture.issueTokens(t, server, client)
	call := func(name, version, token string) *httptest.ResponseRecorder {
		headers := connectorBearer(token)
		headers.Set("MCP-Protocol-Version", version)
		return connectorJSON(t, server, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}}, headers)
	}
	if response := call("factory_overview", "2025-11-25", tokens.AccessToken); response.Code != 200 {
		t.Fatalf("successful tool: HTTP %d", response.Code)
	}
	if response := call("factory_overview", "2025-11-25", tokens.AccessToken); response.Code != 200 || !strings.Contains(response.Body.String(), `"isError":true`) {
		t.Fatal("provider failure did not become sanitized MCP tool error")
	}
	call("untrusted-tool-secret-marker", "2025-11-25", tokens.AccessToken)
	call("factory_overview", "unsupported", tokens.AccessToken)
	call("factory_overview", "2025-11-25", "credential-secret-marker")
	logs := fixture.logs.String()
	for _, secret := range []string{tokens.AccessToken, tokens.RefreshToken, "overview-content-marker", "provider-secret-marker", "untrusted-tool-secret-marker", "credential-secret-marker"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("diagnostics leaked %q", secret)
		}
	}
	outcomes := map[string]bool{}
	unavailable := false
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] != "tool_call" && event["event"] != "connection" {
			continue
		}
		correlation, ok := event["correlation_id"].(string)
		if !ok || correlation == "" {
			t.Fatal("diagnostic lacks server correlation")
		}
		if _, ok := event["duration_ms"].(float64); !ok {
			t.Fatal("diagnostic lacks duration")
		}
		if event["tool"] == "unavailable" {
			unavailable = true
		}
		if event["event"] == "tool_call" {
			outcome, ok := event["outcome"].(string)
			if !ok {
				t.Fatal("tool diagnostic lacks outcome")
			}
			outcomes[outcome] = true
			if event["tool"] != "factory_overview" && event["tool"] != "unknown" {
				t.Fatal("parseable tool identity was lost")
			}
		}
	}
	for _, outcome := range []string{"success", "refusal", "error"} {
		if !outcomes[outcome] {
			t.Fatalf("missing %s tool diagnostic", outcome)
		}
	}
	if !unavailable {
		t.Fatal("unauthenticated refusal did not use unavailable tool identity")
	}
	if !strings.Contains(logs, `"reason":"protocol","tool":"factory_overview"`) {
		t.Fatal("parseable protocol refusal lost safe tool identity")
	}
}

func TestToolArgumentsUseObjectSemantics(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) {
		calls.Add(1)
		return ToolResult{Content: []TextContent{{Type: "text", Text: "ok"}}}, nil
	}))
	handler := fixture.server(t)
	client := registerConnectorClient(t, handler, "Client", "none")
	tokens, _ := fixture.issueTokens(t, handler, client)
	for _, tc := range []struct {
		arguments string
		valid     bool
	}{{"{ }", true}, {"{}", true}, {"null", false}, {"[]", false}, {`{"extra":true}`, false}} {
		response := connectorRequest(t, handler, http.MethodPost, fixture.options.Config.ResourceURL, "application/json", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factory_overview","arguments":`+tc.arguments+`}}`, http.Header{"Authorization": {"bearer " + tokens.AccessToken}, "Accept": {"application/json"}, "MCP-Protocol-Version": {"2025-11-25"}})
		if response.Code != http.StatusOK {
			t.Fatalf("arguments %s HTTP %d", tc.arguments, response.Code)
		}
		if strings.Contains(response.Body.String(), `"error"`) == tc.valid {
			t.Fatalf("arguments %s result %s", tc.arguments, response.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("invalid arguments reached provider: %d", calls.Load())
	}
}
