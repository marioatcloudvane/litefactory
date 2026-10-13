package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestImplementStatusRefreshRoutesThroughParentSnapshot(t *testing.T) {
	ctx := t.Context()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	parentRef := naming.Ref{Forge: "github", Owner: "acme", Repo: "tickets", Issue: 1}
	var mu sync.Mutex
	body := report.ImplementHandoffMarker(parentRef) + "\nOld status"
	edits := 0
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer original-token" {
			t.Errorf("request used another retained credential")
		}
		comment := func() map[string]any {
			return map[string]any{"id": 10, "body": body, "url": origin + "/repos/acme/tickets/issues/comments/10", "issue_url": origin + "/repos/acme/tickets/issues/1", "user": map[string]any{"id": 99, "login": "original-bot"}}
		}
		var response any
		switch {
		case r.URL.Path == "/user":
			response = map[string]any{"id": 99, "login": "original-bot"}
		case r.URL.Path == "/repos/acme/tickets/issues/1":
			response = map[string]any{"id": 701, "number": 1, "url": origin + r.URL.Path, "title": "parent", "state": "open", "user": map[string]any{"id": 42, "login": "owner"}}
		case r.URL.Path == "/repos/acme/tickets":
			response = map[string]any{"id": 202, "full_name": "acme/tickets", "has_issues": true}
		case r.URL.Path == "/repos/acme/tickets/issues/1/comments" && r.Method == http.MethodGet:
			response = []any{comment()}
		case r.URL.Path == "/repos/acme/tickets/issues/comments/10" && r.Method == http.MethodGet:
			response = comment()
		case r.URL.Path == "/repos/acme/tickets/issues/comments/10" && r.Method == http.MethodPatch:
			var in struct {
				Body string `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			body = in.Body
			edits++
			response = comment()
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	origin = server.URL
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "status.db"))
	check(err)
	defer db.Close()
	check(db.Migrate(ctx))
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "code", RemoteURL: "git@github.com:acme/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	check(err)
	code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "ORIGINAL", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	check(err)
	tickets := code
	tickets.ID = 0
	tickets.Revision = 0
	tickets.Role = store.TicketConnection
	tickets.Destination = "acme/tickets"
	tickets.RemoteID = "202"
	tickets, err = db.SetConnection(ctx, tickets, 0)
	check(err)
	admit := func(number, remote, actor int64, version store.ConnectionVersion) store.Session {
		t.Helper()
		ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: strconv.FormatInt(remote, 10)}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "acme/tickets", Key: strconv.FormatInt(number, 10)}}
		_, e := db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: version.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
		check(e)
		session, e := db.AdmitSession(ctx, repo, ref, code.Revision, version.Revision, actor, "original-bot", "github:acme/tickets#"+strconv.FormatInt(number, 10))
		check(e)
		return session
	}
	parent := admit(1, 701, 99, tickets)
	newer := tickets
	newer.CredentialEnv = "REPLACEMENT"
	newer, err = db.SetConnection(ctx, newer, tickets.Revision)
	check(err)
	worker := admit(2, 702, 100, newer)
	factory := projectconnection.NewFactory(projectconnection.Options{Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "ORIGINAL" {
			return "original-token", true
		}
		return "replacement-token", true
	}}})
	clients, err := factory.Clients(ctx, tickets, 99)
	check(err)
	bridge, err := turn.NewRoutedGitHubTicketWorkflow(db, clients.Tickets, 99, ProjectTicketSelector(db, factory))
	check(err)
	redactor := redact.New(redact.Options{Environ: []string{}})
	safe, err := redactor.Apply(ctx, "probe", 100)
	check(err)
	if err := bridge.EditComment(ctx, github.Repo{Owner: "acme", Name: "tickets"}, 10, safe); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("ambiguous container accepted: %v", err)
	}
	check(db.PutManagedFeature(ctx, store.ManagedFeature{ParentTask: parent.TaskRef, Generation: 1, State: "planning", WorkflowVersion: store.WorkflowSeparated}))
	var revisions []store.ApprovedStoryRevision
	for i, key := range []string{"story-001", "story-002"} {
		story := store.ManagedStory{ParentTask: parent.TaskRef, Generation: 1, Key: key, IssueNumber: 201 + i, IssueID: int64(7001 + i), State: "planning"}
		if i > 0 {
			story.Dependencies = []string{"story-001"}
		}
		check(db.PutManagedStory(ctx, story))
		revisions = append(revisions, store.ApprovedStoryRevision{ManagedStory: story, ParentID: 701, Title: key, Body: key, Hash: strings.Repeat("ab", 32), FetchedAt: time.Now()})
	}
	check(db.SetManagedFeatureReady(ctx, parent.TaskRef, 1, true))
	check(db.ApproveManagedStories(ctx, parent.TaskRef, 1, revisions))
	for _, r := range revisions {
		check(db.DeliverStoryScopeNotice(ctx, parent.TaskRef, 1, r.Key, "freeze"))
	}
	check(db.ActivateManagedScope(ctx, parent.TaskRef, 1))
	_, err = db.FreezeSessionSlug(ctx, parent.ID, "feature", "lf/feature")
	check(err)
	check(db.EnsureImplementStories(ctx, parent.TaskRef, 1, []store.ImplementStorySeed{{Key: "story-001", Branch: "lf/first"}, {Key: "story-002", Position: 1, Branch: "lf/second"}}, time.Now()))
	for _, states := range [][2]string{{store.ImplementWaiting, store.ImplementRunning}, {store.ImplementRunning, store.ImplementParked}} {
		row, e := db.GetImplementStory(ctx, parent.TaskRef, 1, "story-001")
		check(e)
		transition := store.ImplementTransition{Parent: parent.TaskRef, Generation: 1, Key: row.Key, From: states[0], To: states[1], ExpectedRevision: row.Revision, At: time.Now()}
		if states[1] == store.ImplementParked {
			transition.Reason = "Sandbox blocked"
		}
		_, e = db.TransitionImplementStory(ctx, transition)
		check(e)
	}
	opts := turn.Options{DB: db, SubIssueCreator: bridge, MCP: &turn.MCPRuntime{Paths: config.NewPaths(t.TempDir()), Redactor: redactor}}

	completed := make(chan error, 1)
	service, err := turn.NewImplementStatusService(ctx, turn.ImplementStatusOptions{
		Now:  time.Now,
		Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		Refresh: func(ctx context.Context, parent string, generation int64) (bool, error) {
			changed, e := turn.RefreshImplementParentStatus(ctx, opts, parent, generation)
			if e == nil && !changed {
				e = errors.New("background status did not edit")
			}
			completed <- e
			return changed, e
		},
	})
	check(err)
	defer service.Close()
	check(service.RefreshImplementStatus(turn.ExecutionContext(ctx, worker.ID), parent.TaskRef, 1, turn.ImplementStatusProgress))
	select {
	case err := <-completed:
		check(err)
	case <-time.After(10 * time.Second):
		t.Fatal("background status did not finish")
	}
	service.Close()
	mu.Lock()
	body = report.ImplementHandoffMarker(parentRef) + "\nOld status"
	mu.Unlock()
	changed, err := turn.RefreshImplementParentStatus(turn.ExecutionContext(ctx, worker.ID), opts, parent.TaskRef, 1)
	check(err)
	if !changed {
		t.Fatal("worker status did not edit")
	}

	mu.Lock()
	defer mu.Unlock()
	if edits != 2 || !strings.Contains(body, "Sandbox blocked") || !strings.Contains(body, "waiting for") {
		t.Fatalf("edits=%d body=%s", edits, body)
	}
}
