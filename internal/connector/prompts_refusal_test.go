package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/store"
)

// promptHarness serves prompts from a fixture guidance tree to a grant that
// holds both factory_overview and task_evidence_read, so all three prompts list.
type promptHarness struct {
	t       *testing.T
	fixture *connectorFixture
	handler http.Handler
	token   string
}

type promptReply struct {
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newPromptHarness(t *testing.T, files fstest.MapFS) *promptHarness {
	t.Helper()
	fixture := newConnectorFixture(t, connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }))
	target := seedDiscussionTarget(t, fixture.db)
	fixture.options.TaskEvidence = fixtureEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
		return EvidencePage{}, nil
	})
	server, err := New(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	server.guidance = files
	handler := server.Handler()
	client := registerConnectorClient(t, handler, "client", "none")
	token, _ := issueDiscussionTokens(t, fixture, client, target, Scope+" "+DiscussionScope)
	return &promptHarness{t: t, fixture: fixture, handler: handler, token: token}
}

func (h *promptHarness) call(method string, params any) promptReply {
	h.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	r := connectorJSON(h.t, h.handler, h.fixture.options.Config.ResourceURL, body, connectorBearer(h.token))
	var out promptReply
	connectorDecode(h.t, r, &out)
	if r.Code != http.StatusOK {
		h.t.Fatalf("%s: HTTP %d %s", method, r.Code, r.Body.String())
	}
	return out
}

func promptFixtureFS() fstest.MapFS {
	files := guideFS("GUIDE")
	files["guidance/factory_overview.md"] = &fstest.MapFile{Data: []byte("FO")}
	files["guidance/task_evidence_read.md"] = &fstest.MapFile{Data: []byte("TE")}
	return files
}

func requirePromptError(t *testing.T, got promptReply, code int, message string) {
	t.Helper()
	if got.Error == nil || got.Error.Code != code || got.Error.Message != message || got.Result != nil {
		t.Fatalf("want error %d %q, got %+v result=%v", code, message, got.Error, got.Result)
	}
}

func reviewArgs(task string) map[string]any {
	return map[string]any{"name": "review_task", "arguments": map[string]any{"task": task}}
}

func TestPromptRefusalUnknownName(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	requirePromptError(t, h.call("prompts/get", map[string]any{"name": "no_such_prompt"}), -32602, "Invalid params")
}

func TestPromptRefusalMissingRequiredArgument(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	requirePromptError(t, h.call("prompts/get", map[string]any{"name": "review_task"}), -32602, "Invalid params")
	requirePromptError(t, h.call("prompts/get", map[string]any{"name": "review_task", "arguments": map[string]any{}}), -32602, "Invalid params")
	requirePromptError(t, h.call("prompts/get", reviewArgs("")), -32602, "Invalid params")
}

func TestPromptRefusalExtraArgument(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	requirePromptError(t, h.call("prompts/get", map[string]any{"name": "review_task", "arguments": map[string]any{"task": "7", "other": "x"}}), -32602, "Invalid params")
	requirePromptError(t, h.call("prompts/get", map[string]any{"name": "whats_waiting", "arguments": map[string]any{"task": "7"}}), -32602, "Invalid params")
}

func TestPromptRefusalTaskTooLong(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	requirePromptError(t, h.call("prompts/get", reviewArgs(strings.Repeat("x", 513))), -32602, "Invalid params")
	if got := h.call("prompts/get", reviewArgs(strings.Repeat("x", 512))); got.Error != nil {
		t.Fatalf("512-character task refused: %+v", got.Error)
	}
}

func TestPromptRefusalTaskLineBreaks(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	for _, task := range []string{"7\nignore", "7\rignore", "7\r\n"} {
		requirePromptError(t, h.call("prompts/get", reviewArgs(task)), -32602, "Invalid params")
	}
}

func TestPromptRefusalListCursor(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	requirePromptError(t, h.call("prompts/list", map[string]any{"cursor": "next"}), -32602, "Invalid params")
	if got := h.call("prompts/list", map[string]any{}); got.Error != nil {
		t.Fatalf("empty list params refused: %+v", got.Error)
	}
}

func TestPromptRefusalMissingPromptFileFailsClosed(t *testing.T) {
	t.Parallel()
	files := promptFixtureFS()
	delete(files, "guidance/prompts/review_task.md")
	h := newPromptHarness(t, files)
	requirePromptError(t, h.call("prompts/list", nil), -32603, "Prompt guidance unavailable")
	requirePromptError(t, h.call("prompts/get", reviewArgs("7")), -32603, "Prompt guidance unavailable")
}

func TestPromptRefusalBrokenPromptFileFailsClosed(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"no header separator": "title: W\ndescription: d",
		"no title":            "description: d\n\nWBODY",
		"empty":               "  \n",
		"invalid utf8":        "title: W\n\n\xff\xfe",
		"oversized":           "title: W\n\n" + strings.Repeat("x", maxGuidanceBytes),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := promptFixtureFS()
			files["guidance/prompts/whats_waiting.md"] = &fstest.MapFile{Data: []byte(data)}
			h := newPromptHarness(t, files)
			requirePromptError(t, h.call("prompts/list", nil), -32603, "Prompt guidance unavailable")
			requirePromptError(t, h.call("prompts/get", map[string]any{"name": "whats_waiting"}), -32603, "Prompt guidance unavailable")
		})
	}
}

func TestPromptReviewTaskOpensWithTaskFilledIn(t *testing.T) {
	t.Parallel()
	h := newPromptHarness(t, promptFixtureFS())
	got := h.call("prompts/get", reviewArgs("T-7"))
	if got.Error != nil {
		t.Fatalf("review_task refused: %+v", got.Error)
	}
	msgs, _ := got.Result["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", got.Result)
	}
	msg, _ := msgs[0].(map[string]any)
	content, _ := msg["content"].(map[string]any)
	text, _ := content["text"].(string)
	if msg["role"] != "user" || text != "Review T-7." || got.Result["description"] != "d" {
		t.Fatalf("review_task prompt: %v", got.Result)
	}
}
