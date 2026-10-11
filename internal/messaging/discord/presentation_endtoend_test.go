package discord_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/messaging/response"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type presentationProvider struct {
	calls    int
	requests []agent.ChatRequest
}

func (*presentationProvider) DoctorIsolation(context.Context) (string, error) {
	return "controlled test fixture; not live provider evidence", nil
}

func (p *presentationProvider) RunChat(ctx context.Context, request agent.ChatRequest) (agent.ChatResult, error) {
	if err := request.Validate(); err != nil {
		return agent.ChatResult{}, err
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > response.Budget {
		return agent.ChatResult{}, fmt.Errorf("presentation deadline missing")
	}
	files, err := os.ReadDir(request.WorkingDirectory)
	if err != nil || len(files) != 0 {
		return agent.ChatResult{}, fmt.Errorf("scratch is not empty")
	}
	p.requests = append(p.requests, request)
	p.calls++
	if p.calls == 1 {
		return agent.ChatResult{Answer: []byte(`{"claims":[{"text":"Hier ist der aktuelle Zustand.","facts":["result"],"protected":[]}]}`)}, nil
	}
	return agent.ChatResult{Answer: []byte(`{"findings":[{"claim":0,"support":"supported"}]}`)}, nil
}

// TestPlainLanguageReplyJourney covers signed admission, bot-only language
// history, isolated generation/checking, persistence, one-message attachment
// delivery and retry after restart. Providers and Discord are controlled fakes.
func TestPlainLanguageReplyJourney(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "reply.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	if err = db.ReplaceHealthSnapshot(ctx, store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(-time.Second)}, nil); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var payloads, rawDetails, routes []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/@me":
			_, _ = io.WriteString(w, `{"id":"8","bot":true}`)
			return
		case "/channels/3/messages":
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, `[{"id":"9","channel_id":"3","timestamp":"2026-10-11T08:00:00Z","author":{"id":"8","bot":true},"content":"> Wie ist der Zustand?\n> Project: acme/app\n[lf:ask:v1]\nUNRELATED PRIOR ANSWER"}]`)
				return
			}
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload, raw string
		for {
			part, e := reader.NextPart()
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				t.Error(e)
				break
			}
			data, e := io.ReadAll(part)
			if e != nil {
				t.Error(e)
			}
			switch part.FormName() {
			case "payload_json":
				payload = string(data)
			case "files[0]":
				if part.FileName() != presentation.RawFilename {
					t.Errorf("filename %q", part.FileName())
				}
				raw = string(data)
			}
			if e = part.Close(); e != nil {
				t.Error(e)
			}
		}
		mu.Lock()
		defer mu.Unlock()
		payloads = append(payloads, payload)
		rawDetails = append(rawDetails, raw)
		routes = append(routes, r.Method+" "+r.URL.Path)
		if len(payloads) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"200"}`)
	}))
	defer api.Close()
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &presentationProvider{}
	model := &response.NativeModel{Config: cfg, Paths: config.NewPaths(t.TempDir()), Providers: map[string]response.ChatRunner{"claude": provider}, Meter: db}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newService := func() (*discord.Adapter, *messaging.Service) {
		a := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
		s := messaging.New(db, "discord", "2", []string{"3"}, a, messaging.FactoryStatus{DB: db})
		s.EnablePresentation(model, a)
		return a, s
	}
	a, s := newService()
	w := signed(t, a.Handler(s), private, at, body("100", "3", "2", "health"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"type":5`) || provider.calls != 0 {
		t.Fatalf("admission: %d %s", w.Code, w.Body)
	}
	if more, err := s.Step(ctx, at); err != nil || !more {
		t.Fatalf("step: %t %v", more, err)
	}
	row, err := db.ReadChatReply(ctx, "discord", "100")
	if err != nil {
		t.Fatal(err)
	}
	if row.PresentationState != "narrated" || !row.PresentationGeneration || !row.PresentationCheck || row.State != "pending" {
		t.Fatalf("unfixed presentation: %+v", row)
	}
	var outcome presentation.Outcome
	if err = json.Unmarshal([]byte(row.PresentationOutcome), &outcome); err != nil {
		t.Fatal(err)
	}
	// The visible content is the narration only; the raw result is attached once.
	if outcome.LanguageHint() != "German" || row.ReplyText != "Hier ist der aktuelle Zustand." || row.ReplyAttachment != outcome.RawText {
		t.Fatalf("source changed: %+v", row)
	}
	if len(provider.requests) != 2 || provider.requests[0].Usage.AttemptID == provider.requests[1].Usage.AttemptID {
		t.Fatal("generation and check share usage")
	}
	for _, request := range provider.requests {
		if strings.Contains(request.Prompt, "UNRELATED PRIOR ANSWER") || strings.Contains(request.Prompt, "Wie ist") || strings.Contains(request.Prompt, "interaction-secret") {
			t.Fatal("history or authority leaked into provider")
		}
		if _, err := os.Stat(request.WorkingDirectory); !os.IsNotExist(err) {
			t.Fatal("scratch retained")
		}
	}
	_, s = newService() // Lost edit handle deliberately exercises channel fallback.
	if more, err := s.Step(ctx, row.NextAttemptAt); err != nil || !more {
		t.Fatalf("retry: %t %v", more, err)
	}
	if provider.calls != 2 {
		t.Fatal("retry repeated narration")
	}
	mu.Lock()
	if len(payloads) != 2 || rawDetails[0] != outcome.RawText || rawDetails[1] != rawDetails[0] || !strings.HasPrefix(routes[0], "PATCH ") || routes[1] != "POST /channels/3/messages" {
		t.Errorf("delivery mismatch: %v %v", routes, rawDetails)
	}
	var first, second struct {
		Content string `json:"content"`
		Nonce   string `json:"nonce"`
		Enforce bool   `json:"enforce_nonce"`
		Allowed struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	if err = json.Unmarshal([]byte(payloads[0]), &first); err != nil {
		t.Error(err)
	}
	if err = json.Unmarshal([]byte(payloads[1]), &second); err != nil {
		t.Error(err)
	}
	if first.Content != second.Content || second.Nonce == "" || !second.Enforce || len(second.Allowed.Parse) != 0 {
		t.Error("retry changed payload or mention policy")
	}
	if first.Content != "Hier ist der aktuelle Zustand." || strings.Contains(first.Content, "Raw result.") || strings.Contains(first.Content, outcome.RawText) {
		t.Errorf("visible content repeats raw text: %q", first.Content)
	}
	mu.Unlock()
	row, err = db.ReadChatReply(ctx, "discord", "100")
	if err != nil || row.State != "confirmed" || row.PresentationOutcome != "" || row.ReplyText != "" || row.ReplyAttachment != "" {
		t.Fatalf("terminal content retained: %+v %v", row, err)
	}
	snapshot, err := db.NewMeterSnapshot(ctx, store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snapshot.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snapshot.Next(ctx, 100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("usage: %+v %v", rows, err)
	}
	for _, usage := range rows {
		if usage.Project != "factory" || !strings.HasPrefix(usage.ExecutionPath, "chat_presentation_") || usage.PriceRevisionID < 1 {
			t.Fatalf("fabricated ownership or unpinned price: %+v", usage)
		}
	}
}

func TestDuplicateAnswerAcknowledgesReceiptWithoutClaimingApplication(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "duplicate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	a := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("test-token"), Now: func() time.Time { return at }})
	s := messaging.New(db, "discord", "2", []string{"3"}, a, nil)
	s.EnableResearch(nil, nil)
	// An admitted invalid command has no answer effect. A repeated transport
	// identity must not turn that refusal into a success acknowledgement.
	r := store.ChatReply{Transport: "discord", MessageID: "100", ChannelRef: "3", Command: "answer", CreatedAt: at, ReplyText: "your answer was not recorded; nothing was applied"}
	if _, err = db.AdmitChatReply(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	w := signed(t, a.Handler(s), private, at, body("100", "3", "2", "answer"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "already received") || strings.Contains(w.Body.String(), "already answered") || !strings.Contains(w.Body.String(), "Raw result.") {
		t.Fatalf("false duplicate completion: %d %s", w.Code, w.Body)
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "100")
	if err != nil || row.Attempts != 0 || row.ReplyText != r.ReplyText {
		t.Fatalf("duplicate changed command: %+v %v", row, err)
	}
}
