package connector

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestActionArgumentsRejectAuthorityAndReplacementFacts(t *testing.T) {
	tests := []struct{ name, input string }{
		{"action_propose", `{"action":"diagnose","arguments":{},"client_request_key":"one","owner_id":"42"}`},
		{"action_propose", `{"action":"diagnose","action":"ship","arguments":{},"client_request_key":"one"}`},
		{"action_propose", `{"action":"diagnose","arguments":{"repo":"one","repo":"two"},"client_request_key":"one"}`},
		{"action_propose", `{"action":"diagnose","arguments":{"repo":null},"client_request_key":"one"}`},
		{"action_propose", `{"action":"diagnose","arguments":null,"client_request_key":"one"}`},
		{"action_propose", `{"action":"diagnose","arguments":{},"client_request_key":""}`},
		{"action_confirm", `{"proposal_id":"one","arguments":{"repo":"other"}}`},
		{"action_cancel", `{"proposal_id":"one","grant_id":"replacement"}`},
		{"action_inspect", `{"proposal_id":null}`},
		{"action_history", `{"limit":0}`},
		{"action_history", `{"filters":{"owner":"other"}}`},
		{"action_history", `{"filters":{"action":"one","Action":"two"}}`},
		{"action_history", `{"filters":{"state":null}}`},
		{"action_history", `{"filters":{"state":"failed","state":"succeeded"}}`},
		{"action_history", `{"limit":51}`},
		{"action_history", `{"limit":1,"cursor":null}`},
		{"action_history", `{"limit":1} {}`},
	}
	for _, tc := range tests {
		t.Run(tc.name+tc.input, func(t *testing.T) {
			if _, err := parseActionArguments(tc.name, json.RawMessage(tc.input)); err == nil {
				t.Fatal("unsafe arguments accepted")
			}
		})
	}
	if _, err := parseActionArguments("action_propose", json.RawMessage(`{"action":"diagnose","arguments":{"text":"`+strings.Repeat("x", 8193)+`"},"client_request_key":"one"}`)); err == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestActionArgumentsPreserveClientCarriedIdentifiers(t *testing.T) {
	a, err := parseActionArguments("action_propose", json.RawMessage(`{"action":"diagnose","arguments":{"repo":"github:owner/repo"},"client_request_key":"one","discord_channel_id":"123"}`))
	if err != nil || a.Action != "diagnose" || a.Arguments["repo"] != "github:owner/repo" || a.ClientRequestKey != "one" || a.DiscordChannelID != "123" {
		t.Fatalf("proposal: %+v %v", a, err)
	}
	for _, name := range []string{"action_confirm", "action_cancel", "action_inspect"} {
		a, err = parseActionArguments(name, json.RawMessage(`{"proposal_id":"client-carried"}`))
		if err != nil || a.ProposalID != "client-carried" {
			t.Fatalf("%s: %+v %v", name, a, err)
		}
	}
	a, err = parseActionArguments("action_history", json.RawMessage(`{}`))
	if err != nil || a.Limit != 50 {
		t.Fatalf("history: %+v %v", a, err)
	}
}

func TestActionConsentIsSeparateFromReadSelection(t *testing.T) {
	form := url.Values{"repositories": {"all"}, "repository": {"github:owner/read"}}
	if _, reason := consentActionCoverage(form); reason == "" {
		t.Fatal("read selection became action consent")
	}
	form["action_repository"] = []string{"github:owner/action"}
	got, reason := consentActionCoverage(form)
	if reason != "" || got.All || len(got.Repositories) != 1 || got.Repositories[0].RepositoryRef != "github:owner/action" {
		t.Fatalf("action coverage: %+v %s", got, reason)
	}
	read, reason := consentCoverage(form, DiscussionScope)
	if reason != "" || !read.All {
		t.Fatalf("read coverage changed: %+v %s", read, reason)
	}
	if actionGrant(store.ConnectorGrant{Scope: DiscussionScope + " " + CodeReviewScope, Coverage: &store.ConnectorCoverage{All: true}}) {
		t.Fatal("old read grant gained action authority")
	}
	if !actionGrant(store.ConnectorGrant{Scope: StandardsManagementScope}) {
		t.Fatal("standards consent missing")
	}
}

// This domain only resolves facts; this boundary test never admits effects.
type actionContractDomain struct{ repo store.Repo }

func (d actionContractDomain) Resolve(_ context.Context, _ owneraction.Principal, inv owneraction.Invocation, _ time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{Invocation: inv, RepositoryID: d.repo.ID, RepositoryRef: "github:" + d.repo.Owner + "/" + d.repo.Name, TargetKind: "repository", TargetID: strconv.FormatInt(d.repo.ID, 10), TargetRevision: "reviewed-revision", Summary: "Diagnose repository", Estimate: "unknown", Payload: `{}`}, nil
}
func (actionContractDomain) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return nil
}
func (actionContractDomain) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	return owneraction.Result{}, errors.New("unexpected effect")
}
func (actionContractDomain) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, bool, error) {
	return owneraction.Result{}, false, nil
}

func TestActionMCPUsesTrustedGrantAndFrozenFacts(t *testing.T) {
	f := newConnectorFixture(t, nil)
	repo := registerTestRepo(t, f.db, "owner", "action")
	f.options.Actions = owneraction.New(f.db, actionContractDomain{repo: repo}, func() time.Time { return f.now })
	j, _, csrf, cookie := consentPageFor(t, f, RepositoryActionsScope)
	refused := postConsentResponse(t, j, csrf, cookie, url.Values{"scope": {RepositoryActionsScope}, "duration": {"1"}, "repositories": {"all"}})
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("all-read became action consent: %d", refused.Code)
	}
	tokens := exchangeConsent(t, j, postConsentResponse(t, j, csrf, cookie, url.Values{"scope": {RepositoryActionsScope}, "duration": {"1"}, "action_repository": {"github:owner/action"}}))
	handler := f.server(t)
	call := func(name string, args map[string]any) *httptest.ResponseRecorder {
		return connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, connectorBearer(tokens.AccessToken))
	}
	response := call("action_propose", map[string]any{"action": "diagnose", "arguments": map[string]string{"repo": "github:owner/action"}, "client_request_key": "one"})
	var out struct {
		Result struct {
			IsError  bool                 `json:"isError"`
			Proposal owneraction.Proposal `json:"structuredContent"`
		} `json:"result"`
	}
	connectorDecode(t, response, &out)
	if response.Code != http.StatusOK || out.Result.IsError || out.Result.Proposal.ID == "" {
		t.Fatalf("proposal: %d %s", response.Code, response.Body.String())
	}
	proposal := out.Result.Proposal
	grants, err := f.db.ListConnectorGrants(t.Context(), 42, f.now)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants: %+v %v", grants, err)
	}
	if proposal.Principal.OwnerID != "42" || proposal.Principal.ClientID != grants[0].ClientID || proposal.Principal.GrantID != grants[0].ID || proposal.Principal.Transport != "mcp" || proposal.Facts.TargetRevision != "reviewed-revision" || proposal.State != "awaiting_confirmation" {
		t.Fatalf("facts or trusted origin lost: %+v", proposal)
	}
	replay := call("action_propose", map[string]any{"action": "diagnose", "arguments": map[string]string{"repo": "github:owner/action"}, "client_request_key": "one"})
	connectorDecode(t, replay, &out)
	if out.Result.Proposal.ID != proposal.ID {
		t.Fatal("lost-response retry made another proposal")
	}
	response = call("action_confirm", map[string]any{"proposal_id": proposal.ID})
	connectorDecode(t, response, &out)
	if out.Result.IsError || out.Result.Proposal.Reason != "too_early" || out.Result.Proposal.State != "awaiting_confirmation" {
		t.Fatalf("early confirmation: %s", response.Body.String())
	}
	response = call("action_inspect", map[string]any{"proposal_id": proposal.ID})
	connectorDecode(t, response, &out)
	if out.Result.IsError || out.Result.Proposal.ID != proposal.ID {
		t.Fatalf("inspection: %s", response.Body.String())
	}
	for _, args := range []map[string]any{{"proposal_id": proposal.ID, "owner_id": "42"}, {"proposal_id": proposal.ID, "arguments": map[string]string{"repo": "other"}}} {
		response = call("action_confirm", args)
		if !strings.Contains(response.Body.String(), "Invalid params") {
			t.Fatalf("authority/replacement field accepted: %s", response.Body.String())
		}
	}
	response = call("action_propose", map[string]any{"action": "arbitrary-shell", "arguments": map[string]string{}, "client_request_key": "unknown"})
	connectorDecode(t, response, &out)
	if !out.Result.IsError {
		t.Fatalf("unknown action accepted: %s", response.Body.String())
	}
}

func TestReadGrantCannotDiscoverOrCallActions(t *testing.T) {
	f := newConnectorFixture(t, nil)
	f.options.Actions = owneraction.New(f.db, nil, func() time.Time { return f.now })
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "read client", "none")
	token, _ := issueCoverageTokens(t, f, client, Scope, nil, f.now.Add(time.Hour))
	listed := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, connectorBearer(token))
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "action_propose") {
		t.Fatalf("read grant exposed actions: %d %s", listed.Code, listed.Body.String())
	}
	called := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "action_propose", "arguments": map[string]any{"action": "diagnose", "arguments": map[string]string{}, "client_request_key": "one"}}}, connectorBearer(token))
	if called.Code != http.StatusForbidden {
		t.Fatalf("old read grant action call: %d %s", called.Code, called.Body.String())
	}
	if ping := f.ping(t, handler, token); ping.Code != http.StatusOK {
		t.Fatalf("read grant stopped working: %d", ping.Code)
	}
}

// issueActionContractTokens exercises fresh consent and the real OAuth exchange
// while keeping each boundary test independent of browser presentation.
func issueActionContractTokens(t *testing.T, f *connectorFixture, handler http.Handler, client connectorTestClient, repo store.Repo) (connectorTestTokens, string) {
	t.Helper()
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomCredential()
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 43)
	b := store.ConnectorBrowser{ID: id, ClientID: client.ClientID, RedirectURI: "https://client.example/callback", Resource: f.options.Config.ResourceURL, Scope: RepositoryActionsScope, PKCEChallenge: digest(verifier), NonceDigest: digest(id + "nonce"), UpstreamStateDigest: digest(id + "upstream"), OwnerGeneration: owner.Generation, ExpiresAt: f.now.Add(10 * time.Minute)}
	if err = f.db.CreateConnectorBrowser(t.Context(), b, 42, f.now); err != nil {
		t.Fatal(err)
	}
	if err = f.db.VerifyConnectorBrowser(t.Context(), b.ID, b.NonceDigest, b.UpstreamStateDigest, 42, f.now); err != nil {
		t.Fatal(err)
	}
	g := store.ConnectorGrant{ID: id, ClientID: client.ClientID, Resource: b.Resource, Scope: b.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: f.now, ExpiresAt: f.now.Add(time.Hour), ActionCoverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repo.ID, RepositoryRef: "github:" + repo.Owner + "/" + repo.Name}}}}
	c := store.ConnectorCode{Digest: digest(id + "code"), GrantID: id, ClientID: client.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: f.now.Add(5 * time.Minute)}
	if err = f.db.ConsentConnectorBrowser(t.Context(), b.ID, b.NonceDigest, 42, f.now, g, c, true); err != nil {
		t.Fatal(err)
	}
	response := connectorForm(t, handler, f.issuer+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ClientID}, "redirect_uri": {b.RedirectURI}, "resource": {b.Resource}, "code": {id + "code"}, "code_verifier": {verifier}}, nil)
	var tokens connectorTestTokens
	connectorDecode(t, response, &tokens)
	if response.Code != http.StatusOK || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("action token exchange: %d %s", response.Code, response.Body.String())
	}
	return tokens, id
}

type actionContractResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Result struct {
		IsError  bool                 `json:"isError"`
		Proposal owneraction.Proposal `json:"structuredContent"`
	} `json:"result"`
}

func actionContractCall(t *testing.T, f *connectorFixture, handler http.Handler, token, name string, args map[string]any) actionContractResponse {
	t.Helper()
	response := connectorJSON(t, handler, f.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, connectorBearer(token))
	if response.Code != http.StatusOK {
		t.Fatalf("%s HTTP %d: %s", name, response.Code, response.Body.String())
	}
	var out actionContractResponse
	connectorDecode(t, response, &out)
	return out
}

type actionContractDelivery struct{ calls *atomic.Int32 }

func (d actionContractDelivery) Send(_ context.Context, p owneraction.Proposal) (string, error) {
	d.calls.Add(1)
	if p.Tier != owneraction.Binding || p.Facts.TargetRevision != "reviewed-revision" || p.ApprovalChannel != "channel" {
		return "", errors.New("approval facts changed")
	}
	return "approval-message", nil
}

type actionBindingContractDomain struct {
	actionContractDomain
	effects *atomic.Int32
}

func (d actionBindingContractDomain) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	d.effects.Add(1)
	return owneraction.Result{}, errors.New("unexpected effect")
}

func TestActionMCPBindingRefusesUntrustedReactionsAndRevokedOrigin(t *testing.T) {
	f := newConnectorFixture(t, nil)
	repo := registerTestRepo(t, f.db, "owner", "binding")
	var effects, sends atomic.Int32
	actions := owneraction.New(f.db, actionBindingContractDomain{actionContractDomain: actionContractDomain{repo: repo}, effects: &effects}, func() time.Time { return f.now })
	actions.SetDiscord("discord-owner", []string{"channel"})
	actions.SetDelivery(actionContractDelivery{calls: &sends})
	f.options.Actions = actions
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "binding client", "none")
	tokens, grantID := issueActionContractTokens(t, f, handler, client, repo)
	proposed := actionContractCall(t, f, handler, tokens.AccessToken, "action_propose", map[string]any{"action": "confirm", "arguments": map[string]string{}, "client_request_key": "binding"})
	if proposed.Error != nil || proposed.Result.IsError || proposed.Result.Proposal.Tier != owneraction.Binding {
		t.Fatalf("binding proposal: %+v", proposed)
	}
	id := proposed.Result.Proposal.ID
	f.now = f.now.Add(5 * time.Second)
	confirmed := actionContractCall(t, f, handler, tokens.AccessToken, "action_confirm", map[string]any{"proposal_id": id})
	if confirmed.Error != nil || confirmed.Result.IsError || confirmed.Result.Proposal.State != "awaiting_discord" || effects.Load() != 0 || sends.Load() != 0 {
		t.Fatalf("confirmation admitted or delivered synchronously: %+v", confirmed)
	}
	delivered, err := actions.Step(t.Context(), id)
	if err != nil || delivered.State != "awaiting_discord" || delivered.ApprovalMessage != "approval-message" || sends.Load() != 1 {
		t.Fatalf("durable delivery: %+v %v", delivered, err)
	}
	base := owneraction.Reaction{ChannelID: "channel", MessageID: "approval-message", UserID: "discord-owner", Emoji: "✅"}
	for _, kind := range []string{"other-user", "bot", "other-message", "other-channel", "other-emoji", "custom-emoji"} {
		bad := base
		switch kind {
		case "other-user":
			bad.UserID = "outsider"
		case "bot":
			bad.Bot = true
		case "other-message":
			bad.MessageID = "unbound-message"
		case "other-channel":
			bad.ChannelID = "unbound-channel"
		case "other-emoji":
			bad.Emoji = "☑️"
		case "custom-emoji":
			bad.EmojiID = "custom"
		}
		if _, err = actions.Reaction(t.Context(), bad); err == nil {
			t.Fatalf("%s reaction authorized", kind)
		}
	}
	inspected := actionContractCall(t, f, handler, tokens.AccessToken, "action_inspect", map[string]any{"proposal_id": id})
	if inspected.Result.IsError || inspected.Result.Proposal.State != "awaiting_discord" || effects.Load() != 0 {
		t.Fatalf("invalid reaction changed proposal: %+v", inspected)
	}
	if err = f.db.RevokeConnectorGrant(t.Context(), 42, grantID, f.now); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = actions.Reaction(t.Context(), base); !errors.Is(err, store.ErrConnectorInvalid) {
			t.Fatalf("revoked origin reaction: %v", err)
		}
	}
	if _, err = actions.Step(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	record, err := f.db.ReadOwnerAction(t.Context(), id)
	if err != nil || record.State != "awaiting_discord" || effects.Load() != 0 || sends.Load() != 1 {
		t.Fatalf("revocation or replay admitted effects: %+v effects=%d sends=%d err=%v", record, effects.Load(), sends.Load(), err)
	}
}

func TestActionMCPRefreshKeepsOriginalGrantReplacementCannotConfirm(t *testing.T) {
	f := newConnectorFixture(t, nil)
	repo := registerTestRepo(t, f.db, "owner", "refresh")
	f.options.Actions = owneraction.New(f.db, actionContractDomain{repo: repo}, func() time.Time { return f.now })
	handler := f.server(t)
	client := registerConnectorClient(t, handler, "same client", "none")
	original, grantID := issueActionContractTokens(t, f, handler, client, repo)
	proposed := actionContractCall(t, f, handler, original.AccessToken, "action_propose", map[string]any{"action": "diagnose", "arguments": map[string]string{}, "client_request_key": "refresh"})
	if proposed.Error != nil || proposed.Result.IsError {
		t.Fatalf("proposal: %+v", proposed)
	}
	id := proposed.Result.Proposal.ID
	f.now = f.now.Add(5 * time.Second)
	replacement, replacementID := issueActionContractTokens(t, f, handler, client, repo)
	if replacementID == grantID {
		t.Fatal("replacement reused grant identity")
	}
	denied := actionContractCall(t, f, handler, replacement.AccessToken, "action_confirm", map[string]any{"proposal_id": id})
	if !denied.Result.IsError || denied.Result.Proposal.ID != "" {
		t.Fatalf("replacement grant adopted original proposal: %+v", denied)
	}
	response := connectorForm(t, handler, f.issuer+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ClientID}, "resource": {f.options.Config.ResourceURL}, "refresh_token": {original.RefreshToken}}, nil)
	var refreshed connectorTestTokens
	connectorDecode(t, response, &refreshed)
	if response.Code != http.StatusOK || refreshed.AccessToken == "" {
		t.Fatalf("refresh: %d %s", response.Code, response.Body.String())
	}
	confirmed := actionContractCall(t, f, handler, refreshed.AccessToken, "action_confirm", map[string]any{"proposal_id": id})
	if confirmed.Error != nil || confirmed.Result.IsError || confirmed.Result.Proposal.State != "admitted" || confirmed.Result.Proposal.Principal.GrantID != grantID {
		t.Fatalf("same-grant refresh confirmation: %+v", confirmed)
	}
}

func TestActionServicePublicationIsSafeDuringDiscovery(t *testing.T) {
	f := newConnectorFixture(t, nil)
	server, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	actions := owneraction.New(f.db, nil, func() time.Time { return f.now })
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for range 1000 {
			server.SetActions(actions)
			server.SetActions(nil)
		}
	}()
	go func() {
		defer workers.Done()
		for range 1000 {
			names := server.availableTools(store.ConnectorGrant{Scope: RepositoryActionsScope})
			if len(names) != 0 && len(names) != len(actionToolNames) {
				t.Errorf("partial service publication: %v", names)
				return
			}
			if schema := server.actionSchema("action_propose"); schema["type"] != "object" {
				t.Errorf("unavailable schema during publication: %v", schema)
				return
			}
		}
	}()
	workers.Wait()
	server.SetActions(actions)
	if names := server.availableTools(store.ConnectorGrant{Scope: RepositoryActionsScope}); !slices.Equal(names, actionToolNames) {
		t.Fatalf("published actions missing: %v", names)
	}
}
