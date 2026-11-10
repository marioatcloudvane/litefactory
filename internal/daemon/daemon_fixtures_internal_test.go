package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// fixtureFilter names the fixture's owner and bot and refuses ordinary
// admission, which these fixtures never reach.
type fixtureFilter struct{}

func (fixtureFilter) Identities() turn.Identities {
	return turn.Identities{OwnerUserID: 7, BotUserID: 99}
}
func (fixtureFilter) Admit(context.Context, int64, github.Comment) (turn.Admission, error) {
	return turn.Admission{}, errors.New("normal admission must not run")
}

// fixtureForge is a small forge seam: one issue, posted and seen comments.
type fixtureForge struct {
	Forge
	posts   []github.Comment
	postErr error
	seen    []github.Comment
	issue   github.Issue
}

func (f *fixtureForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.issue, nil
}
func (f *fixtureForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	return github.CommentPage{Comments: f.seen}, nil
}
func (f *fixtureForge) ListCommentsAfter(_ context.Context, _ github.Repo, _ int, after int64) ([]github.Comment, error) {
	var out []github.Comment
	for _, c := range f.seen {
		if c.ID > after {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fixtureForge) PostComment(_ context.Context, repo github.Repo, issue int, body redact.Body) (github.Comment, error) {
	c := github.Comment{ID: int64(100 + len(f.posts)), Body: body.String(), Author: github.User{ID: 99}}
	c.HTMLURL = fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", repo.Owner, repo.Name, issue, c.ID)
	f.posts = append(f.posts, c)
	return c, f.postErr
}

// daemonFixture is a migrated store, one registered repository and a daemon
// with no workers running.
func daemonFixture(t *testing.T) (*Daemon, *fixtureForge, store.Repo) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n")); err != nil {
		t.Fatal(err)
	}
	forge := &fixtureForge{issue: github.Issue{ID: 10406, Number: 406, State: "closed"}}
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	d := &Daemon{instanceID: "live-fixture", db: db, forge: forge, filter: fixtureFilter{}, log: slog.New(slog.DiscardHandler), pool: newPool(1), now: func() time.Time { return at }}
	d.discovery.tick(at)
	return d, forge, repo
}

type projectPublication struct {
	mu          sync.Mutex
	posts       int
	publisherID int64
	comments    []map[string]any
	loseReceipt bool
}

type projectFixture struct {
	d           *Daemon
	repo        store.Repo
	connection  store.ConnectionVersion
	appointment store.ApproverAppointment
	session     store.Session
	issue       github.Issue
	source      github.Comment
	publication *projectPublication
	httpClient  *http.Client
}

func newProjectFixture(t *testing.T, savedSession bool) projectFixture {
	t.Helper()
	d, _, repo := daemonFixture(t)
	origin := ticket.Origin("https://api.github.com")
	connection, err := d.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "FIXTURE_PROJECT", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	code := connection
	connection.Role = store.TicketConnection
	connection.Destination = "team/tickets"
	connection.RemoteID = "202"
	connection, err = d.db.SetConnection(t.Context(), connection, 0)
	if err != nil {
		t.Fatal(err)
	}
	appointment, err := d.db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "42"}, Handle: "ticket-approver", VerifiedAt: time.Now().UTC(), Provenance: "explicit"}, connection.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	issue := github.Issue{ID: 10406, Number: 406, State: github.StateOpen}
	fresh := appointment.ActivatedAt.Add(time.Minute)
	source := github.Comment{ID: 100, Author: github.User{ID: 42, Login: "renamed-approver"}, Body: "explain the stalled work", CreatedAt: fresh, UpdatedAt: fresh, APIURL: "https://api.github.com/repos/team/tickets/issues/comments/100", IssueAPIURL: "https://api.github.com/repos/team/tickets/issues/406"}
	publication := &projectPublication{publisherID: 99}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch r.URL.Path {
		case "/user":
			publication.mu.Lock()
			value = map[string]any{"id": publication.publisherID, "login": "publisher", "type": "Bot"}
			publication.mu.Unlock()
		case "/repos/team/tickets":
			value = map[string]any{"id": 202, "full_name": "team/tickets", "has_issues": true}
		case "/repos/team/tickets/issues/406":
			value = map[string]any{"id": 10406, "number": 406, "url": "https://api.github.com/repos/team/tickets/issues/406", "state": "open", "user": map[string]any{"id": 55, "login": "creator"}}
		case "/repos/team/tickets/issues/406/comments":
			publication.mu.Lock()
			defer publication.mu.Unlock()
			if r.Method == http.MethodPost {
				var input struct{ Body string }
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				publication.posts++
				comment := map[string]any{"id": 500, "body": input.Body, "url": "https://api.github.com/repos/team/tickets/issues/comments/500", "issue_url": "https://api.github.com/repos/team/tickets/issues/406", "user": map[string]any{"id": 99, "login": "publisher", "type": "Bot"}}
				publication.comments = append(publication.comments, comment)
				w.WriteHeader(http.StatusCreated)
				value = comment
				if publication.loseReceipt {
					value = map[string]any{"id": 0}
				}
			} else {
				value = publication.comments
			}
		case "/repos/team/tickets/issues/comments":
			value = []map[string]any{{"id": source.ID, "body": source.Body, "url": source.APIURL, "issue_url": source.IssueAPIURL, "created_at": fresh.Format(time.RFC3339Nano), "updated_at": fresh.Format(time.RFC3339Nano), "user": map[string]any{"id": 42, "login": "ticket-approver", "type": "User"}}}
		default:
			t.Errorf("unexpected provider destination %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: approverLoopbackTransport{target: target}}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, HTTPClient: httpClient, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	clients, err := factory.Clients(t.Context(), connection, 99)
	if err != nil {
		t.Fatal(err)
	}
	filter, err := turn.NewFilter(d.db, turn.Identities{OwnerUserID: 7, BotUserID: 99}, d.log)
	if err != nil {
		t.Fatal(err)
	}
	d.connections, d.tickets, d.filter, d.forge = factory, clients.Tickets, filter, clients.Code
	var session store.Session
	if savedSession {
		ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "10406"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: "team/tickets", Key: "406"}}
		if _, err = d.db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
			t.Fatal(err)
		}
		session, err = d.db.AdmitSession(t.Context(), repo, ref, code.Revision, connection.Revision, 99, "publisher", "github:team/tickets#406")
		if err != nil {
			t.Fatal(err)
		}
	}
	return projectFixture{d: d, repo: repo, connection: connection, appointment: appointment, session: session, issue: issue, source: source, publication: publication, httpClient: httpClient}
}
