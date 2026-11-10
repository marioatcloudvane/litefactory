package projectconnection_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestResolveUsesCurrentConnectionAndRefusesNonHumanAndPublisher(t *testing.T) {
	kind, id := "User", int64(42)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"id":7,"login":"publisher","type":"User"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"id":%d,"login":"person","type":%q}`, id, kind)
	}))
	defer server.Close()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/resolve", Forge: "github", Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "o/tickets", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0); err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-test-token", true }}})
	service := projectconnection.ApproverService{DB: db, Factory: factory}

	got, err := service.Resolve(t.Context(), repo.ID, "person", 0)
	if err != nil || got.Actor.RemoteID != "42" || got.Actor.Origin != ticket.Origin(server.URL) || got.Handle != "person" {
		t.Fatalf("resolve %+v %v", got, err)
	}
	kind = "Bot"
	if _, err = service.Resolve(t.Context(), repo.ID, "person", 0); !errors.Is(err, projectconnection.ErrApproverNotHuman) {
		t.Fatalf("bot: %v", err)
	}
	kind, id = "User", 7
	if _, err = service.Resolve(t.Context(), repo.ID, "person", 0); !errors.Is(err, projectconnection.ErrApproverPublisher) {
		t.Fatalf("publisher: %v", err)
	}
	if _, err = service.Resolve(t.Context(), repo.ID, "bad/login", 0); !errors.Is(err, projectconnection.ErrApproverNotHuman) {
		t.Fatalf("malformed login: %v", err)
	}
}
