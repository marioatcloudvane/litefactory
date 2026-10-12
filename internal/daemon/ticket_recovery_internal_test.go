package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestRecoveryObservationRejectsQualifiedCollisions(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	v, _ := subscriptionCacheFixture(t, f, "routing", "https://api.github.com")
	p, err := f.d.db.InitTicketRecoveryPosition(t.Context(), v, v.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "99"}, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: "7"}}}
	for _, which := range []string{"origin", "provider", "container"} {
		t.Run(which, func(t *testing.T) {
			other := snapshot
			switch which {
			case "origin":
				other.Ref.Identity.Origin = "https://another.test/api/v3"
				other.Ref.Locator.Origin = other.Ref.Identity.Origin
			case "provider":
				other.Ref.Identity.Provider = "another"
				other.Ref.Locator.Provider = "another"
			case "container":
				other.Ref.Locator.Container = "elsewhere/routing"
			}
			if _, err := f.d.recoveryObservations(t.Context(), v, p, ticket.ChangePage{Tickets: []ticket.Snapshot{other}}, f.now, nil); !errors.Is(err, store.ErrTicketIdentityConflict) {
				t.Fatalf("qualified collision accepted: %v", err)
			}
		})
	}
	observations, err := f.d.recoveryObservations(t.Context(), v, p, ticket.ChangePage{Tickets: []ticket.Snapshot{snapshot}}, f.now, nil)
	if err != nil || len(observations) != 1 || observations[0].TicketID != "99" || observations[0].ConnectionVersionID != v.ID {
		t.Fatalf("matching source %+v %v", observations, err)
	}
}

func TestRecoveryInterruptedReplyStreamKeepsHandoffAndScanBoundary(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	var failReplies atomic.Bool
	failReplies.Store(true)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "login": "bot"})
		case "/repos/acme/streams":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1, "full_name": "acme/streams", "permissions": map[string]bool{"pull": true}})
		case "/repos/acme/streams/issues":
			if r.URL.Query().Get("sort") == "created" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 70, "number": 7, "url": origin + "/repos/acme/streams/issues/7", "title": "qualified ticket", "state": "open"}})
		case "/repos/acme/streams/issues/comments":
			if failReplies.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"temporary provider outage"}`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	v, _ := subscriptionCacheFixture(t, f, "streams", origin)
	// The source fixture's locator is immutable; replace only the verified ID by
	// making a fresh version before creating its recovery position.
	v.RemoteID = "1"
	var err error
	v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := v.CreatedAt
	// Credentials are explicit references; the resolver remains the existing
	// production factory boundary and never reaches the real keychain.
	v.CredentialSource = "env"
	v.CredentialEnv = "RECOVERY_TEST_TOKEN"
	v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	q := &repoQueue{repo: repo, forgeRepo: github.Repo{Owner: "acme", Name: "streams"}, log: f.d.log}
	started := v.CreatedAt.Add(time.Minute)
	complete, firstErr := f.d.recoverSource(f.d.discoveryContext(t.Context()), q, started)
	if firstErr == nil || complete {
		t.Fatalf("outage completed recovery: %v %v", complete, firstErr)
	}
	position, err := f.d.db.GetTicketRecoveryPosition(t.Context(), v.ID)
	if err != nil {
		t.Fatalf("position missing after first stream error %v: %v", firstErr, err)
	}
	if !position.TicketsComplete || position.RepliesComplete || !position.CompletedThrough.Equal(enrollment) || !position.ScanThrough.Equal(started) {
		t.Fatalf("interrupted coverage %+v", position)
	}
	pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 1 || pending[0].TicketID != "70" {
		t.Fatalf("interrupted handoff %+v %v", pending, err)
	}
	failReplies.Store(false)
	resumed := started.Add(time.Hour)
	if complete, err := f.d.recoverSource(f.d.discoveryContext(t.Context()), q, resumed); err != nil || !complete {
		t.Fatalf("recovery retry %v %v", complete, err)
	}
	position, err = f.d.db.GetTicketRecoveryPosition(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !position.CompletedThrough.Equal(started) {
		t.Fatalf("completion skipped interval while reply stream interrupted: %+v", position)
	}
	// An invalid provider continuation restarts from completed coverage. Its
	// already handed off observation stays pending for ordinary consumption.
	position.ScanThrough = resumed
	position.TicketsComplete = false
	position.RepliesComplete = false
	position.TicketCursor = "invalid-json"
	position, err = f.d.db.CommitTicketRecoveryPage(t.Context(), position, position.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.restartRecovery(t.Context(), position); err != nil {
		t.Fatal(err)
	}
	position, err = f.d.db.GetTicketRecoveryPosition(t.Context(), v.ID)
	if err != nil || position.TicketCursor != "" || !position.CompletedThrough.Equal(started) {
		t.Fatalf("restart reset coverage %+v %v", position, err)
	}
	pending, err = f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("restart lost pending work %+v %v", pending, err)
	}
}

func TestRecoveryQueueRetainsSignalsUntilMatchingWorkerDrain(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	v, _ := subscriptionCacheFixture(t, f, "drain", "https://api.github.com")
	repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.d.db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	observation := store.PendingTicketObservation{ConnectionVersionID: v.ID, RepoID: repo.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "70", TicketKey: "7", ObservedAt: f.now}
	if err := f.d.db.HandoffTicketObservation(t.Context(), observation); err != nil {
		t.Fatal(err)
	}
	pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("initial signal %+v %v", pending, err)
	}
	q := &repoQueue{repo: repo, log: f.d.log}
	c := &candidate{session: session, repo: repo, log: f.d.log}
	seen := map[int64]*candidate{}
	f.d.queueObservation(q, seen, c, pending[0])
	worker := &task{session: session, observations: append([]store.PendingTicketObservation(nil), c.observations...), log: f.d.log}
	pending, err = f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("queued/inflight signal consumed early %+v %v", pending, err)
	}
	if err := f.d.db.HandoffTicketObservation(t.Context(), observation); err != nil {
		t.Fatal(err)
	}
	f.d.clearTicketObservations(t.Context(), worker)
	pending, err = f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 1 || pending[0].Generation != 2 {
		t.Fatalf("old drain removed concurrent observation %+v %v", pending, err)
	}
	worker.observations = pending
	f.d.clearTicketObservations(t.Context(), worker)
	pending, err = f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("clean matching drain retained signal %+v %v", pending, err)
	}
}

func TestRecoveryObservationCannotAdoptAnotherProjectsSession(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	original, _ := subscriptionCacheFixture(t, f, "original-project", "https://api.github.com")
	other, _ := subscriptionCacheFixture(t, f, "other-project", "https://api.github.com")
	other.Destination, other.RemoteID = original.Destination, original.RemoteID
	var err error
	other, err = f.d.db.SetConnection(t.Context(), other, other.Revision)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := f.d.db.GetRepo(t.Context(), original.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.d.db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: original.Provider, Origin: original.Origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: original.Provider, Origin: original.Origin, Container: original.Destination, Key: "7"}}
	if _, err := f.d.db.BindSessionTicket(t.Context(), session.ID, ref); err != nil {
		t.Fatal(err)
	}
	if err := f.d.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{ConnectionVersionID: other.ID, RepoID: other.RepoID, Provider: other.Provider, Origin: other.Origin, Container: other.Destination, ContainerID: other.RemoteID, TicketID: "700", TicketKey: "7", ObservedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	otherRepo, err := f.d.db.GetRepo(t.Context(), other.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{Logger: f.d.log})
	q := &repoQueue{repo: otherRepo, forgeRepo: githubRepo(other.Destination), log: f.d.log}
	seen := map[int64]*candidate{}
	if err := f.d.drainTicketObservations(t.Context(), map[int64]*repoQueue{otherRepo.ID: q}, seen); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 || len(q.candidates) != 0 {
		t.Fatal("another project adopted existing saved work")
	}
}

func TestTicketAdmissionFailureBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		deferred bool
	}{
		{"repository missing", &github.NotFoundError{APIError: github.APIError{Err: github.ErrNotFound}}, true},
		{"access forbidden", github.ErrForbidden, true},
		{"credential rejected", github.ErrUnauthorized, true},
		{"Linear refused", &ticket.Failure{Kind: ticket.Refused}, true},
		{"store missing", store.ErrNotFound, false},
		{"infrastructure", errors.New("database unavailable"), false},
		{"provider outage", github.ErrUnexpectedStatus, false},
		{"cancellation", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"GitHub quota", &github.RateLimitError{APIError: github.APIError{Err: github.ErrRateLimited}, RetryAfter: time.Minute}, false},
		{"Linear quota", &ticket.Failure{Kind: ticket.Refused, Cause: &ticket.RateLimitError{RetryAfter: time.Minute}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ticketAdmissionDeferred(fmt.Errorf("find the task: %w", tc.err)); got != tc.deferred {
				t.Fatalf("deferred=%t want %t", got, tc.deferred)
			}
		})
	}
}

func TestRecoveryAdmissionContinuesWithinSource(t *testing.T) {
	for _, mode := range []string{"not-found", "cancel", "rate-limit", "invalid-response"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryHTTPFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var inspections atomic.Int64
			var origin string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user":
					_, _ = w.Write([]byte(`{"id":99,"login":"bot"}`))
				case "/repos/acme/code":
					if inspections.Add(1) == 1 {
						switch mode {
						case "not-found":
							http.NotFound(w, r)
						case "cancel":
							cancel()
							http.NotFound(w, r)
						case "rate-limit":
							w.Header().Set("Retry-After", "60")
							w.WriteHeader(http.StatusTooManyRequests)
						case "invalid-response":
							_, _ = w.Write([]byte("invalid JSON"))
						}
						return
					}
					_, _ = w.Write([]byte(`{"id":2,"full_name":"acme/code"}`))
				case "/repos/acme/admission":
					_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/admission"}`))
				case "/repos/acme/admission/issues/7", "/repos/acme/admission/issues/8":
					n := 7
					if strings.HasSuffix(r.URL.Path, "/8") {
						n = 8
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": n * 10, "number": n, "title": "Pending", "state": "open", "url": origin + r.URL.Path})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			origin = server.URL
			v, _ := subscriptionCacheFixture(t, f, "admission", origin)
			v.RemoteID, v.CredentialSource, v.CredentialEnv = "1", "env", "TEST_ADMISSION_TOKEN"
			var err error
			v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
			if err != nil {
				t.Fatal(err)
			}
			code := v
			code.ID, code.Revision, code.Role, code.Destination, code.RemoteID = 0, 0, store.CodeConnection, "acme/code", "2"
			if _, err = f.d.db.SetConnection(t.Context(), code, 0); err != nil {
				t.Fatal(err)
			}
			if _, err = f.d.db.SetApprover(t.Context(), store.ApproverAppointment{RepoID: v.RepoID, Actor: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "42"}, Handle: "owner", VerifiedAt: time.Now(), Provenance: "explicit"}, v.Revision, 0); err != nil {
				t.Fatal(err)
			}
			f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
			repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range []int{7, 8} {
				if err := f.d.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: "1", TicketID: strconv.Itoa(n * 10), TicketKey: strconv.Itoa(n), ObservedAt: f.now}); err != nil {
					t.Fatal(err)
				}
			}
			q := &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}
			seen := map[int64]*candidate{}
			err = f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{repo.ID: q}, seen)
			switch mode {
			case "not-found":
				if err != nil || len(seen) != 1 || len(q.candidates) != 1 || q.candidates[0].issue.Number != 8 {
					t.Fatalf("later ticket starved: %v %+v", err, q.candidates)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation swallowed: %v", err)
				}
			case "rate-limit":
				var rate *github.RateLimitError
				if !errors.As(err, &rate) || rate.RetryAfter != time.Minute {
					t.Fatalf("rate limit lost: %v", err)
				}
			case "invalid-response":
				if err == nil {
					t.Fatal("infrastructure failure swallowed")
				}
			}
		})
	}
}

func TestRecoveryLinearTicketWithInaccessibleGitHubCode(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	remote := newLinearWorkflowServer(t)
	if _, err := f.d.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	var refused atomic.Bool
	refused.Store(true)
	codeRemote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":99,"login":"bot"}`))
		case "/repos/acme/linear-code":
			if refused.Load() {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/linear-code"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer codeRemote.Close()
	v, _ := subscriptionCacheFixture(t, f, "linear-code", codeRemote.URL)
	codeURL, err := url.Parse(codeRemote.URL)
	if err != nil {
		t.Fatal(err)
	}
	code := v
	code.Origin = "https://api.github.com"
	code.ID, code.Revision, code.Role, code.RemoteID = 0, 0, store.CodeConnection, "1"
	code.CredentialSource, code.CredentialEnv = "env", "CODE_TOKEN"
	if _, err := f.d.db.SetConnection(t.Context(), code, 0); err != nil {
		t.Fatal(err)
	}
	v.Provider, v.Origin, v.Destination, v.RemoteID = ticket.Linear, ticket.Origin(remote.server.URL), "team-uuid/project-uuid", "project-uuid"
	v.CredentialSource, v.CredentialEnv = "env", "LINEAR_TOKEN"
	v.Linear = store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}
	v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{DB: f.d.db, HTTPClient: &http.Client{Transport: linearMixedProviderTransport{githubURL: codeURL}}, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "LINEAR_TOKEN" {
			return "test-linear-token", true
		}
		return "offline-token", true
	}}})
	if _, err := (projectconnection.ApproverService{DB: f.d.db, Factory: f.d.connections}).Set(t.Context(), v.RepoID, "human-uuid", v.Revision, 0); err != nil {
		t.Fatal(err)
	}
	repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.d.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	identity := ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"}
	for range 2 {
		q := &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}
		seen := map[int64]*candidate{}
		if err := f.d.drainTicketObservations(f.d.discoveryContext(t.Context()), map[int64]*repoQueue{repo.ID: q}, seen); err != nil {
			t.Fatal(err)
		}
		if len(seen) != 0 {
			t.Fatal("failed Linear ticket admitted")
		}
		if _, err := f.d.db.GetSessionByTicket(t.Context(), identity); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("failed Linear session exists: %v", err)
		}
		pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
		if err != nil || len(pending) != 1 || pending[0].Generation != 1 {
			t.Fatalf("retry changed: %+v %v", pending, err)
		}
	}
	if !strings.Contains(f.logs.String(), "inspect repository") || !strings.Contains(f.logs.String(), "parent-uuid") {
		t.Fatalf("did not exercise repository refusal: %s", f.logs.String())
	}
	refused.Store(false)
	q := &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}
	seen := map[int64]*candidate{}
	if err := f.d.drainTicketObservations(f.d.discoveryContext(t.Context()), map[int64]*repoQueue{repo.ID: q}, seen); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("unchanged Linear observation not retried: %s", f.logs.String())
	}
}
