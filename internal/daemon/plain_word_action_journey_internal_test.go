package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// The controlled clients interpret ordinary owner words and carry machine
// references themselves. Real domain handlers, HTTP transports and persistence
// enforce the policy; this fixture makes no live model or Discord claim.
type plainJourneyProvider struct{}

func (plainJourneyProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	session := r.Session
	session.Resume = "plain-word-session"
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	var prompt struct {
		OwnerSources []messaging.DialogueTurn `json:"owner_sources"`
	}
	if err := json.Unmarshal([]byte(r.Prompt), &prompt); err != nil {
		return agent.ConversationResult{}, err
	}
	if len(prompt.OwnerSources) == 0 {
		return agent.ConversationResult{}, fmt.Errorf("owner words absent")
	}
	source := prompt.OwnerSources[len(prompt.OwnerSources)-1]
	envelope := messaging.ConversationEnvelope{Version: 1, Kind: "propose", Command: "standards-author"}
	switch source.Text {
	case "Start a standard about bounded checks in quality.":
		envelope.Arguments = []messaging.ConversationArgument{{Name: "action", Value: "start"}}
		for name, value := range map[string]string{"intent": "bounded checks", "category_ids": "quality"} {
			start := strings.Index(source.Text, value)
			envelope.Arguments = append(envelope.Arguments, messaging.ConversationArgument{Name: name, SourceID: source.SourceID, Start: start, End: start + len(value)})
		}
	case "Publish that standard.":
		envelope.Arguments = []messaging.ConversationArgument{{Name: "action", Value: "publish"}, {Name: "text", SourceID: source.SourceID, Start: 0, End: len(source.Text)}}
	default:
		return agent.ConversationResult{}, fmt.Errorf("unexpected model request for %q", source.Text)
	}
	raw, err := json.Marshal(envelope)
	return agent.ConversationResult{Answer: raw, Session: session}, err
}

type plainJourneyAuthor struct{ calls int }

func (a *plainJourneyAuthor) Answer(context.Context, standards.AuthoringCall) ([]byte, error) {
	a.calls++
	return []byte(`{"schema_version":1,"category_id":"quality","rule":"MUST bound checks","description":"Use finite test timeouts.","evidence":"Test commands have finite timeouts.","scope":"All repositories.","check":"manual","applies_to":["story-worker"],"questions":[]}`), nil
}

type plainJourneyControl struct {
	db  *store.DB
	now func() time.Time
}

func (plainJourneyControl) Ready(context.Context) error { return nil }
func (c plainJourneyControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.db.ListConnectorGrants(ctx, 42, c.now())
}
func (c plainJourneyControl) Revoke(ctx context.Context, id string) error {
	return c.db.RevokeConnectorGrant(ctx, 42, id, c.now())
}
func (c plainJourneyControl) RevokeAll(ctx context.Context) error {
	return c.db.RevokeAllConnectorGrants(ctx, 42, c.now())
}

func plainJourneyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func plainJourneyToken(t *testing.T, db *store.DB, resource string, at time.Time) string {
	t.Helper()
	ctx := t.Context()
	owner, err := db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	client := store.ConnectorClient{ID: "journey-client", Name: "Owner's client", AuthMethod: "none", Redirects: []string{"https://client.example/callback"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	if err = db.RegisterConnectorClient(ctx, client, at); err != nil {
		t.Fatal(err)
	}
	browser := store.ConnectorBrowser{ID: "journey-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: resource, Scope: connector.StandardsManagementScope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: at.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(ctx, browser, 42, at); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, 42, at); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: "journey-grant", ClientID: client.ID, Resource: resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	code := store.ConnectorCode{Digest: "journey-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	if err = db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, 42, at, grant, code, true); err != nil {
		t.Fatal(err)
	}
	access := "controlled-access-token"
	_, err = db.ExchangeConnectorCode(ctx, store.ConnectorExchange{Digest: code.Digest, ClientID: client.ID, RedirectURI: code.RedirectURI, Resource: resource, Scope: code.Scope, PKCEChallenge: code.PKCEChallenge, OwnerID: 42, Now: at, Pair: store.ConnectorTokenPair{AccessDigest: plainJourneyDigest(access), RefreshDigest: plainJourneyDigest("controlled-refresh"), AccessExpiresAt: at.Add(15 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func TestPlainWordActionJourney(t *testing.T) {
	ctx := t.Context()
	db := authorDB(t)
	at := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	current, err := db.ReconcileConnectorOwner(ctx, 42, at)
	if err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 42}
	seed, err := book.Create(ctx, "journey-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = book.Publish(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	remoteID := 500
	approvalMessage, approvalText := "", ""
	var sent []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/channels/3/typing" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" {
			t.Errorf("unexpected Discord request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sent = append(sent, body.Content)
		remoteID++
		if strings.Contains(body.Content, "Approval request") {
			approvalMessage = fmt.Sprint(remoteID)
			approvalText = body.Content
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"%d"}`, remoteID)
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})
	chat := messaging.New(db, "discord", "7", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
	chat.Protect = func(_ context.Context, s string) (string, error) { return s, nil }
	author := &plainJourneyAuthor{}
	sharedStandards := &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: author}, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	chat.EnableStandards(sharedStandards)
	actions := owneraction.New(db, messaging.OwnerActionDomain{Service: chat}, func() time.Time { return at })
	actions.SetDiscord("7", []string{"3"})
	actions.SetDelivery(actionApprovalDelivery{adapter})
	discordPrincipal := owneraction.Principal{OwnerID: "42", OwnerGeneration: current.Generation, Transport: "discord", DiscordOwnerID: "7", ChannelID: "3"}
	chat.EnableOwnerActions(actions, func(context.Context, string) (owneraction.Principal, error) { return discordPrincipal, nil })
	if err = chat.EnableConversations(ctx, messaging.ConversationOptions{Factory: true, Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: t.TempDir(), Providers: map[string]messaging.ConversationProvider{"claude": plainJourneyProvider{}}, CheckAccess: func(context.Context, string) error { return nil }}, at); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	local, err := connector.NewLocalControl(ctx, db, plainJourneyControl{db: db, now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = 42
	cfg.GitHubClientID = "fixture"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: db, Logger: logger, LocalControl: local, GitHubSecret: "fixture-secret", CookieKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return at }, Actions: actions})
	if err != nil {
		t.Fatal(err)
	}
	token := plainJourneyToken(t, db, cfg.ResourceURL, at)
	call := func(tool string, args any, target any) {
		t.Helper()
		raw, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(http.MethodPost, cfg.ResourceURL, bytes.NewReader(raw)).WithContext(ctx)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, req)
		var envelope struct {
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
				IsError    bool            `json:"isError"`
			}
			Error json.RawMessage `json:"error"`
		}
		if e = json.Unmarshal(response.Body.Bytes(), &envelope); e != nil || response.Code != 200 || envelope.Result.IsError || len(envelope.Error) > 0 || len(envelope.Result.Structured) == 0 {
			t.Fatalf("%s: HTTP %d %s (%v)", tool, response.Code, response.Body.String(), e)
		}
		if e = json.Unmarshal(envelope.Result.Structured, target); e != nil {
			t.Fatal(e)
		}
	}
	var ownerWords []string
	var selected owneraction.Proposal
	var authorID string
	mcpSay := func(words string) owneraction.Proposal {
		t.Helper()
		ownerWords = append(ownerWords, words)
		switch words {
		case "Start a standard about bounded checks in quality.":
			call("action_propose", map[string]any{"action": "standards-author", "arguments": map[string]string{"action": "start", "intent": "bounded checks", "category_ids": "quality"}, "client_request_key": "owner-start"}, &selected)
			authorID = selected.Facts.TargetID
		case "Publish that standard.":
			call("action_propose", map[string]any{"action": "standards-author", "arguments": map[string]string{"action": "publish", "id": authorID, "text": words}, "client_request_key": "owner-publish"}, &selected)
		case "yes":
			call("action_confirm", map[string]any{"proposal_id": selected.ID}, &selected)
		case "show that":
			call("action_inspect", map[string]any{"proposal_id": selected.ID}, &selected)
		default:
			t.Fatalf("unsupported owner words %q", words)
		}
		return selected
	}
	// MCP client carries every identifier while the owner's inputs stay plain.
	cheap := mcpSay("Start a standard about bounded checks in quality.")
	if cheap.Tier != owneraction.Conversational || cheap.Facts.TargetRevision == "" || cheap.Principal.GrantID != "journey-grant" || cheap.Facts.Estimate == "" {
		t.Fatalf("incomplete MCP facts: %+v", cheap)
	}
	if got := mcpSay("show that"); got.ID != cheap.ID || got.Facts.TargetRevision != cheap.Facts.TargetRevision {
		t.Fatal("inspection changed the fixed target")
	}
	at = at.Add(5 * time.Second)
	mcpSay("yes")
	if _, err = actions.Step(ctx, cheap.ID); err != nil {
		t.Fatal(err)
	}
	if got := mcpSay("show that"); got.State != "succeeded" {
		t.Fatalf("MCP conversational: %+v", got)
	}

	binding := mcpSay("Publish that standard.")
	if binding.Tier != owneraction.Binding {
		t.Fatal("publication was not binding")
	}
	at = at.Add(5 * time.Second)
	mcpSay("yes")
	if _, err = actions.Step(ctx, binding.ID); err != nil {
		t.Fatal(err)
	}
	if got := mcpSay("show that"); got.State != "awaiting_discord" || approvalMessage == "" {
		t.Fatalf("MCP bypassed Discord: %+v", got)
	}
	daemon := &Daemon{chat: &chatRuntime{service: chat}, log: logger}
	daemon.ownerReaction(ctx, discord.OwnerReaction{UserID: "7", ChannelID: "3", MessageID: approvalMessage, ReceivedAt: at})
	if _, err = actions.Step(ctx, binding.ID); err != nil {
		t.Fatal(err)
	}
	if got := mcpSay("show that"); got.State != "succeeded" || len(got.Attempts) < 2 {
		t.Fatalf("MCP binding: %+v", got)
	}
	var mcpHistory owneraction.Page
	call("action_history", map[string]any{"limit": 10}, &mcpHistory)
	if len(mcpHistory.Proposals) != 2 {
		t.Fatalf("MCP history count %d", len(mcpHistory.Proposals))
	}
	// Discord intake, conversation interpretation, delivery, selection and reaction
	// use the same service and existing domain handlers.
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	next := 100
	replyTo := ""
	say := func(words string) string {
		t.Helper()
		ownerWords = append(ownerWords, words)
		next++
		at = at.Add(time.Second)
		reader := &standardsJourneyReader{message: store.NaturalMessage{ChannelRef: "3", SourceID: fmt.Sprint(next), OwnerID: "7", Text: words, ReplyTo: replyTo, CreatedAt: at}}
		intake := messaging.Intake{DB: db, Reader: reader, Owner: "7", Transport: "discord", Channels: []string{"3"}, Protect: chat.Protect}
		if e := intake.Poll(ctx, at); e != nil {
			t.Fatal(e)
		}
		first := len(sent)
		for range 100 {
			work, e := chat.ConversationStep(ctx, at)
			if e != nil {
				t.Fatal(e)
			}
			delivered, e := chat.ConversationDeliveryStep(ctx, at)
			if e != nil {
				t.Fatal(e)
			}
			if !work && !delivered {
				break
			}
		}
		return strings.Join(sent[first:], "\n")
	}
	latestDiscord := func() owneraction.Proposal {
		t.Helper()
		page, e := actions.History(ctx, discordPrincipal, "", 10)
		if e != nil {
			t.Fatal(e)
		}
		for _, proposal := range page.Proposals {
			if proposal.Principal.Transport == "discord" {
				return proposal
			}
		}
		t.Fatal("Discord proposal missing")
		return owneraction.Proposal{}
	}
	if preview := say("Start a standard about bounded checks in quality."); !strings.Contains(preview, "bounded checks") || !strings.Contains(preview, "Reply yes") || strings.Contains(preview, "Arguments") {
		t.Fatalf("Discord review missing: %s", preview)
	}
	discordCheap := latestDiscord()
	replyTo = discordCheap.ProposalMessage
	if discordCheap.AvailableAt.IsZero() || discordCheap.Tier != owneraction.Conversational {
		t.Fatalf("undelivered Discord proposal: %+v", discordCheap)
	}
	if details := say("show that"); !strings.Contains(details, discordCheap.Facts.TargetRevision) {
		t.Fatalf("Discord details missing: %s", details)
	}
	at = at.Add(5 * time.Second)
	say("yes")
	if _, err = actions.Step(ctx, discordCheap.ID); err != nil {
		t.Fatal(err)
	}
	if result := say("show that"); !strings.Contains(result, "Done") {
		t.Fatalf("Discord conversational result: %s", result)
	}
	replyTo = ""
	// The binding journey reads in order: yes first, then the announced tap,
	// then the tap on the approval request, each with its deadline and cancel.
	proposed := say("Publish that standard.")
	discordBinding := latestDiscord()
	until := discordBinding.ExpiresAt.UTC().Format("15:04") + " UTC"
	if yes, tap := strings.Index(proposed, "yes"), strings.Index(proposed, "✅"); yes < 0 || tap < yes || !strings.Contains(proposed, until) {
		t.Fatalf("binding proposal steps: %s", proposed)
	}
	replyTo = discordBinding.ProposalMessage
	at = at.Add(5 * time.Second)
	acknowledged := say("yes")
	for _, token := range []string{"✅", "Reply no", until} {
		if !strings.Contains(acknowledged, token) {
			t.Fatalf("binding acknowledgment lost %q: %s", token, acknowledged)
		}
	}
	if _, err = actions.Step(ctx, discordBinding.ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"Approval request", "Publication of standards draft", "✅", "this message", until} {
		if !strings.Contains(approvalText, token) {
			t.Fatalf("approval request lost %q: %s", token, approvalText)
		}
	}
	for _, token := range []string{discordBinding.ID, "Owner:", "History reference"} {
		if strings.Contains(approvalText, token) {
			t.Fatalf("approval request shows %q: %s", token, approvalText)
		}
	}
	waiting, err := actions.Inspect(ctx, discordPrincipal, discordBinding.ID)
	if err != nil || waiting.State != "awaiting_discord" {
		t.Fatalf("Discord binding admitted without tap: %+v %v", waiting, err)
	}
	daemon.ownerReaction(ctx, discord.OwnerReaction{UserID: "7", ChannelID: "3", MessageID: approvalMessage, ReceivedAt: at})
	if _, err = actions.Step(ctx, discordBinding.ID); err != nil {
		t.Fatal(err)
	}
	if result := say("show that"); !strings.Contains(result, "Done") || !strings.Contains(result, "Publication of standards draft") {
		t.Fatalf("Discord binding result: %s", result)
	}
	if history := say("show my action history"); !strings.Contains(history, discordBinding.ID) || !strings.Contains(history, binding.ID) {
		t.Fatalf("cross-client Discord history missing: %s", history)
	}
	for _, words := range ownerWords {
		for _, id := range []string{cheap.ID, binding.ID, discordCheap.ID, discordBinding.ID, authorID} {
			if id != "" && strings.Contains(words, id) {
				t.Fatalf("owner copied an identifier: %q", words)
			}
		}
	}
	finalBook, e := book.Current(ctx)
	if e != nil || finalBook.Publication == nil || finalBook.Publication.Provenance != "owner-action:"+discordBinding.ID || len(finalBook.Snapshot.Standards) != 2 {
		t.Fatalf("recorded success did not match domain effects: %+v %v", finalBook, e)
	}
	if author.calls != 2 {
		t.Fatalf("provider called %d times, want one per start", author.calls)
	}
}
