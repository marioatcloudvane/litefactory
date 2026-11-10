package discordcomponent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/discord"
)

func TestDiscordReadFailuresAreLoggedOncePerState(t *testing.T) {
	var out bytes.Buffer
	reads := &discordReadLog{log: slog.New(slog.NewTextHandler(&out, nil))}
	limited := &discord.IntakeFailure{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Second}
	for range 10 {
		reads.report(t.Context(), "ordinary message intake", limited)
	}
	reads.report(t.Context(), "conversation access", limited)
	for range 3 {
		reads.report(t.Context(), "ordinary message intake", nil)
	}
	reads.report(t.Context(), "ordinary message intake", &discord.IntakeFailure{Status: http.StatusUnauthorized, Capability: true})
	reads.report(t.Context(), "ordinary message intake", errors.New("natural intake text too large"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("want one line per state change, got %d:\n%s", len(lines), out.String())
	}
	for i, want := range []string{"kind=\"rate limited\"", "source=\"conversation access\"", "recovered", "kind=unauthorized", "kind=other"} {
		if !strings.Contains(lines[i], want) {
			t.Fatalf("line %d misses %s: %s", i, want, lines[i])
		}
	}
	if !strings.Contains(lines[0], "retry_after=30s") || !strings.Contains(lines[0], "status=429") {
		t.Fatalf("rate limit line misses its wait: %s", lines[0])
	}
}

func TestIntakeStatusNamesTheCause(t *testing.T) {
	if s := intakeStatus(&discord.IntakeFailure{Status: http.StatusTooManyRequests, RetryAfter: 30 * time.Second}, 30*time.Second); !strings.Contains(s, "rate limited") || !strings.Contains(s, "30s") {
		t.Fatalf("rate limit status: %q", s)
	}
	if s := intakeStatus(&discord.IntakeFailure{Status: http.StatusForbidden, Capability: true}, time.Second); !strings.Contains(s, "forbidden") || !strings.Contains(s, "Message Content") {
		t.Fatalf("access status: %q", s)
	}
}

func TestNoticeQuestionExplainsMissingConversationBinding(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "notice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateAlertControlScan(ctx, "4", "100"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/applications/@me" {
			fmt.Fprint(w, `{"flags":262144}`)
			return
		}
		fmt.Fprintf(w, `[{"id":"101","channel_id":"4","content":"<@9> private factory question","timestamp":%q,"author":{"id":"7"}}]`, at.Add(-time.Second).Format(time.RFC3339Nano))
	}))
	t.Cleanup(api.Close)
	var logs bytes.Buffer
	c, err := New(Options{Storage: db, Factory: factory.NewMessaging(db), Logger: slog.New(slog.NewTextHandler(&logs, nil)), AlertControls: true,
		Credentials:   Credentials{Settings: config.DiscordChat{PublicKey: strings.Repeat("ab", 32), OwnerID: "7", ChannelIDs: []string{"3", "4"}}, Token: secrets.NewToken("private-token"), APIURL: api.URL, Client: api.Client()},
		Conversations: &messaging.ConversationOptions{Instructions: factorylibtest.Library(t), Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "chat", Provider: "claude", Model: "test"}}, StateRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.intake.PollChannel(ctx, "4", at); err != nil {
		t.Fatal(err)
	}
	scan, exists, err := db.ReadAlertControlScan(ctx, "4")
	if err != nil || !exists || scan.Cursor != "101" {
		t.Fatalf("scan %+v exists=%v err=%v", scan, exists, err)
	}
	pending, err := db.PendingNaturalMessages(ctx, "4", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("notice admitted: %+v %v", pending, err)
	}
	for _, want := range []string{"reason=no_conversation_binding", "channel_id=4", "reason=non_control_text", "source_id=101"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("missing %s in logs: %s", want, logs.String())
		}
	}
	for _, private := range []string{"private factory question", "private-token"} {
		if strings.Contains(logs.String(), private) {
			t.Error("private data logged")
		}
	}
}

type diagnosticProvider struct{ prompts []string }

func (p *diagnosticProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	if err := r.BeforeLaunch(ctx); err != nil {
		return agent.ConversationResult{}, err
	}
	p.prompts = append(p.prompts, r.Prompt)
	session := r.Session
	session.Resume = session.Ref
	if err := r.OnSession(ctx, session); err != nil {
		return agent.ConversationResult{}, err
	}
	return agent.ConversationResult{Session: session, Answer: []byte("Fixture answer.")}, nil
}

func TestBoundQuestionsPreserveTextAndReplay(t *testing.T) {
	for _, controls := range []bool{false, true} {
		for _, text := range []string{"what is running in the factory?", "<@9> what is running in the factory?", "<@9>"} {
			t.Run(fmt.Sprintf("controls=%t/%s", controls, text), func(t *testing.T) {
				ctx := t.Context()
				db, err := store.Open(ctx, filepath.Join(t.TempDir(), "bound.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				if err = db.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
					t.Fatal(err)
				}
				if err = db.ActivateAlertControlScan(ctx, "3", "100"); err != nil {
					t.Fatal(err)
				}
				at := time.Now().UTC()
				var mu sync.Mutex
				var replies []string
				reads := 0
				api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/applications/@me":
						fmt.Fprint(w, `{"flags":262144}`)
					case r.Method == http.MethodPost && r.URL.Path == "/channels/3/messages":
						var body struct {
							Content, Nonce string
							EnforceNonce   bool `json:"enforce_nonce"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if body.Nonce == "" || !body.EnforceNonce {
							t.Error("missing stable delivery nonce")
						}
						mu.Lock()
						replies = append(replies, body.Content)
						mu.Unlock()
						fmt.Fprint(w, `{"id":"999"}`)
					case r.Method == http.MethodPost:
						w.WriteHeader(http.StatusNoContent)
					case r.URL.Path == "/channels/3/messages":
						mu.Lock()
						reads++
						mu.Unlock()
						_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "101", "channel_id": "3", "content": text, "timestamp": at.Add(-time.Second), "author": map[string]string{"id": "7"}}})
					default:
						t.Errorf("unexpected network work: %s %s", r.Method, r.URL)
						fmt.Fprint(w, `[]`)
					}
				}))
				t.Cleanup(api.Close)
				var logs bytes.Buffer
				var logMu sync.Mutex
				logger := slog.New(slog.NewTextHandler(lockedWriter{&logMu, &logs}, nil))
				provider := &diagnosticProvider{}
				c, err := New(Options{Storage: db, Factory: factory.NewMessaging(db), Logger: logger, AlertControls: controls,
					Credentials:   Credentials{Settings: config.DiscordChat{PublicKey: strings.Repeat("ab", 32), OwnerID: "7", ChannelIDs: []string{"3", "4"}}, Token: secrets.NewToken("private-token"), APIURL: api.URL, Client: api.Client()},
					Conversations: &messaging.ConversationOptions{Instructions: factorylibtest.Library(t), StateRoot: t.TempDir(), Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "chat", Provider: "claude", Model: "fixture"}}, Providers: map[string]messaging.ConversationProvider{"claude": provider}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err = c.service.EnableConversations(ctx, *c.conversations, at); err != nil {
					t.Fatal(err)
				}
				for range 2 { // startup and reconnect replay the same source identity
					if err = c.intake.PollChannel(ctx, "3", at); err != nil {
						t.Fatal(err)
					}
					for range 3 {
						if _, err = c.service.ConversationStep(ctx, at); err != nil {
							t.Fatal(err)
						}
						if _, err = c.service.ConversationDeliveryStep(ctx, at); err != nil {
							t.Fatal(err)
						}
					}
				}
				if len(provider.prompts) != 1 {
					t.Fatalf("provider calls %d", len(provider.prompts))
				}
				var prompt struct{ OwnerInput string }
				if err = json.Unmarshal([]byte(provider.prompts[0]), &prompt); err != nil || prompt.OwnerInput != text {
					t.Fatalf("provider input changed: %q %v", prompt.OwnerInput, err)
				}
				conversation, err := db.ReadConversation(ctx, "discord", "3")
				if err != nil {
					t.Fatal(err)
				}
				turn, err := db.LatestConversationTurn(ctx, conversation.ID)
				if err != nil || turn.State != "completed" || turn.DeliveredAt.IsZero() || turn.SourceID != "101" || turn.Source != text {
					t.Fatalf("turn state=%s delivered=%v source=%q error=%v", turn.State, turn.DeliveredAt, turn.Source, err)
				}
				if err = json.Unmarshal([]byte(turn.Prompt), &prompt); err != nil || prompt.OwnerInput != text {
					t.Fatalf("persisted input changed: %q %v", prompt.OwnerInput, err)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(replies) != 1 || replies[0] != "Fixture answer." {
					t.Fatalf("replies %v", replies)
				}
				wantReads := 3 // one capability read and two ordinary pages
				if controls {
					wantReads = 5
				}
				if reads != wantReads {
					t.Fatalf("history reads %d, want %d", reads, wantReads)
				}
				logMu.Lock()
				defer logMu.Unlock()
				for _, want := range []string{"stage=setup channel_id=4 reason=no_conversation_binding", "stage=ordinary_scan reason=released", "stage=conversation_admission", "reason=persisted", "reason=cursor_replay"} {
					if !strings.Contains(logs.String(), want) {
						t.Errorf("missing %s: %s", want, logs.String())
					}
				}
				if strings.Contains(logs.String(), "what is running") || strings.Contains(logs.String(), "private-token") {
					t.Fatal("private text logged")
				}
			})
		}
	}
}
