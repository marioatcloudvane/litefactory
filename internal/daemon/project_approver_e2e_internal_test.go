package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// TestProjectApproverWorkflowEndToEnd crosses real HTTP, project management,
// factory admission, exact Plan approval and a SQLite restart. The code owner,
// ticket author, assignee, publisher and appointed approver are distinct.
func TestProjectApproverWorkflowEndToEnd(t *testing.T) {
	const approverID, codeOwnerID, publisherID = int64(42), int64(7), int64(99)
	var origin string
	replies := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		write := func(v any) {
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/user":
			write(map[string]any{"id": publisherID, "login": "factory", "type": "Bot"})
		case "/users/replacement":
			write(map[string]any{"id": 43, "login": "replacement", "type": "User"})
		case "/users/approver", "/user/42":
			write(map[string]any{"id": approverID, "login": "approver", "type": "User"})
		case "/repos/code-owner/code":
			write(map[string]any{"id": 101, "full_name": "code-owner/code", "has_issues": true})
		case "/repos/team/tickets":
			write(map[string]any{"id": 202, "full_name": "team/tickets", "has_issues": true})
		case "/repos/team/tickets/issues/1":
			write(map[string]any{"id": 701, "number": 1, "url": origin + r.URL.Path, "title": "Third-party ticket", "body": "Context from a third party", "state": "open", "user": map[string]any{"id": 55, "login": "creator"}, "assignees": []map[string]any{{"id": 56, "login": "assignee"}}})
		case "/repos/team/tickets/issues/1/comments":
			write(replies)
		default:
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	path := filepath.Join(t.TempDir(), "approver.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: ticket.Origin(origin), Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "APPROVER_E2E", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	connection := code
	connection.Role = store.TicketConnection
	connection.Destination = "team/tickets"
	connection.RemoteID = "202"
	connection, err = db.SetConnection(t.Context(), connection, 0)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: publisherID, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name != "APPROVER_E2E" {
			t.Errorf("unexpected credential %s", name)
		}
		return "offline-token", true
	}}})
	management := projectconnection.ApproverService{DB: db, Factory: factory}
	appointment, err := management.Set(t.Context(), repo.ID, "approver", connection.Revision, 0)
	if err != nil {
		t.Fatalf("operator set: %v", err)
	}
	inspected, err := management.Inspect(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Appointment.ID != appointment.ID || inspected.Appointment.Actor.RemoteID != strconv.FormatInt(approverID, 10) || inspected.Appointment.Actor.Origin != ticket.Origin(origin) {
		t.Fatalf("qualified appointment not inspectable: %+v", inspected)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: ticket.Origin(origin), RemoteID: "701"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: ticket.Origin(origin), Container: "team/tickets", Key: "1"}}
	if _, err := db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := AdmitProjectTicket(t.Context(), db, factory, repo, ref)
	if err != nil {
		t.Fatalf("factory ticket admission: %v", err)
	}
	clients, err := factory.Clients(t.Context(), connection, publisherID)
	if err != nil {
		t.Fatal(err)
	}
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: codeOwnerID, BotUserID: publisherID, TicketOrigin: ticket.Origin(origin)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{db: db, connections: factory, tickets: clients.Tickets, filter: filter}
	fresh := time.Now().UTC().Add(time.Minute)
	reply := func(id, actor int64, body string, created time.Time) map[string]any {
		return map[string]any{"id": id, "url": fmt.Sprintf("%s/repos/team/tickets/issues/comments/%d", origin, id), "issue_url": origin + "/repos/team/tickets/issues/1", "body": body, "created_at": created.Format(time.RFC3339Nano), "updated_at": created.Format(time.RFC3339Nano), "user": map[string]any{"id": actor, "login": "display-only", "type": "User"}}
	}
	replies = []map[string]any{reply(10, codeOwnerID, "/implement unauthorized", fresh), reply(11, approverID, "/implement build the story", fresh), reply(13, 55, "/implement creator", fresh), reply(14, 56, "/implement assignee", fresh), reply(15, publisherID, "/implement self", fresh)}
	admit := func(c github.Comment) turn.Admission {
		t.Helper()
		q, e := daemon.qualifyReply(t.Context(), session.ID, c)
		if e != nil {
			t.Fatal(e)
		}
		a, e := daemon.admitReply(t.Context(), session.ID, c, q)
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	comments, err := daemon.readComments(t.Context(), github.Repo{Owner: "team", Name: "tickets"}, 1, 0)
	if err != nil || len(comments) != 5 {
		t.Fatalf("read instructions: %+v %v", comments, err)
	}
	if a := admit(comments[0]); a.Triggers() {
		t.Fatal("code owner gained ticket instruction authority")
	}
	if a := admit(comments[1]); !a.Triggers() {
		t.Fatalf("fresh approver instruction rejected: %+v", a)
	}
	for _, c := range comments[2:] {
		if admit(c).Triggers() {
			t.Fatalf("creator, assignee or publisher gained authority: %d", c.Author.ID)
		}
	}
	// Edits cannot make an older comment fresh. Equal and missing creation
	// timestamps also require a newly authored reply.
	old := reply(20, approverID, "/implement stale", appointment.ActivatedAt.Add(-time.Second))
	old["updated_at"] = fresh.Format(time.RFC3339Nano)
	equal := reply(21, approverID, "/implement equal-time", appointment.ActivatedAt)
	missing := reply(22, approverID, "/implement missing-time", fresh)
	delete(missing, "created_at")
	replies = append(replies, old, equal, missing)
	comments, err = daemon.readComments(t.Context(), github.Repo{Owner: "team", Name: "tickets"}, 1, 11)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range comments {
		if c.ID >= 20 && admit(c).Triggers() {
			t.Fatalf("stale creation evidence became authority: %d", c.ID)
		}
	}
	refreshed, err := management.Refresh(t.Context(), repo.ID, connection.Revision, appointment.Revision)
	if err != nil || refreshed.ID != appointment.ID || refreshed.Revision != appointment.Revision || !refreshed.ActivatedAt.Equal(appointment.ActivatedAt) {
		t.Fatalf("same-account refresh changed replay boundary: %+v %v", refreshed, err)
	}
	state := planbrief.PlanState{SharedRevision: "shared-1"}
	draft := planbrief.PlanDraft{Key: "story-003", Name: "Appoint an approver", UserStory: planbrief.UserStoryContent{Actor: "project owner", Want: "verified authority", Benefit: "safe work"}, Problem: "Roles differ", InScope: []string{"Ticket authority"}, NotInScope: []string{"Code permissions"}}
	if err = state.PutCandidate(draft, "story", "candidate-1"); err != nil {
		t.Fatal(err)
	}
	if err = state.OpenPlanWait("story-003", "review-1", "review", 701); err != nil {
		t.Fatal(err)
	}
	revision, err := db.SavePlanWorkflowState(t.Context(), session.TaskRef, 0, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.Set(t.Context(), repo.ID, "replacement", connection.Revision, appointment.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("replacement ignored outstanding owner review: %v", err)
	}
	replies = append(replies, reply(12, approverID, "/approve-plan story-003 story candidate-1", fresh.Add(time.Second)))
	comments, err = daemon.readComments(t.Context(), github.Repo{Owner: "team", Name: "tickets"}, 1, 11)
	if err != nil {
		t.Fatal(err)
	}
	var approval github.Comment
	for _, c := range comments {
		if c.ID == 12 {
			approval = c
		}
	}
	if a := admit(approval); !a.Triggers() {
		t.Fatalf("exact approver reply rejected: %+v", a)
	}
	promoted, err := state.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: approval.ID, OwnerID: approverID, ActorID: approval.Author.ID, IssueID: 701, Key: "story-003", Stage: "story", Candidate: "candidate-1", WaitID: "review-1", Body: approval.Body})
	if err != nil || !promoted {
		t.Fatalf("exact workflow approval: %v %v", promoted, err)
	}
	if _, err = db.SavePlanWorkflowState(t.Context(), session.TaskRef, revision, state); err != nil {
		t.Fatal(err)
	}
	intent := store.ReportDelivery{Key: "approver-effect-1", SessionID: session.ID, TriggerID: 12, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "forge", Purpose: "reply", BotUserID: publisherID, Body: "<!-- lf:delivery key=approver-effect-1 -->\nApproved by the original approver", Marker: "<!-- lf:delivery key=approver-effect-1 -->"}
	if _, err = db.EnsureReportDelivery(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	if claimed, err := db.MarkReportDeliverySent(t.Context(), intent.Key); err != nil || !claimed {
		t.Fatalf("publication intent not claimed: %v %v", claimed, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	db, err = store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err := db.GetPlanWorkflowState(t.Context(), session.TaskRef)
	if err != nil || len(saved.Stories) != 1 || saved.Stories[0].Stage != "technical" || saved.Stories[0].Approvals["story"] == "" || len(saved.ConsumedReplies) != 1 || saved.ConsumedReplies[0] != 12 {
		t.Fatalf("approval not preserved across restart: %+v %v", saved, err)
	}
	management.DB = db
	persisted, err := management.Inspect(t.Context(), repo.ID)
	if err != nil || persisted.Appointment.ID != appointment.ID {
		t.Fatalf("appointment not preserved across restart: %+v %v", persisted, err)
	}
	retained, err := db.GetReportDelivery(t.Context(), intent.Key)
	if err != nil || retained.Body != intent.Body || retained.Marker != intent.Marker || retained.BotUserID != publisherID || !retained.PossiblySent || retained.Completed {
		t.Fatalf("restart changed uncertain original effect: %+v %v", retained, err)
	}
	foundPublication := false
	for _, blocker := range persisted.Blockers {
		if blocker.Kind == "report_publication" && blocker.Key == intent.Key {
			foundPublication = true
		}
	}
	if !foundPublication {
		t.Fatalf("uncertain effect missing from blocker inspection: %+v", persisted.Blockers)
	}
	if _, err := management.Set(t.Context(), repo.ID, "replacement", connection.Revision, appointment.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("restart allowed replacement across unresolved effect: %v", err)
	}
	t.Log("verified operator appointment, fresh instruction, exact story approval and restart preserved; code owner, ticket creator and assignee have no appointment authority")
}

// Code controls retain their separately qualified authority when the project
// has no usable ticket approver; appointment is not a delivery prerequisite.
func TestProjectCodeAuthorityWithoutTicketAppointment(t *testing.T) {
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "code-authority.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", RemoteURL: "git@github.com:owner/repo.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code, err := db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "CODE_E2E", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
	if err != nil {
		t.Fatal(err)
	}
	connection := code
	connection.Role = store.TicketConnection
	connection, err = db.SetConnection(t.Context(), connection, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: connection.Origin, RemoteID: "701"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: connection.Origin, Container: repo.Slug(), Key: "1"}}
	if _, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: connection.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, connection.Revision, 99, "publisher", "github:owner/repo#1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 7, BotUserID: 99}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{db: db, connections: projectconnection.NewFactory(projectconnection.Options{}), filter: filter}
	if _, err = db.CurrentApprover(t.Context(), repo.ID); !errors.Is(err, store.ErrApproverMissing) {
		t.Fatalf("fixture unexpectedly appointed an approver: %v", err)
	}
	for _, tc := range []struct {
		name    string
		author  github.User
		body    string
		allowed bool
	}{
		{"code owner", github.User{ID: 7}, "/merge exact-target", true},
		{"other human", github.User{ID: 42}, "/merge exact-target", false},
		{"publisher", github.User{ID: 99}, "/merge exact-target", false},
		{"bot identity", github.User{ID: 7, IsBot: true}, "/merge exact-target", false},
		{"self marker", github.User{ID: 7}, "<!-- lf:delivery -->\n/merge exact-target", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := d.authorizeCodeControl(t.Context(), session.ID, github.Comment{ID: 1, Author: tc.author, Body: tc.body})
			if (err == nil) != tc.allowed {
				t.Fatalf("code authority allowed=%v, want %v: %v", err == nil, tc.allowed, err)
			}
		})
	}
	// Keep the qualified public GitHub origin while using controlled local HTTP
	// responses. No production network call is made by this fixture.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var value any
		switch r.URL.Path {
		case "/user":
			value = map[string]any{"id": 99, "login": "publisher", "type": "Bot"}
		case "/repos/owner/repo":
			value = map[string]any{"id": 101, "full_name": "owner/repo", "has_issues": true}
		case "/repos/owner/repo/issues/1":
			value = map[string]any{"id": 701, "number": 1, "url": "https://api.github.com/repos/owner/repo/issues/1", "state": "open", "user": map[string]any{"id": 55, "login": "creator"}}
		default:
			t.Errorf("unexpected code-control request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, HTTPClient: &http.Client{Transport: approverLoopbackTransport{target: target}}, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	clients, err := factory.Clients(t.Context(), connection, 99)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge, err := turn.NewGitHubTicketWorkflow(db, clients.Tickets, 99)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: github.CommentSizeLimit, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
	if err != nil {
		t.Fatal(err)
	}
	executor := &approverCodeControlExecutor{t: t}
	d.connections, d.tickets, d.reporter, d.turn, d.log = factory, clients.Tickets, reporter, executor, logger
	taskRef, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	task := &task{repo: repo, forgeRepo: github.Repo{Owner: repo.Owner, Name: repo.Name}, session: session, issue: github.Issue{ID: 701, Number: 1}, ref: taskRef, log: logger, tickAt: time.Now().UTC()}
	for _, input := range []struct {
		id, actor int64
		body      string
	}{
		{100, 99, "/merge no-authority"},
		{101, 42, "/deliver no-authority"},
		{102, 7, "/merge exact-target"},
		{103, 42, "/merge no-authority"},
		{104, 7, "/deliver exact-target"},
	} {
		comment := github.Comment{ID: input.id, Author: github.User{ID: input.actor, Login: "display-only"}, Body: input.body, CreatedAt: time.Now().UTC(), APIURL: fmt.Sprintf("https://api.github.com/repos/owner/repo/issues/comments/%d", input.id), IssueAPIURL: "https://api.github.com/repos/owner/repo/issues/1"}
		if err := d.handleComment(t.Context(), task, comment); err != nil {
			t.Fatalf("code-control routing %d: %v", input.id, err)
		}
		watermark, present, err := db.GetWatermark(t.Context(), session.ID)
		if err != nil || !present || watermark.LastCommentID != input.id {
			t.Fatalf("code-control %d stalled polling: %+v %v %v", input.id, watermark, present, err)
		}
	}

	// Retained session authority survives replacement or disabling of defaults.
	if _, err := db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: connection.Origin, RemoteID: "42"}, Handle: "approver", VerifiedAt: time.Now().UTC(), Provenance: "explicit"}, connection.Revision, 0); err != nil {
		t.Fatal(err)
	}
	for _, current := range []store.ConnectionVersion{code, connection} {
		replacement := current
		replacement.Enabled = false
		replacement.Origin = "https://replacement.example"
		replacement.Destination = "replacement/project"
		if _, err := db.SetConnection(t.Context(), replacement, current.Revision); err != nil {
			t.Fatal(err)
		}
	}
	d.pool = newPool(1)
	d.instanceID = "saved-code-control"
	task.workerLog = logger
	if !d.inflight.enter(session.ID) || !d.pool.try() {
		t.Fatal("worker admission failed")
	}
	ran := false
	d.runTicketPollingWorker(t.Context(), task, func(ctx context.Context) {
		ran = true
		blockers, err := db.ApproverBlockers(ctx, repo.ID)
		if err != nil || len(blockers) == 0 {
			t.Fatalf("poll worker has no durable barrier: %+v %v", blockers, err)
		}
		for _, input := range []struct {
			id, actor int64
			origin    string
		}{{105, 99, "https://api.github.com"}, {106, 7, "https://foreign.example"}, {107, 7, "https://api.github.com"}} {
			c := github.Comment{ID: input.id, Author: github.User{ID: input.actor}, Body: "/merge exact-target", CreatedAt: time.Now().UTC(), APIURL: fmt.Sprintf("%s/repos/owner/repo/issues/comments/%d", input.origin, input.id), IssueAPIURL: input.origin + "/repos/owner/repo/issues/1"}
			err := d.handleComment(ctx, task, c)
			if input.origin == "https://foreign.example" {
				if err == nil {
					t.Fatal("foreign origin accepted")
				}
				continue
			}
			if err != nil {
				t.Fatalf("retained source routing: %v", err)
			}
		}
	})
	if !ran || len(executor.controls) != 3 || executor.controls[2].CommentID != 107 {
		t.Fatalf("saved code owner blocked by current defaults: ran=%v controls=%+v", ran, executor.controls)
	}
	if executor.controls[0].CommentID != 102 || executor.controls[1].CommentID != 104 {
		t.Fatalf("missing-appointment delivery routing: %+v", executor.controls)
	}
}

// approverLoopbackTransport changes transport routing only. Provider identities
// and the trusted project origin remain public GitHub values under test.
type approverLoopbackTransport struct{ target *url.URL }

func (transport approverLoopbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	local := request.Clone(request.Context())
	localURL := *request.URL
	localURL.Scheme, localURL.Host = transport.target.Scheme, transport.target.Host
	local.URL = &localURL
	return http.DefaultTransport.RoundTrip(local)
}

type approverCodeControlExecutor struct {
	t        *testing.T
	controls []turn.DeliveryControl
}

func (executor *approverCodeControlExecutor) Execute(context.Context, turn.Request) (turn.Result, error) {
	executor.t.Error("delivery control unexpectedly dispatched an agent")
	return turn.Result{}, errors.New("delivery controls must not dispatch an agent")
}
func (executor *approverCodeControlExecutor) ApplyDelivery(_ context.Context, control turn.DeliveryControl) error {
	executor.controls = append(executor.controls, control)
	return nil
}
