package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
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

// refusalActions counts history reads; any other method panics through the nil
// embedded interface.
type refusalActions struct {
	OwnerActions
	calls *atomic.Int64
}

func (a refusalActions) HistoryFiltered(context.Context, owneraction.Principal, owneraction.Filters, string, int) (owneraction.Page, error) {
	a.calls.Add(1)
	return owneraction.Page{}, nil
}

// refusalDiscussion counts advisory job reads; any other method panics through
// the nil embedded interface.
type refusalDiscussion struct {
	TaskDiscussion
	calls *atomic.Int64
}

func (d refusalDiscussion) Get(context.Context, owneraction.Principal, string, string) (taskdiscussion.JobView, error) {
	d.calls.Add(1)
	return taskdiscussion.JobView{}, nil
}

func (refusalDiscussion) PublicationAvailable() bool { return false }

// refusalWorkflows makes the workflow tools available; refused calls never
// reach it, so every method panics through the nil embedded interface.
type refusalWorkflows struct{ Workflows }

// #1421 C2, C5, C6: hosted clients send every optional schema field, empty;
// such a call parses exactly like the call that leaves those fields out.
func TestEmptyOptionalArgumentsParseAsOmitted(t *testing.T) {
	t.Parallel()
	summary := `{"question":"Why?","conclusion":"Because.","alternatives_or_uncertainty":"None.","evidence_basis":"Saved review.","attribution":"Agent recommendation."}`
	type parsed func(name string, raw json.RawMessage) (any, error)
	families := map[string]parsed{
		"workflows":  func(n string, r json.RawMessage) (any, error) { return parseWorkflowArguments(n, r) },
		"evidence":   func(_ string, r json.RawMessage) (any, error) { return parseEvidenceArguments(r) },
		"usage":      func(_ string, r json.RawMessage) (any, error) { return parseUsageArguments(r) },
		"code":       func(n string, r json.RawMessage) (any, error) { return parseCodeArguments(n, r) },
		"advisory":   func(n string, r json.RawMessage) (any, error) { return parseAdvisoryArguments(n, r) },
		"brainstorm": func(n string, r json.RawMessage) (any, error) { return parseBrainstormArguments(n, r) },
		"decisions":  func(n string, r json.RawMessage) (any, error) { return parseDecisionArguments(n, r) },
		"actions":    func(n string, r json.RawMessage) (any, error) { return parseActionArguments(n, r) },
	}
	for _, tc := range []struct{ family, tool, empty, omitted string }{
		{"workflows", "workflows_list", `{"project":"","cursor":"","limit":0}`, `{}`},
		{"workflows", "workflows_list", `{"project":null,"cursor":null,"limit":null}`, `{}`},
		{"workflows", "workflow_describe", `{"workflow_ref":"w","version":"1","project":""}`, `{"workflow_ref":"w","version":"1"}`},
		{"workflows", "workflow_propose", `{"workflow_ref":"w","version":"1","project":null,"inputs":{},"client_request_key":"k"}`, `{"workflow_ref":"w","version":"1","inputs":{},"client_request_key":"k"}`},
		{"workflows", "workflow_job_get", `{"job":"j","cursor":"","limit":0}`, `{"job":"j"}`},
		{"evidence", "task_evidence_read", `{"task_ref":"t","section":"status","cursor":"","limit":0}`, `{"task_ref":"t","section":"status"}`},
		{"evidence", "task_evidence_read", `{"task_ref":"t","section":"status","cursor":null,"limit":null}`, `{"task_ref":"t","section":"status"}`},
		{"usage", "usage_read", `{"scope":"factory","repository":"","issue":0,"period":"","from":"","to":"","mode":"","agent":"","model":"","view":"","by":null,"limit":0,"cursor":""}`, `{"scope":"factory"}`},
		{"usage", "usage_read", `{"scope":"task","repository":"github:acme/api","issue":7,"period":null,"view":null,"limit":null}`, `{"scope":"task","repository":"github:acme/api","issue":7}`},
		{"code", "repo_read", `{"task_ref":"t","branch":"","path":"","start_line":0,"lines":0,"limit":0,"view_token":"","cursor":""}`, `{"task_ref":"t"}`},
		{"code", "repo_grep", `{"task_ref":"t","pattern":"x","path":null,"ignore_case":null,"cursor":null}`, `{"task_ref":"t","pattern":"x"}`},
		{"code", "repo_diff", `{"task_ref":"t","mode":"base_to_task","commit":"","path":""}`, `{"task_ref":"t","mode":"base_to_task"}`},
		{"code", "standards_read", `{"task_ref":"t","role":"","categories":null,"limit":0}`, `{"task_ref":"t"}`},
		{"code", "repo_read", `{"cursor":"opaque","task_ref":"","limit":0}`, `{"cursor":"opaque"}`},
		{"code", "repo_read", `{"cursor":"opaque","task_ref":null,"branch":null,"start_line":0}`, `{"cursor":"opaque"}`},
		{"advisory", "ask_task_agent", `{"task_ref":"t","question":"q","client_request_key":"k","conversation_ref":"","conversation_revision":0}`, `{"task_ref":"t","question":"q","client_request_key":"k"}`},
		{"advisory", "judge_task", `{"task_ref":"t","focus":"","client_request_key":"k","conversation_ref":null,"conversation_revision":null}`, `{"task_ref":"t","client_request_key":"k"}`},
		{"advisory", "ask_task_agent", `{"task_ref":"t","question":"q","client_request_key":"k","conversation_ref":"c","conversation_revision":0}`, `{"task_ref":"t","question":"q","client_request_key":"k","conversation_ref":"c"}`},
		{"advisory", "advisory_job_get", `{"job_ref":"j","cursor":""}`, `{"job_ref":"j"}`},
		{"advisory", "discussion_summary_publish", `{"task_ref":"t","conversation_ref":"c","conclusion_id":"one","summary":` + summary + `,"source_job_ref":""}`, `{"task_ref":"t","conversation_ref":"c","conclusion_id":"one","summary":` + summary + `}`},
		{"advisory", "discussion_summary_get", `{"publication_ref":"p","cancel_pending":null}`, `{"publication_ref":"p"}`},
		{"brainstorm", "brainstorm_entries", `{"brainstorm":"b","kind":"","cursor":"","limit":0}`, `{"brainstorm":"b"}`},
		{"brainstorm", "brainstorm_list", `{"repository":"github:acme/api","cursor":null,"limit":null}`, `{"repository":"github:acme/api"}`},
		{"brainstorm", "brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","document_name":"","issue_reference":"","request_key":"k","expected_revision":0}`, `{"brainstorm":"b","kind":"idea","text":"t","request_key":"k","expected_revision":0}`},
		{"brainstorm", "issue_search", `{"repository":"github:acme/api","text":"voice","limit":0}`, `{"repository":"github:acme/api","text":"voice"}`},
		{"brainstorm", "brainstorm_draft", `{"brainstorm":"b","request_key":"k","note":""}`, `{"brainstorm":"b","request_key":"k"}`},
		{"brainstorm", "brainstorm_scan_get", `{"scan":"s","cursor":"","limit":0}`, `{"scan":"s"}`},
		{"decisions", "approve_stage", `{"task_ref":"t","stage":"plan","client_request_key":"k","summary":` + summary + `,"conversation_ref":"","conclusion_ref":"","discord_channel_id":""}`, `{"task_ref":"t","stage":"plan","client_request_key":"k","summary":` + summary + `}`},
		{"decisions", "revise_task", `{"task_ref":"t","stage":"plan","change":"c","client_request_key":"k","summary":` + summary + `,"conversation_ref":null,"conclusion_ref":null,"discord_channel_id":null}`, `{"task_ref":"t","stage":"plan","change":"c","client_request_key":"k","summary":` + summary + `}`},
		{"actions", "action_propose", `{"action":"diagnose","arguments":{},"client_request_key":"k","discord_channel_id":""}`, `{"action":"diagnose","arguments":{},"client_request_key":"k"}`},
		{"actions", "action_history", `{"filters":null,"cursor":"","limit":0}`, `{}`},
		{"actions", "action_history", `{"filters":{"state":"failed"},"cursor":null,"limit":null}`, `{"filters":{"state":"failed"}}`},
	} {
		t.Run(tc.tool+tc.empty, func(t *testing.T) {
			parse := families[tc.family]
			want, err := parse(tc.tool, json.RawMessage(tc.omitted))
			if err != nil {
				t.Fatalf("omitted form refused: %v", err)
			}
			got, err := parse(tc.tool, json.RawMessage(tc.empty))
			if err != nil {
				t.Fatalf("empty optional members refused: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("empty form parsed to %+v, want omitted form %+v", got, want)
			}
		})
	}
}

// #1421 C4: emptiness never hides a malformed call, a repeated or unknown key,
// whitespace or an out-of-range value; required members stay strict.
func TestEmptyOptionalArgumentsKeepStrictRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ tool, raw string }{
		{"workflows_list", `{"project":"","project":"x"}`},
		{"workflows_list", `{"project":"x","project":""}`},
		{"workflows_list", `{"project":""} {}`},
		{"workflows_list", `[]`},
		{"workflows_list", `{"cursor":" "}`},
		{"workflows_list", `{"limit":51}`},
		{"workflows_list", `{"limit":-1}`},
		{"workflows_list", `{"owner_id":""}`},
		{"workflows_list", `{"Project":""}`},
		{"workflow_describe", `{"workflow_ref":"","version":"1"}`},
		{"workflow_continue_propose", `{"job":"j","session_revision":0,"answers":{"q":"a"},"client_request_key":"k"}`},
	} {
		if _, err := parseWorkflowArguments(tc.tool, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.tool, tc.raw)
		}
	}
	for _, raw := range []string{`{"task_ref":"t","section":"status","limit":0,"limit":1}`, `{"task_ref":"","section":"status"}`, `{"task_ref":"t","section":""}`, `{"task_ref":"t","section":"status","owner":""}`} {
		if _, err := parseEvidenceArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("evidence accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"scope":""}`, `{"scope":null}`, `{"scope":"factory","period":" "}`, `{"scope":"factory","limit":-1}`} {
		if _, err := parseUsageArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("usage accepted %s", raw)
		}
	}
	for _, tc := range []struct{ tool, raw string }{{"repo_read", `{"task_ref":""}`}, {"repo_grep", `{"task_ref":"t","pattern":""}`}, {"repo_diff", `{"task_ref":"t","mode":""}`}, {"repo_read", `{"cursor":"opaque","task_ref":"t"}`}, {"repo_read", `{"cursor":"","task_ref":""}`}} {
		if _, err := parseCodeArguments(tc.tool, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.tool, tc.raw)
		}
	}
	for _, tc := range []struct{ tool, raw string }{{"brainstorm_add", `{"brainstorm":"b","kind":"idea","text":"t","request_key":"k","expected_revision":null}`}, {"brainstorm_get", `{"brainstorm":""}`}} {
		if _, err := parseBrainstormArguments(tc.tool, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.tool, tc.raw)
		}
	}
	for _, tc := range []struct{ tool, raw string }{{"action_propose", `{"action":"diagnose","arguments":{},"client_request_key":""}`}, {"action_inspect", `{"proposal_id":""}`}, {"action_history", `{"filters":{"state":null}}`}} {
		if _, err := parseActionArguments(tc.tool, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("%s accepted %s", tc.tool, tc.raw)
		}
	}
}

// #1421 C2, C3, C4: through the handler, read-only tools given empty optional
// members reach their provider once; an empty required member is refused by
// name without reaching any provider.
func TestEmptyOptionalArgumentsReachProvidersThroughHandler(t *testing.T) {
	t.Parallel()
	r := newRefusalFixture(t)
	server, err := New(t.Context(), r.f.options)
	if err != nil {
		t.Fatal(err)
	}
	server.SetBrainstorm(refusalBrainstorms{calls: &r.calls})
	server.SetActions(refusalActions{calls: &r.calls})
	server.SetTaskDiscussion(refusalDiscussion{calls: &r.calls})
	server.SetWorkflows(refusalWorkflows{})
	r.handler = server.Handler()
	brainstormToken := r.token(t, r.target, BrainstormScope)
	usageToken := r.token(t, store.ConnectorGrantTarget{}, FactoryUsageScope)
	evidenceToken := r.token(t, r.target, DiscussionScope)
	codeToken := r.token(t, r.target, DiscussionScope+" "+CodeReviewScope)
	actionToken := r.token(t, store.ConnectorGrantTarget{}, StandardsManagementScope)
	// repo_read needs a recorded code target, which seeds its own repository.
	code := &refusalFixture{f: newConnectorFixture(t, nil)}
	codeTarget := seedCodeProtocolTarget(t, code.f.db)
	code.f.options.CodeEvidence = codeProtocolProvider(func(context.Context, store.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error) {
		r.calls.Add(1)
		return CodeEvidencePage{}, nil
	})
	code.handler = code.f.server(t)
	codeReadToken, _ := issueCodeProtocolTokens(t, code.f, registerConnectorClient(t, code.handler, "code client", "none"), codeTarget, code.f.now.Add(time.Hour))
	advisoryToken := r.token(t, r.target, AdvisoryScope)

	before := r.calls.Load()
	body, reply, log, _ := code.call(t, codeReadToken, "repo_read", `{"task_ref":"`+codeTarget.TaskRef+`","branch":"","path":"","start_line":0,"lines":0,"limit":0,"view_token":"","cursor":""}`)
	if reply.Error != nil || log.Outcome == "refusal" || r.calls.Load()-before != 1 {
		t.Fatalf("repo_read with empty optional members: %s log %+v calls %d", body, log, r.calls.Load()-before)
	}
	for _, tc := range []struct{ token, tool, args string }{
		{evidenceToken, "task_evidence_read", `{"task_ref":"` + r.target.TaskRef + `","section":"status","cursor":"","limit":0}`},
		{usageToken, "usage_read", `{"scope":"factory","repository":"","issue":0,"period":"","from":"","to":"","mode":"","agent":"","model":"","view":"","by":null,"limit":0,"cursor":""}`},
		{brainstormToken, "issue_search", `{"repository":"github:acme/api","text":"voice","limit":0}`},
		{actionToken, "action_history", `{"filters":null,"cursor":"","limit":0}`},
		{advisoryToken, "advisory_job_get", `{"job_ref":"job","cursor":""}`},
	} {
		before := r.calls.Load()
		body, reply, log, _ = r.call(t, tc.token, tc.tool, tc.args)
		if reply.Error != nil || log.Outcome == "refusal" {
			t.Fatalf("%s refused empty optional members: %s log %+v", tc.tool, body, log)
		}
		if n := r.calls.Load() - before; n != 1 {
			t.Fatalf("%s reached its provider %d times, want once: %s", tc.tool, n, body)
		}
	}

	before = r.calls.Load()
	for _, tc := range []struct{ token, tool, args, argument string }{
		{brainstormToken, "brainstorm_get", `{"brainstorm":""}`, "brainstorm"},
		{codeToken, "repo_read", `{"task_ref":null}`, "task_ref"},
		{actionToken, "workflow_describe", `{"workflow_ref":"","version":"1"}`, "workflow_ref"},
		{usageToken, "usage_read", `{"scope":""}`, "scope"},
		{evidenceToken, "task_evidence_read", `{"task_ref":"","section":"status"}`, "task_ref"},
		{actionToken, "action_propose", `{"action":"diagnose","arguments":{},"client_request_key":""}`, "client_request_key"},
		{actionToken, "workflows_list", `{"cursor":" "}`, "cursor"},
		{actionToken, "workflows_list", `{"limit":51}`, "limit"},
	} {
		body, reply, log, line := r.call(t, tc.token, tc.tool, tc.args)
		refused(t, tc.tool+" "+tc.args, body, reply, log, line, tc.tool, tc.argument)
	}
	if n := r.calls.Load() - before; n != 0 {
		t.Fatalf("refused calls reached providers %d times", n)
	}
}
