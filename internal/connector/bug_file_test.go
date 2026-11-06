package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// bugFileContractDomain resolves facts only and publishes the real catalogue
// with bug_file enabled, so discovery shows what a factory would list.
type bugFileContractDomain struct{ actionContractDomain }

func (bugFileContractDomain) ActionCatalog() []owneraction.ActionDefinition {
	return owneractions.Availability{BugFile: true, Answer: true}.ActionCatalog()
}

func bugFileFixture(t *testing.T, name string) (*connectorFixture, http.Handler, string) {
	t.Helper()
	f := newConnectorFixture(t, nil)
	repo := registerTestRepo(t, f.db, "owner", name)
	f.options.Actions = owneraction.New(f.db, bugFileContractDomain{actionContractDomain{repo: repo}}, func() time.Time { return f.now })
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "bug client", "none")
	tokens, _ := issueActionContractTokens(t, f, handler, client, repo)
	return f, handler, tokens.AccessToken
}

func bugFileArgs(key, title string) map[string]any {
	return map[string]any{"repository": " owner/bugs ", "title": title, "description": "Expected a page.\nGot an error.", "client_request_key": key}
}

func bugFileAsAction(args map[string]any) map[string]any {
	return map[string]any{"action": "bug_file", "arguments": map[string]any{"repository": args["repository"], "title": args["title"], "description": args["description"]}, "client_request_key": args["client_request_key"]}
}

func TestBugFileProposeListedWithStrictSchema(t *testing.T) {
	f, handler, token := bugFileFixture(t, "bugs")
	listed := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, connectorBearer(token))
	var out struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	connectorDecode(t, listed, &out)
	schemas := map[string]json.RawMessage{}
	for _, tool := range out.Result.Tools {
		schemas[tool.Name] = tool.InputSchema
	}
	raw, ok := schemas["bug_file_propose"]
	if !ok {
		t.Fatalf("bug_file_propose not listed: %s", listed.Body.String())
	}
	type field struct {
		Type      string `json:"type"`
		MinLength int    `json:"minLength"`
		MaxLength int    `json:"maxLength"`
	}
	var schema struct {
		Type                 string           `json:"type"`
		Properties           map[string]field `json:"properties"`
		Required             []string         `json:"required"`
		AdditionalProperties *bool            `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"repository": 1024, "title": 256, "description": 8192, "client_request_key": 128}
	if schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties || len(schema.Properties) != len(want) {
		t.Fatalf("schema shape: %s", raw)
	}
	for name, maximum := range want {
		if p := schema.Properties[name]; p.Type != "string" || p.MinLength != 1 || p.MaxLength != maximum {
			t.Fatalf("%s bounds: %+v", name, p)
		}
	}
	required := slices.Clone(schema.Required)
	slices.Sort(required)
	if !slices.Equal(required, []string{"client_request_key", "description", "repository", "title"}) {
		t.Fatalf("required: %v", schema.Required)
	}

	var propose struct {
		OneOf []struct {
			Properties struct {
				Action struct {
					Const string `json:"const"`
				} `json:"action"`
				Arguments struct {
					Properties           map[string]json.RawMessage `json:"properties"`
					Required             []string                   `json:"required"`
					AdditionalProperties *bool                      `json:"additionalProperties"`
				} `json:"arguments"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if err := json.Unmarshal(schemas["action_propose"], &propose); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, variant := range propose.OneOf {
		if variant.Properties.Action.Const != "bug_file" {
			continue
		}
		found = true
		arguments := variant.Properties.Arguments
		names := slices.Clone(arguments.Required)
		slices.Sort(names)
		if len(arguments.Properties) != 3 || !slices.Equal(names, []string{"description", "repository", "title"}) || arguments.AdditionalProperties == nil || *arguments.AdditionalProperties {
			t.Fatalf("action_propose bug_file variant: %+v", arguments)
		}
	}
	if !found {
		t.Fatalf("action_propose catalogue lacks bug_file: %s", schemas["action_propose"])
	}
}

func TestBugFileProposeNotListedForReadGrant(t *testing.T) {
	f := newConnectorFixture(t, nil)
	f.options.Actions = owneraction.New(f.db, nil, func() time.Time { return f.now })
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "read client", "none")
	token, _ := issueCoverageTokens(t, f, client, Scope, nil, f.now.Add(time.Hour))
	listed := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, connectorBearer(token))
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "bug_file_propose") {
		t.Fatalf("read grant exposed bug_file_propose: %d %s", listed.Code, listed.Body.String())
	}
	if names := (&Server{}).availableTools(store.ConnectorGrant{Scope: RepositoryActionsScope}); slices.Contains(names, "bug_file_propose") {
		t.Fatalf("listed without the action service: %v", names)
	}
}

func TestBugFileProposeRefusesArgumentsOutsideItsContract(t *testing.T) {
	long := strings.Repeat("é", 257)
	for name, raw := range map[string]string{
		"unknown field":   `{"repository":"r","title":"t","description":"d","client_request_key":"k","discord_channel_id":"1"}`,
		"missing title":   `{"repository":"r","description":"d","client_request_key":"k"}`,
		"missing key":     `{"repository":"r","title":"t","description":"d"}`,
		"blank title":     `{"repository":"r","title":"  ","description":"d","client_request_key":"k"}`,
		"null field":      `{"repository":null,"title":"t","description":"d","client_request_key":"k"}`,
		"number title":    `{"repository":"r","title":7,"description":"d","client_request_key":"k"}`,
		"long title":      `{"repository":"r","title":"` + long + `","description":"d","client_request_key":"k"}`,
		"long body":       `{"repository":"r","title":"t","description":"` + strings.Repeat("x", 8193) + `","client_request_key":"k"}`,
		"long repository": `{"repository":"` + strings.Repeat("r", 1025) + `","title":"t","description":"d","client_request_key":"k"}`,
		"long key":        `{"repository":"r","title":"t","description":"d","client_request_key":"` + strings.Repeat("k", 129) + `"}`,
		"duplicate field": `{"repository":"r","title":"t","title":"u","description":"d","client_request_key":"k"}`,
		"not an object":   `["r"]`,
	} {
		if _, err := parseActionArguments("bug_file_propose", json.RawMessage(raw)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	a, err := parseActionArguments("bug_file_propose", json.RawMessage(`{"repository":" r ","title":"`+strings.Repeat("é", 256)+`","description":"`+strings.Repeat("x", 8192)+`","client_request_key":"k"}`))
	if err != nil || a.Action != "bug_file" || a.Arguments["repository"] != " r " || len(a.Arguments) != 3 || a.DiscordChannelID != "" || a.ClientRequestKey != "k" {
		t.Fatalf("bounded input refused or changed: %+v %v", a, err)
	}

	f, handler, token := bugFileFixture(t, "strict")
	response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "bug_file_propose", "arguments": map[string]any{"repository": "r", "title": "t", "description": "d", "client_request_key": "k", "labels": "bug"}}}, connectorBearer(token))
	if !strings.Contains(response.Body.String(), "Invalid params") {
		t.Fatalf("unknown field accepted over MCP: %s", response.Body.String())
	}
}

func TestBugFileProposeSharesOneProposalWithActionPropose(t *testing.T) {
	f, handler, token := bugFileFixture(t, "routes")
	dedicated := bugFileArgs("one", "Login fails")
	first := actionContractCall(t, f, handler, token, "bug_file_propose", dedicated)
	if first.Error != nil || first.Result.IsError || first.Result.Proposal.ID == "" {
		t.Fatalf("dedicated proposal: %+v", first)
	}
	assertActionText(t, first, "action_confirm")
	facts := first.Result.Proposal.Facts.Invocation
	if facts.Command != "bug_file" || len(facts.Arguments) != 3 || facts.Arguments["repository"] != " owner/bugs " || facts.Arguments["title"] != "Login fails" {
		t.Fatalf("invocation changed by the tool: %+v", facts)
	}
	if first.Result.Proposal.Principal.ChannelID != "" {
		t.Fatalf("dedicated route set a channel: %+v", first.Result.Proposal.Principal)
	}
	viaAction := actionContractCall(t, f, handler, token, "action_propose", bugFileAsAction(dedicated))
	if viaAction.Result.IsError || viaAction.Result.Proposal.ID != first.Result.Proposal.ID {
		t.Fatalf("action_propose made another proposal: %+v", viaAction)
	}
	again := actionContractCall(t, f, handler, token, "bug_file_propose", dedicated)
	if again.Result.IsError || again.Result.Proposal.ID != first.Result.Proposal.ID {
		t.Fatalf("retry made another proposal: %+v", again)
	}

	second := actionContractCall(t, f, handler, token, "action_propose", bugFileAsAction(bugFileArgs("two", "Login fails")))
	if second.Result.IsError || second.Result.Proposal.ID == first.Result.Proposal.ID {
		t.Fatalf("new key did not make a new proposal: %+v", second)
	}
	if back := actionContractCall(t, f, handler, token, "bug_file_propose", bugFileArgs("two", "Login fails")); back.Result.IsError || back.Result.Proposal.ID != second.Result.Proposal.ID {
		t.Fatalf("dedicated retry of an action_propose request: %+v", back)
	}

	for name, args := range map[string]map[string]any{
		"bug_file_propose": bugFileArgs("one", "Login fails badly"),
		"action_propose":   bugFileAsAction(bugFileArgs("one", "Login fails badly")),
	} {
		changed := actionContractCall(t, f, handler, token, name, args)
		if !changed.Result.IsError || changed.Result.Proposal.ID != "" || len(changed.Result.Content) != 1 || changed.Result.Content[0].Text != "Request key already belongs to different action facts" {
			t.Fatalf("%s accepted changed text under the same key: %+v", name, changed)
		}
	}
}

func TestBugFileProposeConcurrentRoutesGiveOneProposal(t *testing.T) {
	f, handler, token := bugFileFixture(t, "race")
	args := bugFileArgs("same", "Crash on save")
	var wg sync.WaitGroup
	ids := make([]string, 6)
	bodies := make([]string, len(ids))
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name, call := "bug_file_propose", args
			if i%2 == 1 {
				name, call = "action_propose", bugFileAsAction(args)
			}
			response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": call}}, connectorBearer(token))
			bodies[i] = response.Body.String()
			var out actionContractResponse
			if json.Unmarshal(response.Body.Bytes(), &out) == nil && response.Code == http.StatusOK && !out.Result.IsError {
				ids[i] = out.Result.Proposal.ID
			}
		}()
	}
	wg.Wait()
	for i, id := range ids {
		if id == "" || id != ids[0] {
			t.Fatalf("call %d: %q vs %q: %s", i, id, ids[0], bodies[i])
		}
	}
	history := actionContractCall(t, f, handler, token, "action_history", map[string]any{"limit": 10})
	var page owneraction.Page
	if err := json.Unmarshal([]byte(history.Result.Content[0].Text), &page); err != nil || len(page.Proposals) != 1 || page.Proposals[0].ID != ids[0] {
		t.Fatalf("want one proposal in history: %v %s", err, history.Result.Content[0].Text)
	}
}

func TestBugFileRefusalReasonReachesClient(t *testing.T) {
	got := actionFailure(fmt.Errorf("resolve: %w", owneraction.Refusal{Reason: "Filing a bug on Linear is not supported yet"}))
	if !got.IsError || got.Content[0].Text != "Filing a bug on Linear is not supported yet" {
		t.Fatalf("refusal = %+v", got)
	}
	if other := actionFailure(errors.New("hidden detail")); other.Content[0].Text != "Action unavailable" {
		t.Fatalf("plain errors must stay hidden: %+v", other)
	}
}
