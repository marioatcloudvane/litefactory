package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

type remoteControlFixture struct{ db *store.DB }

func (c remoteControlFixture) Ready(context.Context) error { return nil }
func (c remoteControlFixture) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.db.ListConnectorGrants(ctx, 42, time.Now())
}
func (c remoteControlFixture) Revoke(ctx context.Context, id string) error {
	return c.db.RevokeConnectorGrant(ctx, 42, id, time.Now())
}
func (c remoteControlFixture) RevokeAll(ctx context.Context) error {
	return c.db.RevokeAllConnectorGrants(ctx, 42, time.Now())
}

func remoteSetupFixture(t *testing.T) Setup {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "connector.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.RemoteConnector.Enabled = true
	cfg.RemoteConnector.OwnerID = 42
	cfg.RemoteConnector.GitHubClientID = "dedicated-app"
	cfg.RemoteConnector.GitHubSecretRef.Env = "TEST_APP"
	cfg.RemoteConnector.CookieSecretRef.Env = "TEST_COOKIE"
	control, err := connector.NewLocalControl(t.Context(), db, remoteControlFixture{db})
	if err != nil {
		t.Fatal(err)
	}
	resolver := secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		switch name {
		case "TEST_APP":
			return "github-secret-marker", true
		case "TEST_COOKIE":
			return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)), true
		default:
			return "", false
		}
	}}
	return Setup{DB: db, Config: &cfg, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), ConnectorControl: control, ConnectionSecrets: resolver}
}

func TestRemoteConnectorPreparationFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []string{"disabled", "no control", "missing secret", "bad key", "launchd env", "owner mismatch", "public listener"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := remoteSetupFixture(t)
			switch name {
			case "disabled":
				s.Config.RemoteConnector.Enabled = false
				s.ConnectorControl = nil
			case "no control":
				s.ConnectorControl = nil
			case "missing secret":
				s.ConnectionSecrets.LookupEnv = func(string) (string, bool) { return "", false }
			case "bad key":
				s.ConnectionSecrets.LookupEnv = func(string) (string, bool) { return "secret-marker", true }
			case "launchd env":
				s.Launchd = true
			case "owner mismatch":
				s.Config.RemoteConnector.OwnerID = 43
			case "public listener":
				s.Config.RemoteConnector.ListenAddr = "0.0.0.0:8788"
			}
			runtime, err := prepareRemoteConnector(t.Context(), s)
			if runtime != nil {
				defer runtime.listener.Close()
				t.Fatal("invalid setup bound listener")
			}
			if name == "disabled" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid setup accepted")
			}
			if err != nil && strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("startup failure leaked secret")
			}
		})
	}
}

func TestRemoteConnectorListenerLifecycle(t *testing.T) {
	t.Parallel()
	s := remoteSetupFixture(t)
	// Reserve a random numeric loopback port; a taken port must fail startup.
	var lc net.ListenConfig
	occupied, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Config.RemoteConnector.ListenAddr = occupied.Addr().String()
	if runtime, err := prepareRemoteConnector(t.Context(), s); err == nil || runtime != nil {
		_ = occupied.Close()
		t.Fatal("occupied listener accepted")
	}
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := prepareRemoteConnector(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.listener.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.server.Serve(ctx, runtime.listener) }()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+runtime.listener.Addr().String()+"/.well-known/oauth-protected-resource/litefactory/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "cloudvane.at"
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("discovery HTTP %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("connector did not stop after cancellation")
	}
	// Close must be idempotent after Serve's bounded shutdown closes the listener.
	d := &Daemon{remoteConnector: runtime}
	if err := d.closeRemoteConnector(); err != nil {
		t.Fatal(err)
	}
}
