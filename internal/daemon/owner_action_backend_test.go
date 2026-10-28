package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/turn"
)

type independentActionFilter struct{}

func (independentActionFilter) Identities() turn.Identities {
	return turn.Identities{OwnerUserID: 42, BotUserID: 99}
}
func (independentActionFilter) Admit(context.Context, int64, github.Comment) (turn.Admission, error) {
	return turn.Admission{}, errors.New("worker admission not available")
}

func TestMCPActionsAndHistorySurviveUnavailableDiscord(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "token-unavailable"}[configured], func(t *testing.T) {
			setup := remoteSetupFixture(t)
			db := setup.DB
			at := time.Now().UTC().Truncate(time.Millisecond)
			server, err := connector.New(t.Context(), connector.Options{Config: setup.Config.RemoteConnector, DB: db, Logger: setup.Logger, LocalControl: setup.ConnectorControl, GitHubSecret: "fixture", CookieKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return at }})
			if err != nil {
				t.Fatal(err)
			}
			book := &standards.Service{Store: db, OwnerID: 42}
			seed, err := book.Create(t.Context(), "offline-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = book.Approve(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
				t.Fatal(err)
			}
			if _, err = book.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
				t.Fatal(err)
			}
			author := &plainJourneyAuthor{}
			options := &ChatOptions{Now: func() time.Time { return at }, Standards: &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Store: db, Book: book, Runner: author}}}
			if configured {
				options.Settings.OwnerID = "7"
				options.Settings.ChannelIDs = []string{"3"}
				options.Resolve = func(context.Context) (secrets.Token, error) { return secrets.Token{}, errors.New("token unavailable") }
			}
			d := &Daemon{db: db, log: setup.Logger, filter: independentActionFilter{}, remoteConnector: &remoteConnectorRuntime{server: server}}
			sanitizer := redact.New(redact.Options{Environ: []string{}})
			d.prepareIndependentOwnerActions(options, sanitizer)
			if d.chat != nil {
				t.Fatal("MCP composition fabricated a Discord runtime")
			}
			token := plainJourneyToken(t, db, setup.Config.RemoteConnector.ResourceURL, at)
			handler := server.Handler()
			call := func(name string, args any) json.RawMessage {
				t.Helper()
				raw, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
				if e != nil {
					t.Fatal(e)
				}
				request := httptest.NewRequest(http.MethodPost, setup.Config.RemoteConnector.ResourceURL, bytes.NewReader(raw))
				request.RemoteAddr = "127.0.0.1:12345"
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json")
				request.Header.Set("MCP-Protocol-Version", "2025-11-25")
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != http.StatusOK {
					t.Fatalf("%s HTTP %d %s", name, response.Code, response.Body.String())
				}
				return response.Body.Bytes()
			}
			var out struct {
				Result struct {
					IsError  bool                 `json:"isError"`
					Proposal owneraction.Proposal `json:"structuredContent"`
				} `json:"result"`
			}
			raw := call("action_propose", map[string]any{"action": "standards-author", "arguments": map[string]string{"action": "start", "id": "offline-author", "mode": "create", "intent": "Bound checks", "category_ids": "quality"}, "client_request_key": "offline-cheap"})
			if err = json.Unmarshal(raw, &out); err != nil || out.Result.IsError || out.Result.Proposal.ID == "" {
				t.Fatalf("offline proposal: %s %v", raw, err)
			}
			proposal := out.Result.Proposal
			at = proposal.ConfirmNotBefore
			raw = call("action_confirm", map[string]string{"proposal_id": proposal.ID})
			if err = json.Unmarshal(raw, &out); err != nil || out.Result.IsError || out.Result.Proposal.State != "admitted" {
				t.Fatalf("offline confirmation: %s %v", raw, err)
			}
			runCtx, cancel := context.WithCancel(t.Context())
			var joined sync.WaitGroup
			runtime := d.remoteConnector.actions
			runtime.start(runCtx, &joined, d.log)
			runtime.start(runCtx, &joined, d.log)
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			poll := time.NewTicker(10 * time.Millisecond)
			defer poll.Stop()
			complete := false
			for !complete {
				select {
				case <-deadline.C:
					cancel()
					joined.Wait()
					t.Fatal("offline worker did not settle admitted action")
				case <-poll.C:
					record, e := db.ReadOwnerAction(t.Context(), proposal.ID)
					if e != nil {
						cancel()
						joined.Wait()
						t.Fatal(e)
					}
					switch record.State {
					case "succeeded":
						complete = true
					case "failed", "uncertain":
						cancel()
						joined.Wait()
						t.Fatalf("offline action: %+v", record)
					}
				}
			}
			cancel()
			joined.Wait()
			if author.calls != 1 {
				t.Fatalf("worker started duplicate author effects: %d", author.calls)
			}
			// A fresh service after restart still exposes recorded inspection/history,
			// even while no Discord runtime or delivery capability exists.
			d.prepareIndependentOwnerActions(options, sanitizer)
			raw = call("action_inspect", map[string]string{"proposal_id": proposal.ID})
			if err = json.Unmarshal(raw, &out); err != nil || out.Result.IsError || out.Result.Proposal.State != "succeeded" {
				t.Fatalf("offline inspection: %s %v", raw, err)
			}
			history := call("action_history", map[string]any{"limit": 10})
			if !bytes.Contains(history, []byte(proposal.ID)) || !bytes.Contains(history, []byte("succeeded")) {
				t.Fatalf("offline history missing: %s", history)
			}
			binding := call("action_propose", map[string]any{"action": "standards-author", "arguments": map[string]string{"action": "publish", "id": "offline-author"}, "client_request_key": "offline-binding"})
			if !bytes.Contains(binding, []byte("Discord owner approval is not configured")) {
				t.Fatalf("unavailable Discord had a binding bypass: %s", binding)
			}
		})
	}
}

func TestLazyDiscordJoinsExistingOwnerActionExecutor(t *testing.T) {
	setup := remoteSetupFixture(t)
	server, err := connector.New(t.Context(), connector.Options{Config: setup.Config.RemoteConnector, DB: setup.DB, Logger: setup.Logger, LocalControl: setup.ConnectorControl, GitHubSecret: "fixture", CookieKey: bytes.Repeat([]byte{7}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: setup.DB, filter: independentActionFilter{}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), remoteConnector: &remoteConnectorRuntime{server: server}}
	d.prepareIndependentOwnerActions(nil, redact.New(redact.Options{Environ: []string{}}))
	runtime := d.remoteConnector.actions
	actions := runtime.actions
	for _, entry := range actions.Catalog() {
		if entry.Command == "draft" || entry.Command == "diagnose" || strings.HasPrefix(entry.Command, "standards-") {
			t.Fatalf("missing handler advertised: %+v", entry)
		}
	}
	chat := messaging.New(setup.DB, "discord", "7", []string{"3"}, nil, messaging.FactoryStatus{DB: setup.DB})
	adapter := discord.New(discord.Options{Token: secrets.NewToken("fixture")})
	d.configureOwnerActions(chat, "7", []string{"3"}, adapter, time.Now)
	if chat.OwnerActions() != actions || d.remoteConnector.actions != runtime || runtime.notices.Load() != chat {
		t.Fatal("lazy Discord replaced the shared executor")
	}
}
