package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

type doctorProjectFixture struct {
	d           *Daemon
	repo        store.Repo
	connection  store.ConnectionVersion
	appointment store.ApproverAppointment
	session     store.Session
	issue       github.Issue
	source      github.Comment
	publication *doctorProjectPublication
	httpClient  *http.Client
}

func newDoctorProjectFixture(t *testing.T, savedSession bool) doctorProjectFixture {
	t.Helper()
	d, _, repo := doctorDaemonFixture(t)
	origin := ticket.Origin("https://api.github.com")
	connection, err := d.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: origin, Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "DOCTOR_PROJECT", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
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
	source := github.Comment{ID: 100, Author: github.User{ID: 42, Login: "renamed-approver"}, Body: "/doctor explain the stalled work", CreatedAt: fresh, UpdatedAt: fresh, APIURL: "https://api.github.com/repos/team/tickets/issues/comments/100", IssueAPIURL: "https://api.github.com/repos/team/tickets/issues/406"}
	publication := &doctorProjectPublication{publisherID: 99}
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
			t.Errorf("unexpected doctor provider destination %s", r.URL.Path)
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
	d.doctor.service = &doctor.Service{DB: d.db, Paths: config.NewPaths(t.TempDir()), Redactor: redact.New(redact.Options{Environ: []string{}})}
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
	return doctorProjectFixture{d: d, repo: repo, connection: connection, appointment: appointment, session: session, issue: issue, source: source, publication: publication, httpClient: httpClient}
}

func TestDoctorProjectApproverAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		actor                      int64
		operation                  string
		stale, self, foreignOrigin bool
		allowed                    bool
	}{
		{name: "approver diagnosis", actor: 42, operation: "explain", allowed: true},
		{name: "approver stop", actor: 42, operation: "stop", allowed: true},
		{name: "code owner cannot direct diagnosis", actor: 7, operation: "explain"},
		{name: "other project actor", actor: 43, operation: "explain"},
		{name: "publisher", actor: 99, operation: "explain"},
		{name: "preactivation reply", actor: 42, operation: "explain", stale: true},
		{name: "self marker", actor: 42, operation: "explain", self: true},
		{name: "foreign source origin", actor: 42, operation: "explain", foreignOrigin: true},
		{name: "approver cannot approve code repair", actor: 42, operation: "apply"},
		{name: "code owner retains apply", actor: 7, operation: "apply", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDoctorProjectFixture(t, true)
			source := f.source
			source.Author.ID = tc.actor
			source.Body = "/doctor " + tc.operation
			if tc.stale {
				source.CreatedAt = f.appointment.ActivatedAt
				source.UpdatedAt = f.source.CreatedAt
			}
			if tc.self {
				source.Body += "\n<!-- lf:report -->"
			}
			if tc.foreignOrigin {
				source.APIURL = "https://other.example/repos/team/tickets/issues/comments/100"
			}
			routed, err := f.d.admitDoctor(sessionContext(t.Context(), f.session.ID), f.repo, f.issue, source)
			if !routed {
				t.Fatal("doctor command not recognized")
			}
			if tc.allowed && err != nil {
				t.Fatalf("valid doctor command: %v", err)
			}
			control := tc.operation == "stop" || tc.operation == "apply"
			command, claimErr := f.d.db.ClaimDoctorCommand(t.Context(), "", control, "test-doctor")
			if tc.allowed {
				if claimErr != nil || command.ActorID != tc.actor || command.TaskRef != f.session.TaskRef {
					t.Fatalf("doctor authorization not retained: %+v %v", command, claimErr)
				}
				blockers, err := f.d.db.ApproverBlockers(t.Context(), f.repo.ID)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, blocker := range blockers {
					if blocker.Kind == "doctor_command" {
						found = true
					}
				}
				if !found {
					t.Fatalf("doctor command omitted from replacement barriers: %+v", blockers)
				}
			} else if !errors.Is(claimErr, store.ErrNotFound) {
				t.Fatalf("unauthorized command reached inbox: %+v %v; admission error %v", command, claimErr, err)
			}
		})
	}
}

func TestDoctorPriorityUsesConfiguredProjectApprover(t *testing.T) {
	f := newDoctorProjectFixture(t, false)
	start := f.appointment.ActivatedAt
	if _, err := f.d.db.DoctorScanWindow(t.Context(), f.repo.ID, start); err != nil {
		t.Fatal(err)
	}
	at := start.Add(2 * time.Minute)
	f.d.discovery.tick(at)
	if err := f.d.discoverDoctor(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	command, err := f.d.db.ClaimDoctorCommand(t.Context(), "", false, "priority-test")
	if err != nil || command.ActorID != 42 || command.TaskRef != "github:team/tickets#406" {
		t.Fatalf("priority scan ignored project ticket approver: %+v %v", command, err)
	}
	sessions, err := f.d.db.ListRepoSessions(t.Context(), f.repo.ID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("diagnosis allocated workflow sessions: %+v %v", sessions, err)
	}
	if _, err := f.d.db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: f.repo.ID, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: f.connection.Origin, RemoteID: "43"}, Handle: "replacement", VerifiedAt: time.Now().UTC(), Provenance: "explicit"}, f.connection.Revision, f.appointment.Revision); !errors.Is(err, store.ErrApproverBusy) {
		t.Fatalf("replacement crossed doctor inbox: %v", err)
	}
	t.Logf("priority diagnosis admitted from qualified project approver %d into %s", command.ActorID, command.TaskRef)
}

type doctorProjectPublication struct {
	mu          sync.Mutex
	posts       int
	publisherID int64
	comments    []map[string]any
	loseReceipt bool
}

func TestDoctorProjectPublicationPinsOriginalSource(t *testing.T) {
	f := newDoctorProjectFixture(t, false)
	if _, err := f.d.admitDoctor(t.Context(), f.repo, f.issue, f.source); err != nil {
		t.Fatal(err)
	}
	command, err := f.d.db.ClaimDoctorCommand(t.Context(), "", false, "doctor-publisher")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.db.CompleteDoctorCommand(t.Context(), command.ID, "Diagnosis retained for the original ticket.", 0); err != nil {
		t.Fatal(err)
	}
	source, err := f.d.db.GetDoctorCommandSource(t.Context(), command.ID)
	if err != nil || source.ConnectionVersionID != f.connection.ID || source.PublisherID != 99 || source.Ticket.Locator.Container != "team/tickets" {
		t.Fatalf("doctor source not pinned: %+v %v", source, err)
	}
	// Connection edits must not redirect either comment reads or prepared output.
	changed := f.connection
	changed.Destination = "replacement/tickets"
	changed.RemoteID = "303"
	if _, err := f.d.db.SetConnection(t.Context(), changed, f.connection.Revision); err != nil {
		t.Fatal(err)
	}
	ctx, err := f.d.doctorCommandContext(t.Context(), command.ID)
	if err != nil {
		t.Fatal(err)
	}
	selected, publisher, err := selectTicketConnection(ctx, f.d.db, f.d.connections, github.Repo{Owner: "team", Name: "tickets"}, 406, 0)
	if err != nil || publisher != 99 || selected.Origin() != source.Ticket.Identity.Origin {
		t.Fatalf("doctor reads redirected: %v %d %v", selected, publisher, err)
	}
	f.publication.mu.Lock()
	f.publication.loseReceipt = true
	f.publication.mu.Unlock()
	if err := f.d.publishDoctor(t.Context()); err == nil {
		t.Fatal("lost doctor receipt was accepted")
	}
	pending, err := f.d.db.PendingDoctorDeliveries(t.Context(), "")
	if err != nil || len(pending) != 1 || !pending[0].PossiblySent || pending[0].BotUserID != 99 || pending[0].Destination != "github:team/tickets#406" {
		t.Fatalf("uncertain doctor source changed: %+v %v", pending, err)
	}
	original := pending[0]
	// Restart from disk and reconstruct the original credential/client from the
	// source companion before reconciling the already-posted bytes.
	dbPath := f.d.db.Path()
	if err := f.d.db.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.Open(t.Context(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restarted.Close(); err != nil {
			t.Error(err)
		}
	})
	f.d.db, f.d.doctor.service.DB = restarted, restarted
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, HTTPClient: f.httpClient, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	if err := f.d.publishDoctor(t.Context()); err != nil {
		t.Fatal(err)
	}
	retained, err := f.d.db.GetReportDelivery(t.Context(), original.Key)
	if err != nil || !retained.Completed || retained.Body != original.Body || retained.Marker != original.Marker || retained.BotUserID != original.BotUserID {
		t.Fatalf("doctor reconciliation changed prepared source: %+v %v", retained, err)
	}
	f.publication.mu.Lock()
	defer f.publication.mu.Unlock()
	if f.publication.posts != 1 {
		t.Fatalf("doctor recovery repeated publication %d times", f.publication.posts)
	}
}

func TestDoctorSavedSourceRejectsChangedPublisher(t *testing.T) {
	f := newDoctorProjectFixture(t, true)
	f.publication.mu.Lock()
	f.publication.publisherID = 98
	f.publication.mu.Unlock()
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{HTTPClient: f.httpClient, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	_, err := f.d.admitDoctor(sessionContext(t.Context(), f.session.ID), f.repo, f.issue, f.source)
	if !errors.Is(err, projectconnection.ErrActorChanged) {
		t.Fatalf("saved publisher change admitted: %v", err)
	}
	if command, err := f.d.db.ClaimDoctorCommand(t.Context(), "", false, "changed-publisher"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("publisher mismatch entered inbox: %+v %v", command, err)
	}
}
