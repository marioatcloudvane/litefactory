package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestManagedChildKeepsDisabledParentConnections(t *testing.T) {
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			if _, err := w.Write([]byte(`{"id":99,"login":"original-bot"}`)); err != nil {
				t.Error(err)
			}
		case "/repos/acme/tickets":
			if _, err := w.Write([]byte(`{"id":202,"full_name":"acme/tickets","has_issues":true}`)); err != nil {
				t.Error(err)
			}
		case "/repos/acme/tickets/issues/2":
			if err := json.NewEncoder(w).Encode(map[string]any{"id": 702, "number": 2, "url": origin + r.URL.Path, "title": "child", "state": "open", "user": map[string]any{"id": 7, "login": "owner"}}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected replacement request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "code", RemoteURL: "git@github.com:acme/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "ORIGINAL", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}
	code, err = db.SetConnection(t.Context(), code, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets := code
	tickets.Role = store.TicketConnection
	tickets.Destination = "acme/tickets"
	tickets.RemoteID = "202"
	tickets, err = db.SetConnection(t.Context(), tickets, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "701"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "acme/tickets", Key: "1"}}
	if _, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	parent, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 99, "original-bot", "github:acme/tickets#1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(t.Context(), repo.ID, store.CodeConnection, code.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DisableConnection(t.Context(), repo.ID, store.TicketConnection, tickets.Revision); err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name != "ORIGINAL" {
			t.Errorf("replacement credential %s", name)
		}
		return "original-token", true
	}}, ExpectedActorID: 99})
	d := &Daemon{db: db, connections: factory}
	child, err := d.managedChildSession(t.Context(), repo, parent.TaskRef, 2, 702)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.GetExecutionSnapshot(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CodeVersionID != code.ID || snapshot.TicketVersionID != tickets.ID || snapshot.ActorID != 99 || snapshot.CodeTarget.Slug() != repo.Slug() {
		t.Fatalf("redirected child %+v", snapshot)
	}
	repeated, err := d.managedChildSession(t.Context(), repo, parent.TaskRef, 2, 702)
	if err != nil || repeated.ID != child.ID {
		t.Fatalf("child not repeatable %+v %v", repeated, err)
	}
	if _, err = d.managedChildSession(t.Context(), repo, parent.TaskRef, 2, 999); err == nil {
		t.Fatal("changed child identity accepted")
	}
	invalid := ref
	invalid.Identity.RemoteID = "703"
	invalid.Locator.Key = "3"
	invalid.Locator.Container = "replacement/tickets"
	if _, err = db.AdmitManagedChild(t.Context(), parent.ID, invalid, "github:replacement/tickets#3"); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("wrong retained child destination %v", err)
	}
}
