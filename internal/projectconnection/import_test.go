package projectconnection_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestLegacyImportRetainsHistoryAndExplicitReplacement(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
		case "/repos/owner/original":
			_, _ = w.Write([]byte(`{"id":91,"full_name":"owner/original","has_issues":true}`))
		case "/repos/owner/original/issues/9":
			_, _ = fmt.Fprintf(w, `{"id":909,"number":9,"url":"http://%s/repos/owner/original/issues/9","title":"Original","state":"open"}`, r.Host)
		default:
			t.Errorf("unexpected import request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/original", Forge: "github", Owner: "owner", Name: "original", RemoteURL: "git@github.com:owner/original.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/worktrees"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	explicit := store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "owner/replacement", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: false, Provenance: "explicit"}
	saved, err := db.SetConnection(t.Context(), explicit, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Providers.GitHub.APIBase = server.URL
	cfg.Providers.GitHub.TokenEnv = "TOKEN"
	cfg.Providers.GitHub.TokenKeychain = config.Keychain{}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-import-token", true }}})
	for range 2 {
		if err := projectconnection.Initialize(t.Context(), db, cfg, factory); err != nil {
			t.Fatal(err)
		}
	}
	current, err := db.CurrentConnection(t.Context(), repo.ID, store.TicketConnection)
	if err != nil || current.ID != saved.ID || current.Enabled {
		t.Fatalf("explicit config overwritten: %v %v", current, err)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetConnectionVersion(t.Context(), snapshot.TicketVersionID)
	if err != nil || retained.Destination != "owner/original" {
		t.Fatalf("original target: %v %v", retained, err)
	}
	after, err := db.GetSessionByTicket(t.Context(), ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), RemoteID: "909"})
	if err != nil || after.ID != session.ID || after.TaskRef != session.TaskRef {
		t.Fatalf("history changed: %v %v", after, err)
	}
	state, err := db.GetProjectImportState(t.Context(), repo.ID)
	if err != nil || state.Status != "complete" {
		t.Fatalf("import state %v %v", state, err)
	}
}

func TestLegacyImportBlocksContradictoryRegistration(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/clone", Forge: "github", Owner: "owner", Name: "original", RemoteURL: "git@github.com:owner/original.git"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	repo.Name = "replacement"
	repo.RemoteURL = "git@github.com:owner/replacement.git"
	if _, err := db.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	factory := projectconnection.NewFactory(projectconnection.Options{})
	if err := projectconnection.Initialize(t.Context(), db, cfg, factory); err != nil {
		t.Fatal(err)
	}
	state, err := db.GetProjectImportState(t.Context(), repo.ID)
	if err != nil || state.Status != "conflict" || state.Reason != "historical_destination_conflict" {
		t.Fatalf("contradictory evidence %v %v", state, err)
	}
	if _, err := db.GetExecutionSnapshot(t.Context(), session.ID); err == nil {
		t.Fatal("ambiguous historical target admitted")
	}
}

func TestLegacyImportBlocksQualifiedEffectContradiction(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/clone", Forge: "github", Owner: "owner", Name: "original", RemoteURL: "git@github.com:owner/original.git"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := ticket.Locator{Provider: ticket.GitHub, Origin: "https://other.example/api", Container: "owner/original", Key: "9"}
	if _, err := db.BindTicketEffect(t.Context(), "uncertain-plan-effect", conflicting, ticket.Identity{}); err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{})
	if err := projectconnection.Initialize(t.Context(), db, config.Defaults(), factory); err != nil {
		t.Fatal(err)
	}
	state, err := db.GetProjectImportState(t.Context(), repo.ID)
	if err != nil || state.Status != "conflict" || state.Reason != "historical_destination_conflict" {
		t.Fatalf("qualified effect contradiction %v %v", state, err)
	}
	if _, err := db.GetExecutionSnapshot(t.Context(), session.ID); err == nil {
		t.Fatal("contradictory saved effect admitted")
	}
	effect, err := db.GetTicketEffect(t.Context(), "uncertain-plan-effect")
	if err != nil || effect.Locator != conflicting || effect.RemoteID != "" {
		t.Fatalf("conflicting effect rewritten %v %v", effect, err)
	}
}

func TestLegacyImportRecordsIncompleteCredentialReference(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/original", Forge: "github", Owner: "owner", Name: "original"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Providers.GitHub.TokenEnv = ""
	cfg.Providers.GitHub.TokenKeychain = config.Keychain{Service: "external-service"}
	for range 2 {
		if err := projectconnection.Initialize(t.Context(), db, cfg, projectconnection.NewFactory(projectconnection.Options{})); err != nil {
			t.Fatal(err)
		}
		state, err := db.GetProjectImportState(t.Context(), repo.ID)
		if err != nil || state.Status != "conflict" || state.Reason != "incomplete_legacy_credential_reference" {
			t.Fatalf("incomplete import not retained: %+v %v", state, err)
		}
	}
}
