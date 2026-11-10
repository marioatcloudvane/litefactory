package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/coder/websocket"
)

type componentJourneyProvider struct {
	calls   atomic.Int32
	resumes atomic.Int32
}

func (p *componentJourneyProvider) RunConversation(ctx context.Context, r agent.ConversationRequest) (agent.ConversationResult, error) {
	p.calls.Add(1)
	if r.Session.Resume != "" {
		p.resumes.Add(1)
	}
	var input struct {
		OwnerSources []messaging.DialogueTurn `json:"owner_sources"`
	}
	if err := json.Unmarshal([]byte(r.Prompt), &input); err != nil {
		return agent.ConversationResult{}, err
	}
	if len(input.OwnerSources) > 0 && input.OwnerSources[len(input.OwnerSources)-1].Text == "Hello." {
		if err := r.BeforeLaunch(ctx); err != nil {
			return agent.ConversationResult{}, err
		}
		session := r.Session
		session.Resume = "plain-word-session"
		if err := r.OnSession(ctx, session); err != nil {
			return agent.ConversationResult{}, err
		}
		return agent.ConversationResult{Session: session, Answer: []byte(`{"version":1,"kind":"answer","text":"Ready to discuss your factory work."}`)}, nil
	}
	return (plainJourneyProvider{}).RunConversation(ctx, r)
}

type componentJourneyAuthor struct{ calls atomic.Int32 }

func (a *componentJourneyAuthor) Answer(ctx context.Context, r standards.AuthoringCall) ([]byte, error) {
	a.calls.Add(1)
	return (&plainJourneyAuthor{}).Answer(ctx, r)
}

type componentJourneyAPI struct {
	mu        sync.Mutex
	history   []map[string]any
	sent      []string
	events    chan map[string]any
	connected chan struct{}
	once      sync.Once
	server    *httptest.Server
}

func newComponentJourneyAPI(t *testing.T) *componentJourneyAPI {
	f := &componentJourneyAPI{events: make(chan map[string]any, 8), connected: make(chan struct{})}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/applications/@me":
			fmt.Fprint(w, `{"flags":262144}`)
		case r.URL.Path == "/applications/1/commands":
			fmt.Fprint(w, `[]`)
		case r.URL.Path == "/gateway/bot":
			fmt.Fprintf(w, `{"url":"ws://%s/ws","session_start_limit":{"remaining":10}}`, r.Host)
		case r.URL.Path == "/channels/3/typing":
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/channels/3/messages" && r.Method == http.MethodPost:
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.sent = append(f.sent, body.Content)
			id := 500 + len(f.sent)
			f.mu.Unlock()
			fmt.Fprintf(w, `{"id":"%d"}`, id)
		case r.URL.Path == "/channels/3/messages":
			f.mu.Lock()
			defer f.mu.Unlock()
			page := []map[string]any{}
			for i := len(f.history) - 1; i >= 0; i-- {
				m := f.history[i]
				if before := r.URL.Query().Get("before"); before == "" || fmt.Sprint(m["id"]) < before {
					page = append(page, m)
				}
			}
			if r.URL.Query().Get("limit") == "1" && len(page) > 1 {
				page = page[:1]
			}
			_ = json.NewEncoder(w).Encode(page)
		case r.URL.Path == "/ws":
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = c.CloseNow() }()
			send := func(op int, data any, seq int, event string) error {
				raw, _ := json.Marshal(map[string]any{"op": op, "d": data, "s": seq, "t": event})
				return c.Write(r.Context(), websocket.MessageText, raw)
			}
			if send(10, map[string]any{"heartbeat_interval": 60000}, 0, "") != nil {
				return
			}
			if _, _, err = c.Read(r.Context()); err != nil {
				return
			}
			if send(0, map[string]any{"session_id": "component-session", "resume_gateway_url": "ws://" + r.Host + "/ws"}, 1, "READY") != nil {
				return
			}
			f.once.Do(func() { close(f.connected) })
			// Reading concurrently lets cancellation close the websocket promptly.
			disconnected := make(chan struct{})
			go func() {
				defer close(disconnected)
				for {
					if _, _, e := c.Read(r.Context()); e != nil {
						return
					}
				}
			}()
			for seq := 2; ; seq++ {
				select {
				case m := <-f.events:
					if send(0, m, seq, "MESSAGE_CREATE") != nil {
						return
					}
				case <-disconnected:
					return
				case <-r.Context().Done():
					return
				}
			}
		default:
			t.Errorf("unexpected controlled Discord request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *componentJourneyAPI) say(id, text string) {
	m := map[string]any{"id": id, "channel_id": "3", "content": text, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "author": map[string]any{"id": "7"}}
	f.mu.Lock()
	f.history = append(f.history, m)
	f.mu.Unlock()
	f.events <- m
}
func (f *componentJourneyAPI) contains(text string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sent := range f.sent {
		if strings.Contains(sent, text) {
			return true
		}
	}
	return false
}
func (f *componentJourneyAPI) delivered(part store.ConversationAnswerPart) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, sent := range f.sent {
		if part.DiscordMessageID == fmt.Sprint(501+i) && part.Text == sent {
			return true
		}
	}
	return false
}

func componentJourneyAwait(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if done() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", what)
		case <-tick.C:
		}
	}
}

// AC-002 exercises real composition, Gateway intake, resumed conversations,
// central confirmation, saved outcomes and Discord delivery with local providers.
func TestDiscordComponentOwnerConversationMainPath(t *testing.T) {
	ctx := t.Context()
	db := authorDB(t)
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	current, err := db.ReconcileConnectorOwner(ctx, 42, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 42}
	seed, err := book.Create(ctx, "component-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = book.Publish(ctx, seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	api := newComponentJourneyAPI(t)
	provider := &componentJourneyProvider{}
	author := &componentJourneyAuthor{}
	d := &Daemon{db: db, filter: independentActionFilter{}, log: slog.New(slog.DiscardHandler), webhook: &webhookRuntime{}}
	opts := &ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", PublicKey: strings.Repeat("ab", 32), OwnerID: "7", ChannelIDs: []string{"3"}}, Token: secrets.NewToken("controlled-component-token"), APIURL: api.server.URL, Client: api.server.Client(), Conversations: &messaging.ConversationOptions{Instructions: factorylibtest.Library(t), Factory: true, Bindings: []messaging.ConversationBinding{{Channel: "3", Name: "owner", Provider: "claude", Model: "fixture"}}, StateRoot: t.TempDir(), Providers: map[string]messaging.ConversationProvider{"claude": provider}}, Standards: &ConversationStandards{DB: db, Book: book, Authoring: &standards.Authoring{Instructions: factorylibtest.Library(t), Store: db, Book: book, Runner: author}}}
	if err = d.initChat(opts); err != nil {
		t.Fatal(err)
	}
	run, cancel := context.WithCancel(ctx)
	var joined sync.WaitGroup
	d.chatServe(run, &joined)
	d.startOwnerActions(run, &joined)
	t.Cleanup(func() {
		cancel()
		done := make(chan struct{})
		go func() { joined.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("component workers did not join")
		}
	})
	select {
	case <-api.connected:
	case <-time.After(10 * time.Second):
		t.Fatal("component did not connect Gateway")
	}
	api.say("101", "Hello.")
	componentJourneyAwait(t, "first saved reply", func() bool { return api.contains("Ready to discuss your factory work.") })
	first, err := db.ReadConversation(ctx, "discord", "3")
	if err != nil || first.SessionHandle == "" {
		t.Fatalf("missing conversation session: %+v %v", first, err)
	}
	api.say("102", "Start a standard about bounded checks in quality.")
	principal := owneraction.Principal{OwnerID: "42", OwnerGeneration: current.Generation, Transport: "discord", DiscordOwnerID: "7", ChannelID: "3"}
	var proposal owneraction.Proposal
	componentJourneyAwait(t, "delivered proposal", func() bool {
		page, e := d.chat.actions.History(ctx, principal, "", 10)
		if e != nil || len(page.Proposals) != 1 {
			return false
		}
		proposal = page.Proposals[0]
		return !proposal.AvailableAt.IsZero()
	})
	if author.calls.Load() != 0 {
		t.Fatal("effect executed before confirmation")
	}
	componentJourneyAwait(t, "confirmation fence", func() bool { return !time.Now().Before(proposal.ConfirmNotBefore) })
	api.say("103", "yes")
	componentJourneyAwait(t, "saved successful action", func() bool {
		record, e := db.ReadOwnerAction(ctx, proposal.ID)
		return e == nil && record.State == "succeeded" && record.ExecutionID != ""
	})
	api.say("104", "show that")
	componentJourneyAwait(t, "saved result reply", func() bool {
		turn, e := db.LatestConversationTurn(ctx, first.ID)
		return e == nil && turn.SourceID == "104" && !turn.DeliveredAt.IsZero() && api.contains("Done")
	})
	final, err := db.ReadConversation(ctx, "discord", "3")
	if err != nil || final.SessionHandle != first.SessionHandle {
		t.Fatalf("conversation session changed: %+v %v", final, err)
	}
	if provider.calls.Load() != 2 || provider.resumes.Load() != 1 || author.calls.Load() != 1 {
		t.Fatalf("unexpected work: model=%d resumes=%d effects=%d", provider.calls.Load(), provider.resumes.Load(), author.calls.Load())
	}
	answer, err := db.ReadConversationAnswer(ctx, "discord", "7", "3", "")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := db.ReadConversationAnswerParts(ctx, answer.ID)
	if err != nil || len(parts) == 0 {
		t.Fatalf("saved reply parts missing: %v %v", parts, err)
	}
	for _, part := range parts {
		if part.AcknowledgedAt.IsZero() || part.DiscordMessageID == "" || !api.delivered(part) {
			t.Fatalf("delivery differs from saved bytes: %+v", part)
		}
	}
}
