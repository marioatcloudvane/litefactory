package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOverviewToolContract(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) {
		calls.Add(1)
		return ToolResult{Content: []TextContent{{Type: "text", Text: "ok"}}}, nil
	}))
	handler := fixture.server(t)
	client := registerConnectorClient(t, handler, "Owner overview", "none")
	tokens, _ := fixture.issueTokens(t, handler, client)
	listed := connectorJSON(t, handler, fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, connectorBearer(tokens.AccessToken))
	var response struct {
		Result struct {
			Tools []struct {
				Name  string `json:"name"`
				Input struct {
					Type       string `json:"type"`
					Additional bool   `json:"additionalProperties"`
				} `json:"inputSchema"`
				Output struct {
					Type       string                     `json:"type"`
					Required   []string                   `json:"required"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"outputSchema"`
				Annotations map[string]bool `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	connectorDecode(t, listed, &response)
	if listed.Code != http.StatusOK || len(response.Result.Tools) != 1 {
		t.Fatalf("read tool list: %d %s", listed.Code, listed.Body.String())
	}
	tool := response.Result.Tools[0]
	if tool.Name != "factory_overview" || tool.Input.Type != "object" || tool.Input.Additional || !tool.Annotations["readOnlyHint"] || tool.Annotations["destructiveHint"] || !tool.Annotations["idempotentHint"] {
		t.Fatalf("invalid read contract: %+v", tool)
	}
	if tool.Output.Type != "object" || len(tool.Output.Required) != 8 {
		t.Fatalf("missing output contract: %+v", tool.Output)
	}
	for _, name := range []string{"activity", "owner_waits", "recent_publications"} {
		var list struct {
			Properties struct {
				Items struct {
					MaxItems int `json:"maxItems"`
				} `json:"items"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(tool.Output.Properties[name], &list); err != nil || list.Properties.Items.MaxItems != 50 {
			t.Fatalf("%s missing independent limit", name)
		}
	}
	for _, params := range []string{`{"name":"factory_overview"}`, `{"name":"factory_overview","arguments":"{}"}`, `{"name":"factory_overview","arguments":false}`} {
		rejected := connectorRequest(t, handler, http.MethodPost, fixture.options.Config.ResourceURL, "application/json", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":`+params+`}`, http.Header{"Authorization": {"Bearer " + tokens.AccessToken}, "MCP-Protocol-Version": {"2025-11-25"}, "Accept": {"application/json"}})
		if rejected.Code != http.StatusOK || !strings.Contains(rejected.Body.String(), `"code":-32602`) {
			t.Fatalf("invalid arguments accepted: %s", rejected.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments reached read service")
	}
}
