package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func projectStartupDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "startup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleBot, Handle: "worker", UserID: 7, Source: store.SourceForgeAPI}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProjectStartupSkipsUnavailableCredentialsAndChangedActors(t *testing.T) {
	t.Setenv("XPC_SERVICE_NAME", "")
	db := projectStartupDB(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/user" {
			t.Errorf("startup performed unexpected operation %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		id := int64(7)
		if r.Header.Get("Authorization") == "Bearer changed-actor-token" {
			id = 99
		}
		_, _ = fmt.Fprintf(w, `{"id":%d,"login":"worker"}`, id)
	}))
	defer server.Close()
	var expectedVersion store.ConnectionVersion
	for _, name := range []string{"a-changed", "b-missing", "c-usable"} {
		repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/" + name, Forge: "github", Owner: "owner", Name: name})
		if err != nil {
			t.Fatal(err)
		}
		saved, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "owner/" + name, CredentialSource: "keychain", CredentialService: name, CredentialAccount: "worker", Enabled: true, Provenance: "explicit"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if name == "c-usable" {
			expectedVersion = saved
		}
	}
	var logs bytes.Buffer
	env := Env{Logger: slog.New(slog.NewTextHandler(&logs, nil)), Keychain: func(_ context.Context, service, _ string) (string, error) {
		switch service {
		case "a-changed":
			return "changed-actor-token", nil
		case "b-missing":
			return "", errors.New("raw-error-containing-hidden-secret")
		default:
			return "usable-project-token", nil
		}
	}}
	cfg := config.Defaults()
	cfg.Providers.GitHub.TokenEnv = "OBSOLETE_GLOBAL_TOKEN"
	legacyErr := errors.New("obsolete global credential unavailable")
	token, selected, err := resolveProjectStartupToken(t.Context(), &env, db, &cfg, legacyErr)
	if err != nil || token.Empty() || selected.ID != expectedVersion.ID || selected.Origin != ticket.Origin(server.URL) {
		t.Fatalf("project fallback selected wrong credential or origin: version %d, error %v", selected.ID, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("authenticated requests=%d", requests.Load())
	}
	identity, err := db.GetIdentity(t.Context(), store.ForgeGitHub, store.RoleBot)
	if err != nil || identity.UserID != 7 {
		t.Fatalf("stored actor changed: %v %v", identity, err)
	}
	if logs.Len() != 0 {
		t.Fatalf("startup emitted credential diagnostics: %q", logs.String())
	}
	if cfg.Providers.GitHub.TokenEnv != "OBSOLETE_GLOBAL_TOKEN" {
		t.Fatal("startup rewrote global config")
	}
}

func TestProjectStartupLaunchdRefusesEnvironmentAndPreservesLegacyError(t *testing.T) {
	t.Setenv("XPC_SERVICE_NAME", launchd.DefaultLabel)
	t.Setenv("LF_TEST_PROJECT_STARTUP_TOKEN", "do-not-use-background-env")
	db := projectStartupDB(t)
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
	}))
	defer server.Close()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/project", Forge: "github", Owner: "owner", Name: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "owner/project", CredentialSource: "env", CredentialEnv: "LF_TEST_PROJECT_STARTUP_TOKEN", Enabled: true, Provenance: "explicit"}, 0); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	legacyErr := errors.New("obsolete credential missing")
	token, selected, err := resolveProjectStartupToken(t.Context(), &Env{}, db, &cfg, legacyErr)
	if token.Empty() == false || selected.ID != 0 || !errors.Is(err, legacyErr) {
		t.Fatalf("background env accepted: selected %d error %v", selected.ID, err)
	}
	if requests.Load() != 0 {
		t.Fatal("environment credential reached background transport")
	}
}
