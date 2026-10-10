package daemon_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
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
