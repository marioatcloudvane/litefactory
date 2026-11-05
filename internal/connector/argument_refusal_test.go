package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

const refusalSentinel = "SECRET-SENTINEL-1410"

// refusalBrainstorms counts the brainstorm calls the refusal cases could
// reach; any other method panics through the nil embedded interface.
type refusalBrainstorms struct {
	Brainstorms
	calls *atomic.Int64
}

func (b refusalBrainstorms) Get(context.Context, owneraction.Principal, string) (brainstorm.View, error) {
	b.calls.Add(1)
	return brainstorm.View{}, nil
}

func (b refusalBrainstorms) SearchIssues(context.Context, owneraction.Principal, string, string, int) (brainstorm.IssueSearchView, error) {
	b.calls.Add(1)
	return brainstorm.IssueSearchView{}, nil
}

type refusalEvidence func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error)

func (f refusalEvidence) Evidence(ctx context.Context, t store.ConnectorGrantTarget, r EvidenceRequest) (EvidencePage, error) {
	return f(ctx, t, r)
}

type refusalFixture struct {
	f       *connectorFixture
	handler http.Handler
	target  store.ConnectorGrantTarget
	source  atomic.Int64
	calls   atomic.Int64 // provider calls across every tool family
	offset  int
}

func newRefusalFixture(t *testing.T) *refusalFixture {
	t.Helper()
	r := &refusalFixture{f: newConnectorFixture(t, nil)}
	r.target = seedDiscussionTarget(t, r.f.db)
	r.f.options.Usage = &usageTestProvider{call: func(context.Context, UsageRequest) (UsageProjection, error) {
		r.calls.Add(1)
		return usageTestProjection(1), nil
	}}
	r.f.options.TaskEvidence = refusalEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
		r.calls.Add(1)
		return EvidencePage{}, nil
	})
	r.f.options.CodeEvidence = codeProtocolProvider(func(context.Context, store.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error) {
		r.calls.Add(1)
		return CodeEvidencePage{}, nil
	})
	server, err := New(t.Context(), r.f.options)
	if err != nil {
		t.Fatal(err)
	}
	server.SetBrainstorm(refusalBrainstorms{calls: &r.calls})
	r.handler = server.Handler()
	return r
}

func (r *refusalFixture) token(t *testing.T, target store.ConnectorGrantTarget, scope string) string {
	t.Helper()
	client := registerConnectorClient(t, r.handler, "refusal client", "none")
	token, _ := issueDiscussionTokens(t, r.f, client, target, scope)
	return token
}

type refusalReply struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Tool     string `json:"tool"`
			Argument string `json:"argument"`
			Problem  string `json:"problem"`
		} `json:"data"`
	} `json:"error"`
	Result *struct {
		IsError bool          `json:"isError"`
		Content []TextContent `json:"content"`
	} `json:"result"`
}

type refusalLog struct {
	Event, Outcome, Reason, Detail, Tool string
}

// call sends one tools/call with raw arguments and returns the response body,
// its decoded form and the single "connector outcome" line it wrote.
func (r *refusalFixture) call(t *testing.T, token, name, args string) (string, refusalReply, refusalLog, string) {
	t.Helper()
	nameJSON, _ := json.Marshal(name)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":` + string(nameJSON) + `,"arguments":` + args + `}}`
	req := httptest.NewRequest(http.MethodPost, r.f.options.Config.ResourceURL, strings.NewReader(body)).WithContext(t.Context())
	n := r.source.Add(1)
	req.RemoteAddr = fmt.Sprintf("127.1.%d.%d:12345", n/250, n%250+1)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", protocolLatest)
	r.offset = len(r.f.logs.String())
	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d %s", name, recorder.Code, recorder.Body.String())
	}
	var reply refusalReply
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil {
		t.Fatalf("%s: %v in %s", name, err, recorder.Body.String())
	}
	logs := r.f.logs.String()
	fresh := logs[r.offset:]
	var outcomes []refusalLog
	for _, line := range strings.Split(strings.TrimSpace(fresh), "\n") {
		var entry struct {
			Msg string
			refusalLog
		}
		if line == "" || json.Unmarshal([]byte(line), &entry) != nil || entry.Msg != "connector outcome" {
			continue
		}
		outcomes = append(outcomes, entry.refusalLog)
	}
	if len(outcomes) != 1 {
		t.Fatalf("%s wrote %d outcome lines: %s", name, len(outcomes), fresh)
	}
	return recorder.Body.String(), reply, outcomes[0], fresh
}

// refused asserts a named -32602 refusal and its log line, and that the
// sentinel value stays out of both.
func refused(t *testing.T, label, body string, reply refusalReply, log refusalLog, logLine, tool, argument string, words ...string) {
	t.Helper()
	if reply.Error == nil || reply.Error.Code != -32602 {
		t.Fatalf("%s: not a -32602 refusal: %s", label, body)
	}
	message := reply.Error.Message
	if !strings.HasPrefix(message, "Invalid params") || !strings.Contains(message, tool) {
		t.Fatalf("%s: message %q does not name tool %s", label, message, tool)
	}
	if argument != "" && !strings.Contains(message, `"`+argument+`"`) {
		t.Fatalf("%s: message %q does not name argument %q", label, message, argument)
	}
	for _, word := range words {
		if !strings.Contains(message, word) {
			t.Fatalf("%s: message %q lacks %q", label, message, word)
		}
	}
	if reply.Error.Data.Tool != tool || reply.Error.Data.Argument != argument || reply.Error.Data.Problem == "" {
		t.Fatalf("%s: data %+v, want tool %s argument %q", label, reply.Error.Data, tool, argument)
	}
	if log.Event != "tool_call" || log.Outcome != "refusal" || log.Reason != "params" || log.Tool != tool {
		t.Fatalf("%s: log %+v, want tool_call refusal params %s", label, log, tool)
	}
	if strings.Contains(body, refusalSentinel) || strings.Contains(logLine, refusalSentinel) {
		t.Fatalf("%s: refused value echoed: %s %s", label, body, logLine)
	}
}

func TestToolArgumentRefusalsNameToolArgumentAndProblem(t *testing.T) {
	t.Parallel()
	r := newRefusalFixture(t)
	brainstormToken := r.token(t, r.target, BrainstormScope)
	usageToken := r.token(t, store.ConnectorGrantTarget{}, FactoryUsageScope)
	codeToken := r.token(t, r.target, DiscussionScope+" "+CodeReviewScope)

	// C1, C6: a missing required argument, logged under the real tool name.
	body, reply, log, line := r.call(t, brainstormToken, "brainstorm_get", `{}`)
	refused(t, "missing", body, reply, log, line, "brainstorm_get", "brainstorm", "required")
	body, reply, log, line = r.call(t, brainstormToken, "brainstorm_get", `{"brainstorm":"`+strings.Repeat("x", 200)+refusalSentinel+`"}`)
	refused(t, "too long", body, reply, log, line, "brainstorm_get", "brainstorm")

	// C2: a wrong-typed argument names its expected type.
	body, reply, log, line = r.call(t, usageToken, "usage_read", `{"scope":"task","repository":"github:o/p","issue":"`+refusalSentinel+`"}`)
	refused(t, "wrong type", body, reply, log, line, "usage_read", "issue", "integer")

	// C3: an unknown argument is named only when its key is short and safe.
	body, reply, log, line = r.call(t, brainstormToken, "issue_search", `{"repository":"github:acme/api","text":"voice","query":"`+refusalSentinel+`"}`)
	refused(t, "unknown", body, reply, log, line, "issue_search", "query", "not an argument of this tool")
	for _, key := range []string{"Bad-Key<script>", strings.Repeat("k", 65)} {
		body, reply, log, line = r.call(t, brainstormToken, "issue_search", `{"repository":"github:acme/api","text":"voice",`+string(must(json.Marshal(key)))+`:"`+refusalSentinel+`"}`)
		refused(t, "unsafe unknown", body, reply, log, line, "issue_search", "", "unknown argument")
		if strings.Contains(body, key) || strings.Contains(line, key) {
			t.Fatalf("unsafe key %q echoed: %s %s", key, body, line)
		}
	}

	// C4: a cursor combined with selectors names the cursor.
	body, reply, log, line = r.call(t, codeToken, "repo_read", `{"cursor":"`+refusalSentinel+`","task_ref":"`+r.target.TaskRef+`"}`)
	refused(t, "cursor conflict", body, reply, log, line, "repo_read", "cursor")

	// C7: no refused call reached a provider.
	if n := r.calls.Load(); n != 0 {
		t.Fatalf("refused calls reached providers %d times", n)
	}

	// C5: an unknown tool gets the fixed marker and a tools/list hint.
	const unknownTool = "no_such_tool_" + refusalSentinel
	body, reply, log, line = r.call(t, brainstormToken, unknownTool, `{}`)
	if reply.Error == nil || reply.Error.Code != -32602 || !strings.Contains(reply.Error.Message, "Unknown tool") || !strings.Contains(reply.Error.Message, "tools/list") {
		t.Fatalf("unknown tool: %s", body)
	}
	if log.Event != "tool_call" || log.Outcome != "refusal" || log.Tool != "unknown" || strings.Contains(body, unknownTool) || strings.Contains(line, unknownTool) {
		t.Fatalf("unknown tool log %+v or echo: %s %s", log, body, line)
	}

	// C8: a valid call still succeeds and logs its real name.
	_, reply, log, _ = r.call(t, usageToken, "usage_read", `{"scope":"factory"}`)
	if reply.Error != nil || reply.Result == nil || reply.Result.IsError || log.Outcome != "success" || log.Reason != "completed" || log.Tool != "usage_read" {
		t.Fatalf("valid usage_read: %+v log %+v", reply, log)
	}
}

func TestTaskEvidenceInvalidCursorSaysRestartWithoutCursor(t *testing.T) {
	t.Parallel()
	r := newRefusalFixture(t)
	token := r.token(t, r.target, DiscussionScope)
	body, reply, log, _ := r.call(t, token, "task_evidence_read", `{"task_ref":"`+r.target.TaskRef+`","section":"status","cursor":"not-a-cursor"}`)
	if reply.Result == nil || !reply.Result.IsError || len(reply.Result.Content) == 0 {
		t.Fatalf("invalid cursor: %s", body)
	}
	text := reply.Result.Content[0].Text
	if !strings.Contains(text, "cursor") || !strings.Contains(text, "without") {
		t.Fatalf("invalid cursor summary %q does not name the cursor and recovery", text)
	}
	if log.Tool != "task_evidence_read" {
		t.Fatalf("log %+v", log)
	}
	if n := r.calls.Load(); n != 0 {
		t.Fatalf("invalid cursor reached the provider %d times", n)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
