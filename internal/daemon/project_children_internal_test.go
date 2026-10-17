package daemon

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
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

// Discovery that first sees a graph Plan story resolves it through the
// parent's admitted versions, not the version current at discovery (#960).
func TestDiscoveryAdmitsGraphPlanStoryUnderParentVersions(t *testing.T) {
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
			if err := json.NewEncoder(w).Encode(map[string]any{"id": 702, "number": 2, "url": origin + r.URL.Path, "title": "story", "state": "open", "user": map[string]any{"id": 7, "login": "owner"}}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "code", RemoteURL: "git@github.com:acme/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "PARENT", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}
	if code, err = db.SetConnection(ctx, code, 0); err != nil {
		t.Fatal(err)
	}
	tickets := code
	tickets.Role = store.TicketConnection
	tickets.Destination = "acme/tickets"
	tickets.RemoteID = "202"
	if tickets, err = db.SetConnection(ctx, tickets, 0); err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "701"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "acme/tickets", Key: "1"}}
	if _, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	parent, err := db.AdmitSession(ctx, repo, ref, code.Revision, tickets.Revision, 99, "original-bot", "github:acme/tickets#1")
	if err != nil {
		t.Fatal(err)
	}
	if path, e := db.AdmitPlanPath(ctx, parent.ID); e != nil || path != "graph" {
		t.Fatalf("graph Plan path: %q %v", path, e)
	}
	if _, err = db.ReservePlanStory(ctx, parent.TaskRef, "story-001"); err != nil {
		t.Fatal(err)
	}
	if err = db.BindPlanStoryIssue(ctx, parent.TaskRef, "story-001", 702, 2); err != nil {
		t.Fatal(err)
	}
	// Another same-source pair becomes current before discovery sees the story.
	nextCode := code
	nextCode.CredentialEnv = "CURRENT"
	if _, err = db.SetConnection(ctx, nextCode, code.Revision); err != nil {
		t.Fatal(err)
	}
	nextTickets := tickets
	nextTickets.CredentialEnv = "CURRENT"
	current, err := db.SetConnection(ctx, nextTickets, tickets.Revision)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) {
		return "token", true
	}}, ExpectedActorID: 99})
	d := &Daemon{db: db, connections: factory, log: slog.New(slog.DiscardHandler)}
	q := &repoQueue{repo: repo, forgeRepo: github.Repo{Owner: "acme", Name: "tickets"}, log: d.log}
	issue := github.Issue{ID: 702, Number: 2, Title: "story", State: github.StateOpen, APIURL: origin + "/repos/acme/tickets/issues/2"}
	c, err := d.sight(ctx, q, issue)
	if err != nil || c == nil {
		t.Fatalf("story discovery: %+v %v", c, err)
	}
	snapshot, err := db.GetExecutionSnapshot(ctx, c.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TicketVersionID != tickets.ID || snapshot.CodeVersionID != code.ID || snapshot.TicketVersionID == current.ID {
		t.Fatalf("story pinned to current version: %+v", snapshot)
	}
	binding, err := db.GetTicketBinding(ctx, ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "702"})
	if err != nil || binding.Provenance != "managed-child" || binding.ConnectionVersionID != tickets.ID {
		t.Fatalf("story binding: %+v %v", binding, err)
	}
	again, err := d.sight(ctx, q, issue)
	if err != nil || again == nil || again.session.ID != c.session.ID {
		t.Fatalf("story rediscovery: %+v %v", again, err)
	}
}
