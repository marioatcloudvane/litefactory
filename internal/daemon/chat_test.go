package daemon_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/webhook"
)

func TestChatRegistrationDoesNotBlockHeartbeatOrGitHub(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan struct{}, 1)
	canceled := make(chan struct{})
	release := make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		select {
		case requested <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer func() { close(release); api.Close() }()
	w.daemon = w.newDaemon(daemon.Options{
		DiscoveryInterval: scheduleInterval, Ticks: w.ticks,
		Webhook: &daemon.WebhookOptions{Listener: w.ln, Secrets: secrets.Resolver{Keychain: w.spy.lookup}, Now: w.clock.peek},
		Chat:    &daemon.ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", PublicKey: hex.EncodeToString(public), OwnerID: "2", ChannelIDs: []string{"3"}}, Token: secrets.NewToken("discord-test-token"), APIURL: api.URL, Client: api.Client()},
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- w.daemon.Run(ctx) }()
	defer cancel()
	select {
	case <-requested:
	case <-time.After(10 * time.Second):
		t.Fatal("chat registration never began")
	}
	// Registration remains blocked. A real heartbeat still completes discovery.
	if _, err := w.daemon.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	body := issuePayload(42)
	reply := w.post("issues", "chat-github-path", webhook.Sign([]byte(webhookTestSecret), body), body)
	if reply != http.StatusAccepted {
		t.Fatalf("GitHub webhook returned %d", reply)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not stop")
	}
	select {
	case <-canceled:
	case <-time.After(10 * time.Second):
		t.Fatal("chat request was not canceled")
	}
}

// An enabled chat whose bot token cannot be resolved leaves only chat
// unavailable: the daemon starts, GitHub work proceeds, status reports the
// reason, and a token stored later starts chat on a later heartbeat.
func TestChatWithUnresolvableTokenStartsLaterWithoutRestart(t *testing.T) {
	w := newWebhookFixture(t)
	w.plantCursor()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registered := make(chan struct{}, 1)
	api := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/commands") {
			select {
			case registered <- struct{}{}:
			default:
			}
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write([]byte("[]"))
	}))
	defer api.Close()
	const secret = "discord-late-token"
	var stored atomic.Bool
	var attempts atomic.Int32
	resolve := func(context.Context) (secrets.Token, error) {
		attempts.Add(1)
		if !stored.Load() {
			return secrets.Token{}, errors.New("keychain item not found")
		}
		return secrets.NewToken(secret), nil
	}
	w.daemon = w.newDaemon(daemon.Options{
		DiscoveryInterval: scheduleInterval, Ticks: w.ticks,
		Logger:  slog.New(slog.NewJSONHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Webhook: &daemon.WebhookOptions{Listener: w.ln, Secrets: secrets.Resolver{Keychain: w.spy.lookup}, Now: w.clock.peek},
		Chat: &daemon.ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", PublicKey: hex.EncodeToString(public), OwnerID: "2", ChannelIDs: []string{"3"}},
			Resolve: resolve, TokenSetting: "chat.discord.token_keychain", APIURL: api.URL, Client: api.Client()},
	})
	records := dispatchRecords(t, w.logs, "chat.discord bot token could not be resolved; chat unavailable, retrying on a later heartbeat")
	if len(records) != 1 || records[0]["level"] != "ERROR" || records[0]["setting"] != "chat.discord.token_keychain" {
		t.Fatalf("startup token error not logged at error level with its setting: %v", records)
	}
	stop := w.run()
	defer func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	}()
	discordStatus := func() int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+w.ln.Addr().String()+discord.Path, bytes.NewReader([]byte("{}")))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	beatUntil := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !done() {
			if time.Now().After(deadline) {
				t.Fatalf("%s never happened", what)
			}
			select {
			case w.ticks <- time.Now():
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	beatUntil("an unavailable chat status and a retried resolution", func() bool {
		return strings.HasPrefix(w.health().Listener.ChatStatus, "unavailable: ") && attempts.Load() >= 2
	})
	if got := w.health().Listener.ChatStatus; !strings.Contains(got, "chat.discord.token_keychain") {
		t.Fatalf("chat status lacks the setting: %q", got)
	}
	if code := discordStatus(); code != http.StatusServiceUnavailable {
		t.Fatalf("Discord path before the token returned %d", code)
	}
	body := issuePayload(42)
	if reply := w.post("issues", "chat-unavailable-github", webhook.Sign([]byte(webhookTestSecret), body), body); reply != http.StatusAccepted {
		t.Fatalf("GitHub webhook returned %d", reply)
	}

	stored.Store(true)
	beatUntil("command registration after the token appeared", func() bool {
		select {
		case <-registered:
			return true
		default:
			return false
		}
	})
	beatUntil("an available chat status", func() bool { return w.health().Listener.ChatStatus == "available" })
	if code := discordStatus(); code == http.StatusServiceUnavailable || code == http.StatusNotFound {
		t.Fatalf("Discord path after the token returned %d", code)
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Fatal("the bot token was logged")
	}
}

func TestChatWarnsWhenNoticesShareConversationChannel(t *testing.T) {
	for _, channel := range []string{"", "3", "4"} {
		t.Run("notice_"+channel, func(t *testing.T) {
			w := newWebhookFixture(t)
			public, _, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			w.daemon = w.newDaemon(daemon.Options{
				Logger:  slog.New(slog.NewJSONHandler(w.logs, nil)),
				Webhook: &daemon.WebhookOptions{Listener: w.ln, Secrets: secrets.Resolver{Keychain: w.spy.lookup}, Now: w.clock.peek},
				Chat:    &daemon.ChatOptions{Settings: config.DiscordChat{ApplicationID: "1", PublicKey: hex.EncodeToString(public), OwnerID: "2", ChannelIDs: []string{"3"}, NoticeChannelID: channel}, Token: secrets.NewToken("fixture-token"), Conversations: &messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{{Channel: "3"}}}},
			})
			records := dispatchRecords(t, w.logs, "Discord notices and conversation share a channel; separate notices may interleave with replies")
			if channel == "3" {
				if len(records) != 1 || records[0]["channel"] != "3" || records[0]["level"] != "WARN" {
					t.Fatalf("warning missing: %v", records)
				}
			} else if len(records) != 0 {
				t.Fatalf("unexpected warning: %v", records)
			}
		})
	}
}
