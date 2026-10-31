package connector

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// The journey crosses HTTP authentication, proposal/confirmation and durable job
// persistence. Only the domain provider is controlled: no external service runs.
type journeyWorkflowAdapter struct {
	calls        int
	repositoryID int64
}
type journeyWorkflowInput struct {
	Adapter      string
	Resolved     durableworkflow.Resolved
	Continuation durableworkflow.Continuation
}

func (a *journeyWorkflowAdapter) Resolve(_ context.Context, _ owneraction.Principal, d durableworkflow.Definition, r durableworkflow.Resolved, c durableworkflow.Continuation, _ time.Time) (owneraction.Facts, error) {
	b, _ := json.Marshal(journeyWorkflowInput{d.Adapter, r, c})
	repositoryID := int64(0)
	if r.Project != "" {
		repositoryID = a.repositoryID
	}
	return owneraction.Facts{RepositoryID: repositoryID, RepositoryRef: r.Project, TargetKind: "domain", TargetID: "saved-domain", TargetRevision: "1", Summary: "Controlled analysis and authoring", Payload: string(b)}, nil
}
func (*journeyWorkflowAdapter) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return nil
}
func (a *journeyWorkflowAdapter) Execute(_ context.Context, _ owneraction.Principal, f owneraction.Facts, _ string, _ time.Time) (durableworkflow.Output, error) {
	a.calls++
	var in journeyWorkflowInput
	if err := json.Unmarshal([]byte(f.Payload), &in); err != nil {
		return durableworkflow.Output{}, err
	}
	if in.Adapter == "log-analysis" {
		items := []map[string]any{{"finding": "Controlled finding"}}
		dryRun, ok := in.Resolved.Inputs["dry_run"].(bool)
		if !ok {
			return durableworkflow.Output{}, errors.New("missing typed dry-run")
		}
		if !dryRun {
			items = append(items, map[string]any{"issue_url": "https://github.com/example/repository/issues/7"})
		}
		return durableworkflow.Output{State: "succeeded", Summary: "Analysis saved", DomainRef: "analysis-saved", Items: items}, nil
	}
	if in.Continuation.Job == "" {
		return durableworkflow.Output{State: "waiting_for_input", Summary: "One author question", DomainRef: "author-saved", SessionRevision: 1, Questions: []durableworkflow.Question{{ID: "question-one", Text: "Which behavior should this standard require?"}}}, nil
	}
	return durableworkflow.Output{State: "succeeded", Summary: "Proposed standard saved", DomainRef: "author-saved", SessionRevision: 2, Items: []map[string]any{{"proposal": "Require bounded tests", "published": false}}}, nil
}
func (*journeyWorkflowAdapter) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (durableworkflow.Output, bool, error) {
	return durableworkflow.Output{}, false, nil
}

func TestDurableWorkflowMCPJourney(t *testing.T) {
	f := newConnectorFixture(t, nil)
	repo := registerTestRepo(t, f.db, "example", "repository")
	registry, err := durableworkflow.NewRegistry(durableworkflow.BuiltinDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	adapter := &journeyWorkflowAdapter{repositoryID: repo.ID}
	svc, err := durableworkflow.New(durableworkflow.Options{DB: f.db, Registry: registry, Adapters: map[string]durableworkflow.Adapter{"log-analysis": adapter, "standards-authoring": adapter}, Now: func() time.Time { return f.now }, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
	if err != nil {
		t.Fatal(err)
	}
	actions := owneraction.New(f.db, svc.WrapDomain(nil), func() time.Time { return f.now })
	svc.BindActions(actions)
	f.options.Actions = actions
	f.options.Workflows = svc
	j, _, csrf, cookie := consentPageFor(t, f, RepositoryActionsScope+" "+StandardsManagementScope+" "+DiscussionScope)
	tokens := exchangeConsent(t, j, postConsentResponse(t, j, csrf, cookie, url.Values{"scope": {RepositoryActionsScope, StandardsManagementScope, DiscussionScope}, "duration": {"1"}, "repositories": {"all"}, "action_repository": {"github:example/repository"}}))
	handler := f.server(t)
	call := func(name string, args map[string]any) map[string]any {
		t.Helper()
		response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, connectorBearer(tokens.AccessToken))
		var out struct {
			Error  any `json:"error"`
			Result struct {
				IsError bool           `json:"isError"`
				Data    map[string]any `json:"structuredContent"`
			} `json:"result"`
		}
		connectorDecode(t, response, &out)
		if out.Error != nil || out.Result.IsError || out.Result.Data == nil {
			t.Fatalf("%s: %s", name, response.Body.String())
		}
		return out.Result.Data
	}
	page := call("workflows_list", map[string]any{"project": "github:example/repository", "limit": 1})
	if len(journeyValue[[]any](t, page["workflows"])) != 1 || page["next_cursor"] == nil {
		t.Fatalf("first catalogue page: %+v", page)
	}
	next := call("workflows_list", map[string]any{"project": "github:example/repository", "limit": 1, "cursor": page["next_cursor"]})
	if len(journeyValue[[]any](t, next["workflows"])) != 1 {
		t.Fatalf("second catalogue page: %+v", next)
	}
	call("workflow_describe", map[string]any{"workflow_ref": "log-analysis", "version": "1", "project": "github:example/repository"})
	if adapter.calls != 0 {
		t.Fatal("discovery launched a provider")
	}
	finish := func(p map[string]any) string {
		t.Helper()
		job := journeyValue[string](t, p["job"])
		proposal := journeyValue[map[string]any](t, p["proposal"])
		id := journeyValue[string](t, proposal["proposal_id"])
		f.now = f.now.Add(6 * time.Second)
		confirmed := call("action_confirm", map[string]any{"proposal_id": id})
		if confirmed["state"] != "admitted" {
			t.Fatalf("confirmation: %+v", confirmed)
		}
		if _, err := actions.Step(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		if ran, err := svc.RunOnce(t.Context()); err != nil || !ran {
			t.Fatalf("worker: %v %v", ran, err)
		}
		return job
	}
	inputs := map[string]any{"after": "2026-10-30T00:00:00Z", "before": "2026-10-31T00:00:00Z", "dry_run": false}
	proposed := call("workflow_propose", map[string]any{"workflow_ref": "log-analysis", "version": "1", "project": "github:example/repository", "inputs": inputs, "client_request_key": "analysis-one"})
	if adapter.calls != 0 {
		t.Fatal("unconfirmed proposal launched a provider")
	}
	replay := call("workflow_propose", map[string]any{"workflow_ref": "log-analysis", "version": "1", "project": "github:example/repository", "inputs": inputs, "client_request_key": "analysis-one"})
	if replay["job"] != proposed["job"] {
		t.Fatal("retry replaced durable identity")
	}
	job := finish(proposed)
	// A newly composed HTTP handler models leaving the chat connection and returning.
	handler = f.server(t)
	saved := call("workflow_job_get", map[string]any{"job": job})
	b, _ := json.Marshal(saved)
	if !containsJourneyText(b, "https://github.com/example/repository/issues/7") {
		t.Fatalf("confirmed filing link missing: %s", b)
	}
	inputs["dry_run"] = true
	dry := finish(call("workflow_propose", map[string]any{"workflow_ref": "log-analysis", "version": "1", "project": "github:example/repository", "inputs": inputs, "client_request_key": "analysis-dry"}))
	drySaved := call("workflow_job_get", map[string]any{"job": dry})
	b, _ = json.Marshal(drySaved)
	if containsJourneyText(b, "issue_url") {
		t.Fatalf("dry-run filed an issue: %s", b)
	}
	author := finish(call("workflow_propose", map[string]any{"workflow_ref": "standards-authoring", "version": "1", "inputs": map[string]any{"intent": "Draft testing standards"}, "client_request_key": "author-one"}))
	questions := call("workflow_job_get", map[string]any{"job": author})
	b, _ = json.Marshal(questions)
	if !containsJourneyText(b, "question-one") {
		t.Fatalf("saved question missing: %s", b)
	}
	continued := call("workflow_continue_propose", map[string]any{"job": author, "session_revision": 1, "answers": map[string]string{"question-one": "Require bounded tests"}, "client_request_key": "answer-one"})
	if finish(continued) != author {
		t.Fatal("continuation replaced the job")
	}
	result := call("workflow_job_get", map[string]any{"job": author})
	b, _ = json.Marshal(result)
	if !containsJourneyText(b, "Require bounded tests") || !containsJourneyText(b, "author-saved") {
		t.Fatalf("saved author result missing: %s", b)
	}
	if adapter.calls != 4 {
		t.Fatalf("provider calls=%d, want four confirmed attempts", adapter.calls)
	}
}
func containsJourneyText(b []byte, text string) bool { return strings.Contains(string(b), text) }

func journeyValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("unexpected response field: %#v", value)
	}
	return result
}

func TestWorkflowArgumentsRejectDuplicateUnknownAndReplacementFacts(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"workflows_list", `{"project":"one","project":"two"}`},
		{"workflow_describe", `{"workflow_ref":"one","version":"1","owner_id":"42"}`},
		{"workflow_propose", `{"workflow_ref":"one","version":"1","inputs":{"intent":"one","intent":"two"},"client_request_key":"key"}`},
		{"workflow_propose", `{"workflow_ref":"one","version":"1","inputs":{},"client_request_key":"key","grant_id":"replacement"}`},
		{"workflow_continue_propose", `{"job":"job","session_revision":1,"answers":{"q":"one","q":"two"},"client_request_key":"key"}`},
		{"workflow_continue_propose", `{"job":"job","session_revision":1,"answers":{"q":"one"},"client_request_key":"key","project":"other"}`},
		{"workflow_job_get", `{"job":"job","limit":0}`},
		{"workflow_job_get", `{"job":"job","cursor":null}`},
		{"workflow_job_get", `{"job":"job"} {}`},
	} {
		t.Run(tc.name+tc.input, func(t *testing.T) {
			if _, err := parseWorkflowArguments(tc.name, json.RawMessage(tc.input)); err == nil {
				t.Fatal("unsafe workflow arguments accepted")
			}
		})
	}
}

func TestWorkflowGuidanceResourcesLoad(t *testing.T) {
	if _, err := loadGuidance(guidanceFiles, workflowToolNames); err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{}
	for _, name := range workflowToolNames {
		files["guidance/"+name+".md"] = &fstest.MapFile{Data: []byte("fixture guidance")}
	}
	if _, err := loadGuidance(files, workflowToolNames); err != nil {
		t.Fatal(err)
	}
	delete(files, "guidance/workflow_job_get.md")
	if _, err := loadGuidance(files, workflowToolNames); err == nil {
		t.Fatal("missing installed guidance accepted")
	}
}

func TestWorkflowArgumentsEnforceStringAndAnswerBounds(t *testing.T) {
	for _, raw := range []string{" ", "\n\t", `{"project":""}`, `{"cursor":" "}`} {
		if _, err := parseWorkflowArguments("workflows_list", json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid empty input accepted: %q", raw)
		}
	}
	for _, answers := range []map[string]string{{"q": strings.Repeat("x", 8193)}, {strings.Repeat("q", 129): "answer"}, {" ": "answer"}, {"q": " "}} {
		raw, err := json.Marshal(map[string]any{"job": "job", "session_revision": 1, "answers": answers, "client_request_key": "key"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseWorkflowArguments("workflow_continue_propose", raw); err == nil {
			t.Fatalf("out-of-contract answer accepted: %+v", answers)
		}
	}
	raw, err := json.Marshal(map[string]any{"job": "job", "session_revision": 1, "answers": map[string]string{strings.Repeat("q", 128): strings.Repeat("a", 8192)}, "client_request_key": "key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseWorkflowArguments("workflow_continue_propose", raw); err != nil {
		t.Fatalf("valid bound refused: %v", err)
	}
}
