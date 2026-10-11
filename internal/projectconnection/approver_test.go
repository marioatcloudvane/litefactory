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

func TestApproverVerificationAndImmutableRefresh(t *testing.T) {
	accountID := int64(42)
	accountType := "User"
	status := http.StatusOK
	login := "approver"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"id":7,"login":"publisher","type":"User"}`))
			return
		}
		if r.URL.Path != "/users/approver" && r.URL.Path != "/user/42" {
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"id":%d,"login":%q,"type":%q,"message":"opaque-provider-diagnostic"}`, accountID, login, accountType)
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
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/approver", Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "ticket-owner/tickets", CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-test-token", true }}})
	service := projectconnection.ApproverService{DB: db, Factory: factory}
	a, err := service.Set(t.Context(), repo.ID, "approver", v.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.Actor.RemoteID != "42" || a.Actor.Origin != ticket.Origin(server.URL) || a.Handle != "approver" {
		t.Fatalf("appointment %+v", a)
	}
	login = "renamed"
	refreshed, err := service.Refresh(t.Context(), repo.ID, v.Revision, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ID != a.ID || refreshed.Revision != a.Revision || !refreshed.ActivatedAt.Equal(a.ActivatedAt) || refreshed.Handle != "renamed" {
		t.Fatalf("refresh changed authority: %+v", refreshed)
	}
	for _, tc := range []struct {
		name, kind string
		id         int64
		status     int
		want       error
	}{
		{"bot", "Bot", 8, http.StatusOK, projectconnection.ErrApproverNotHuman},
		{"organization", "Organization", 8, http.StatusOK, projectconnection.ErrApproverNotHuman},
		{"unknown-type", "", 8, http.StatusOK, projectconnection.ErrApproverNotHuman},
		{"publisher", "User", 7, http.StatusOK, projectconnection.ErrApproverPublisher},
		{"missing", "User", 8, http.StatusNotFound, nil},
		{"transient", "User", 8, http.StatusServiceUnavailable, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accountID = tc.id
			accountType = tc.kind
			status = tc.status
			if _, e := service.Set(t.Context(), repo.ID, "approver", v.Revision, a.Revision); e == nil || (tc.want != nil && !errors.Is(e, tc.want)) {
				t.Fatalf("verification error %v", e)
			}
			retained, e := db.CurrentApprover(t.Context(), repo.ID)
			if e != nil || retained.ID != a.ID || retained.Actor != a.Actor {
				t.Fatalf("failed verification changed authority %+v %v", retained, e)
			}
		})
	}
	if _, err = service.Set(t.Context(), repo.ID, "approver", v.Revision+1, a.Revision); !errors.Is(err, store.ErrConnectionConflict) {
		t.Fatalf("stale connection accepted %v", err)
	}
}

func TestNewProjectDoesNotInheritGlobalApprover(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/new-project", Forge: "github", Owner: "owner", Name: "new-project", RemoteURL: "git@github.com:owner/new-project.git"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "global-owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	// No factory is needed: migration captured eligibility, not current rows or
	// seed-connection provenance, decides whether legacy authority can be imported.
	if err = projectconnection.InitializeApprovers(t.Context(), db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CurrentApprover(t.Context(), repo.ID); !errors.Is(err, store.ErrApproverMissing) {
		t.Fatalf("new project inherited owner: %v", err)
	}
}
