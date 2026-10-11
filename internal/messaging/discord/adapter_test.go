package discord_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func signed(t *testing.T, h http.Handler, key ed25519.PrivateKey, at time.Time, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	stamp := strconv.FormatInt(at.Unix(), 10)
	req := httptest.NewRequest(http.MethodPost, discord.Path, bytes.NewReader(body))
	req.Header.Set("X-Signature-Timestamp", stamp)
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(key, append([]byte(stamp), body...))))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func body(id, channel, owner, command string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"application_id":"1","type":2,"token":"interaction-secret","channel_id":%q,"member":{"user":{"id":%q}},"data":{"name":%q,"type":1}}`, id, channel, owner, command))
}

// TestOwnerCommands exercises the main path through signed HTTP, the real inbox,
// a fake Discord API, retry timing, duplicate delivery and a database restart.
func TestOwnerCommands(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "chat.db")
	open := func() *store.DB {
		db, err := store.Open(ctx, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return db
	}
	db := open()
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.ReplaceHealthSnapshot(ctx, store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(-time.Second)}, nil); err != nil {
		t.Fatal(err)
	}
	var sent []string
	var routes []string
	rateLimit := false
	var nonce string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		var data struct {
			Content string `json:"content"`
			Nonce   string `json:"nonce"`
			Enforce bool   `json:"enforce_nonce"`
		}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if rateLimit {
			rateLimit = false
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != "Bot bot-secret" || !data.Enforce || data.Nonce == "" {
				t.Error("fallback lacks token or enforced nonce")
			}
			nonce = data.Nonce
		} else if r.Header.Get("Authorization") != "" {
			t.Error("original edit sent bot credential")
		}
		sent = append(sent, data.Content)
		routes = append(routes, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	adapter := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("bot-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	service := messaging.New(db, "discord", "2", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
	handler := adapter.Handler(service)
	if err := adapter.Register(ctx); err != nil {
		t.Fatal(err)
	}
	runStep := func(want bool) {
		more, err := service.Step(ctx, at)
		if err != nil || more != want {
			t.Fatalf("step %t, %v", more, err)
		}
	}
	for i, cmd := range []string{"help", "health", "status", "unknown"} {
		id := strconv.Itoa(i + 10)
		for range 2 {
			w := signed(t, handler, private, at, body(id, "3", "2", cmd))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":5`) {
				t.Fatalf("ack: %d %s", w.Code, w.Body)
			}
		}
		runStep(true)
		runStep(false)
		r, err := db.ReadChatReply(ctx, "discord", id)
		if err != nil || r.State != "confirmed" || r.ReplyText != "" || r.Attempts != 1 {
			t.Fatalf("reply: %+v %v", r, err)
		}
	}
	if len(sent) != 4 || sent[0] != "/health, /help, /meter, /prices, /status" || !strings.Contains(sent[1], "Running: true") || !strings.Contains(sent[2], "Polling:") || sent[3] != "unknown command, try /help" {
		t.Fatalf("answers: %v", sent)
	}
	for _, who := range []struct{ channel, owner string }{{"4", "2"}, {"3", "4"}} {
		w := signed(t, handler, private, at, body("99", who.channel, who.owner, "status"))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"content":"no access"`) || !strings.Contains(w.Body.String(), `"flags":64`) {
			t.Fatalf("refusal: %s", w.Body)
		}
		if _, err := db.ReadChatReply(ctx, "discord", "99"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("rejected input persisted: %v", err)
		}
	}
	rateLimit = true
	signed(t, handler, private, at, body("50", "3", "2", "help"))
	runStep(true)
	retry, err := db.ReadChatReply(ctx, "discord", "50")
	if err != nil || retry.State != "pending" || !retry.NextAttemptAt.Equal(at.Add(10*time.Second)) {
		t.Fatalf("retry: %+v, %v", retry, err)
	}
	runStep(false)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	at = at.Add(10 * time.Second)
	adapter = discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("bot-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	service = messaging.New(db, "discord", "2", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
	runStep(true)
	runStep(false)
	if len(sent) != 5 || nonce == "" || routes[4] != "/channels/3/messages" {
		t.Fatalf("restart delivery: %v %v", sent, routes)
	}
	if bytes.Contains([]byte(retry.ReplyText), []byte("interaction-secret")) {
		t.Fatal("token persisted")
	}
}

type rejecting struct{ admissions int }

func (i *rejecting) Admit(_ context.Context, _ messaging.Message, _ time.Time) (bool, bool, error) {
	i.admissions++
	return false, false, nil
}
func (*rejecting) Notify() {}
func TestVerification(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	intake := &rejecting{}
	h := discord.New(discord.Options{PublicKey: pub, Now: func() time.Time { return at }}).Handler(intake)
	ping := []byte(`{"type":1}`)
	for _, test := range []struct {
		name string
		at   time.Time
		body []byte
		key  ed25519.PrivateKey
	}{{"stale", at.Add(-5*time.Minute - time.Second), ping, key}, {"oversize", at, bytes.Repeat([]byte("x"), 65537), key}} {
		t.Run(test.name, func(t *testing.T) {
			if w := signed(t, h, test.key, test.at, test.body); w.Code != http.StatusUnauthorized {
				t.Fatal(w.Code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, discord.Path, strings.NewReader(`{"type":1}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	w = signed(t, h, key, at, ping)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"type":1}` {
		t.Fatalf("ping: %s", w.Body)
	}
	if intake.admissions != 0 {
		t.Fatal("verification reached intake")
	}
}

// TestDeferredEdit covers the deferred acknowledgement followed by an edit of
// @original: a 404 on a live handle retries the edit, 5xx retries, and 401 or
// an expired handle falls back to the channel. Each Step logs one line.
func TestDeferredEdit(t *testing.T) {
	t.Parallel()
	const edit = "/webhooks/1/interaction-secret/messages/@original"
	for _, test := range []struct {
		name    string
		edits   []int
		advance time.Duration
		routes  []string
		states  []string
		status  []int
		outcome []string
	}{
		{"404 then edit", []int{404, 200}, 0, []string{edit, edit}, []string{"pending", "confirmed"}, []int{404, 200}, []string{"retry", "confirmed"}},
		{"500 then edit", []int{500, 200}, 0, []string{edit, edit}, []string{"pending", "confirmed"}, []int{500, 200}, []string{"retry", "confirmed"}},
		{"401 falls back", []int{401}, 0, []string{edit, "/channels/3/messages"}, []string{"confirmed"}, []int{200}, []string{"confirmed"}},
		{"expired handle falls back", nil, 15 * time.Minute, []string{"/channels/3/messages"}, []string{"confirmed"}, []int{200}, []string{"confirmed"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "chat.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := db.ReplaceHealthSnapshot(ctx, store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(-time.Second)}, nil); err != nil {
				t.Fatal(err)
			}
			var routes, contents []string
			edits := append([]int(nil), test.edits...)
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data struct {
					Content string `json:"content"`
					Nonce   string `json:"nonce"`
					Enforce bool   `json:"enforce_nonce"`
				}
				if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
					t.Error(err)
				}
				routes = append(routes, r.URL.Path)
				contents = append(contents, data.Content)
				if r.Method == http.MethodPatch {
					if r.Header.Get("Authorization") != "" {
						t.Error("edit sent bot credential")
					}
					code := edits[0]
					edits = edits[1:]
					w.WriteHeader(code)
					return
				}
				if r.Header.Get("Authorization") != "Bot bot-secret" || !data.Enforce || data.Nonce == "" {
					t.Error("fallback lacks token or enforced nonce")
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer api.Close()
			now := at
			adapter := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("bot-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return now }})
			service := messaging.New(db, "discord", "2", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
			var logs bytes.Buffer
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			w := signed(t, adapter.Handler(service), private, at, body("10", "3", "2", "health"))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":5`) {
				t.Fatalf("ack: %d %s", w.Code, w.Body)
			}
			now = at.Add(test.advance)
			for i, want := range test.states {
				logs.Reset()
				more, err := service.Step(ctx, now)
				if err != nil || !more {
					t.Fatalf("step %d: %t %v", i, more, err)
				}
				r, err := db.ReadChatReply(ctx, "discord", "10")
				if err != nil || r.State != want {
					t.Fatalf("step %d row: %+v %v", i, r, err)
				}
				if want == "pending" {
					if !r.NextAttemptAt.After(now) {
						t.Fatalf("retry not scheduled later: %+v", r)
					}
					now = r.NextAttemptAt
				}
				lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
				if len(lines) != 1 {
					t.Fatalf("step %d log lines: %q", i, logs.String())
				}
				var record struct {
					Command, Outcome, Route string
					Status                  int
				}
				if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
					t.Fatal(err)
				}
				route := "edit"
				if strings.HasPrefix(routes[len(routes)-1], "/channels/") {
					route = "channel"
				}
				if record.Command != "health" || record.Outcome != test.outcome[i] || record.Status != test.status[i] || record.Route != route {
					t.Fatalf("step %d log: %s", i, lines[0])
				}
				if strings.Contains(lines[0], "Running:") || strings.Contains(lines[0], "interaction-secret") {
					t.Fatalf("log carries reply or token: %s", lines[0])
				}
			}
			if strings.Join(routes, ",") != strings.Join(test.routes, ",") {
				t.Fatalf("routes: %v", routes)
			}
			for _, content := range contents {
				if !strings.Contains(content, "Running: ") {
					t.Fatalf("reply: %q", content)
				}
			}
		})
	}
}

// TestRefusedEditLogged checks that a refused interaction edit reports its
// HTTP status and Discord's JSON error code and message on the delivery line,
// without the interaction token or the webhook path.
func TestRefusedEditLogged(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body, route, outcome, message string
		edit, status, code                  int
	}{
		{"401 falls back", `{"message": "Invalid Webhook Token", "code": 50027}`, "channel", "confirmed", "Invalid Webhook Token", 401, 200, 50027},
		{"404 retries", `{"message": "Unknown Webhook", "code": 10015}`, "edit", "retry", "Unknown Webhook", 404, 404, 10015},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			db, err := store.Open(ctx, filepath.Join(t.TempDir(), "chat.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			if err := db.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := db.ReplaceHealthSnapshot(ctx, store.ListenerStatus{DaemonStartedAt: at.Add(-time.Hour), WrittenAt: at, LastHeartbeatStartedAt: at.Add(-time.Second)}, nil); err != nil {
				t.Fatal(err)
			}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPatch {
					w.WriteHeader(test.edit)
					_, _ = io.WriteString(w, test.body)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"id":"77"}`)
			}))
			defer api.Close()
			adapter := discord.New(discord.Options{ApplicationID: "1", PublicKey: public, Token: secrets.NewToken("bot-secret"), APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
			service := messaging.New(db, "discord", "2", []string{"3"}, adapter, messaging.FactoryStatus{DB: db})
			var logs bytes.Buffer
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			w := signed(t, adapter.Handler(service), private, at, body("10", "3", "2", "health"))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"type":5`) {
				t.Fatalf("ack: %d %s", w.Code, w.Body)
			}
			logs.Reset()
			if more, err := service.Step(ctx, at); err != nil || !more {
				t.Fatalf("step: %t %v", more, err)
			}
			lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
			if len(lines) != 1 {
				t.Fatalf("log lines: %q", logs.String())
			}
			var record struct {
				Msg, Outcome, Route string
				Status              int
				EditStatus          int    `json:"edit_status"`
				EditCode            int    `json:"edit_code"`
				EditMessage         string `json:"edit_message"`
			}
			if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
				t.Fatal(err)
			}
			if record.Msg != "chat reply delivery" || record.Route != test.route || record.Status != test.status || record.Outcome != test.outcome || record.EditStatus != test.edit || record.EditCode != test.code || record.EditMessage != test.message {
				t.Fatalf("log: %s", lines[0])
			}
			if strings.Contains(logs.String(), "interaction-secret") || strings.Contains(logs.String(), "/webhooks/") {
				t.Fatalf("log carries token or URL: %s", logs.String())
			}
		})
	}
}
