package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestQualifiedIngressPreservesOwnerCommandAndLegacySession(t *testing.T) {
	t.Parallel()
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo/issues/7" {
			t.Errorf("unexpected ingress request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := fmt.Fprintf(w, `{"id":700,"number":7,"url":%q,"state":"open","title":"ticket","body":"body"}`, base+"/repos/owner/repo/issues/7"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	base = server.URL
	client, err := github.New(github.Options{APIBase: base, Token: secrets.NewToken("offline-token")})
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := github.NewTickets(client)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
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
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", RemoteURL: "git@github.com:owner/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "trees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: ingress.Origin()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db, tickets: ingress, filter: filter}
	observed := github.Comment{ID: 10, APIURL: base + "/repos/owner/repo/issues/comments/10", IssueAPIURL: base + "/repos/owner/repo/issues/7", Author: github.User{ID: 42, Login: "renamed-owner"}, Body: "/implement continue"}
	qualified, err := d.qualifyReply(t.Context(), session.ID, observed)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := d.admitReply(t.Context(), session.ID, observed, qualified)
	if err != nil || !admitted.Triggers() {
		t.Fatalf("owner admission: %+v, %v", admitted, err)
	}
	command := turn.ParseCommand(admitted.Comment.Body)
	if command.Name != "/implement" || command.Text != "continue" {
		t.Fatalf("command changed: %+v", command)
	}
	retained, err := db.GetSession(t.Context(), session.ID)
	if err != nil || retained.TaskRef != session.TaskRef || retained.ID != session.ID {
		t.Fatal("qualified ingress changed historical session identity")
	}
	binding, err := db.GetSessionTicket(t.Context(), session.ID)
	if err != nil || binding.RemoteID != "700" {
		t.Fatalf("ticket mapping: %+v, %v", binding, err)
	}
	foreign := *qualified
	foreign.Author.Identity.Origin = ticket.Origin("https://other.example")
	if _, err := d.admitReply(t.Context(), session.ID, observed, &foreign); err == nil {
		t.Fatal("foreign author admitted")
	}
	observed.IssueAPIURL = base + "/repos/owner/repo/issues/8"
	if _, err := d.qualifyReply(t.Context(), session.ID, observed); err == nil {
		t.Fatal("reply on another ticket admitted")
	}
	if _, present, err := db.GetWatermark(t.Context(), session.ID); err != nil || present {
		t.Fatal("ingress consumed an unhandled command")
	}
}
