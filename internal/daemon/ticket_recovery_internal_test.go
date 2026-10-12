package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
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
	assertObservationWarning(t, f.logs.String(), other, "700", "other_owning_project", "unavailable", "unavailable", "retained")
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

func TestRecoveryDoctorHoldDefersOnlyAffectedObservation(t *testing.T) {
	for _, parent := range []bool{false, true} {
		for _, race := range []bool{false, true} {
			t.Run(fmt.Sprintf("parent=%t/race=%t", parent, race), func(t *testing.T) {
				f, v, q, sessions, onRead := recoveryHoldFixture(t)
				ctx := t.Context()
				observed := 7
				if parent {
					observed = 6
					if _, err := f.d.db.AdmitPlanWorkflowPath(ctx, sessions[0].ID, "graph"); err != nil {
						t.Fatal(err)
					}
					if _, err := f.d.db.ReservePlanStory(ctx, sessions[0].TaskRef, "child"); err != nil {
						t.Fatal(err)
					}
					if err := f.d.db.BindPlanStoryIssue(ctx, sessions[0].TaskRef, "child", 60, observed); err != nil {
						t.Fatal(err)
					}
					if err := f.d.db.AdvanceStoryDiscovery(ctx, sessions[0].ID, observed, 0); err != nil {
						t.Fatal(err)
					}
				}
				hold := func() {
					approveEffect(t, f.d.db, sessions[0], store.DoctorRepairStep{Kind: "poll_flag", Target: fmt.Sprintf("session:%d", sessions[0].ID), Before: "0", After: "1"}, store.DoctorEvidence{TaskRef: sessions[0].TaskRef})
				}
				if race {
					*onRead = func(r *http.Request) {
						if strings.HasSuffix(r.URL.Path, fmt.Sprintf("/issues/%d", observed)) {
							*onRead = nil
							hold()
						}
					}
				} else {
					hold()
				}
				for _, n := range []int{observed, 8} {
					handoffRecoveryHold(t, f.d.db, v, n)
				}
				for range 2 {
					seen := map[int64]*candidate{}
					q.candidates = nil
					if err := f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{q.repo.ID: q}, seen); err != nil {
						t.Fatal(err)
					}
					if len(seen) != 1 || seen[sessions[1].ID] == nil {
						t.Fatalf("unrelated same-source observation blocked: %+v", seen)
					}
					pending, err := f.d.db.ListPendingTicketObservations(ctx, v.ID)
					if err != nil || len(pending) != 2 || pending[0].Generation != 1 || pending[1].Generation != 1 {
						t.Fatalf("lost durable generation: %+v %v", pending, err)
					}
					held, err := f.d.db.GetSession(ctx, sessions[0].ID)
					if err != nil || held.NeedsPoll {
						t.Fatalf("held backlog changed: %+v %v", held, err)
					}
					if parent {
						owed, err := f.d.db.StoryDiscoveryOwed(ctx, sessions[0].ID, observed)
						if err != nil || owed {
							t.Fatalf("held parent discovery changed: %t %v", owed, err)
						}
					}
				}
				if err := f.d.db.StopDoctorRepair(ctx, sessions[0].TaskRef, "release fixture hold"); err != nil {
					t.Fatal(err)
				}
				seen := map[int64]*candidate{}
				if err := f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{q.repo.ID: q}, seen); err != nil {
					t.Fatal(err)
				}
				if len(seen) != 2 {
					t.Fatalf("release did not retry: %+v", seen)
				}
			})
		}
	}
}

func TestRecoveryDoctorHeldRetainedSession(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("replacement-enabled=%t", enabled), func(t *testing.T) {
			f, v, q, sessions, _ := recoveryHoldFixture(t)
			v.Enabled = enabled
			if _, err := f.d.db.SetConnection(t.Context(), v, v.Revision); err != nil {
				t.Fatal(err)
			}
			approveEffect(t, f.d.db, sessions[0], store.DoctorRepairStep{Kind: "poll_flag", Target: fmt.Sprintf("session:%d", sessions[0].ID), Before: "0", After: "1"}, store.DoctorEvidence{TaskRef: sessions[0].TaskRef})
			for range 2 {
				if err := f.d.recoverRetainedSessions(t.Context(), q); err != nil {
					t.Fatal(err)
				}
				for i, s := range sessions {
					got, err := f.d.db.GetSession(t.Context(), s.ID)
					if err != nil || got.NeedsPoll != (i == 1) {
						t.Fatalf("retained backlog: %+v %v", got, err)
					}
				}
			}
			if err := f.d.db.StopDoctorRepair(t.Context(), sessions[0].TaskRef, "release fixture"); err != nil {
				t.Fatal(err)
			}
			if err := f.d.recoverRetainedSessions(t.Context(), q); err != nil {
				t.Fatal(err)
			}
			got, err := f.d.db.GetSession(t.Context(), sessions[0].ID)
			if err != nil || !got.NeedsPoll {
				t.Fatalf("retained scan failed to retry: %+v %v", got, err)
			}
		})
	}
}

// The transport callback runs synchronously after the hold lookup and before
// the provider read returns, placing a real hold in the lookup/write window.
func recoveryHoldFixture(t *testing.T) (*discoveryHTTPFixture, store.ConnectionVersion, *repoQueue, []store.Session, *func(*http.Request)) {
	t.Helper()
	f := newDiscoveryHTTPFixture(t)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4900")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(4*time.Hour).Unix(), 10))
		switch {
		case r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"id":99,"login":"bot"}`))
		case r.URL.Path == "/repos/acme/held":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/held"}`))
		case r.URL.Path == "/repos/acme/held/issues" || r.URL.Path == "/repos/acme/held/issues/comments" || strings.HasSuffix(r.URL.Path, "/labels"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/comments"):
			n, _ := strconv.Atoi(strings.Split(r.URL.Path, "/")[5])
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": n * 100, "body": "/spike process observed work", "url": origin + fmt.Sprintf("/repos/acme/held/issues/comments/%d", n*100), "issue_url": origin + fmt.Sprintf("/repos/acme/held/issues/%d", n), "user": map[string]any{"id": 42, "login": "owner"}, "created_at": time.Now().Add(time.Hour).Format(time.RFC3339), "updated_at": time.Now().Add(time.Hour).Format(time.RFC3339)}})
		case strings.HasPrefix(r.URL.Path, "/repos/acme/held/issues/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/held/issues/"))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": n * 10, "number": n, "title": "Observed", "state": "open", "labels": []map[string]any{{"name": "Mode: Spike"}}, "url": origin + r.URL.Path})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	origin = server.URL
	v, _ := subscriptionCacheFixture(t, f, "held", origin)
	v.RemoteID, v.CredentialSource, v.CredentialEnv = "1", "env", "TEST_RECOVERY_TOKEN"
	var err error
	v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	code := v
	code.ID, code.Revision, code.Role = 0, 0, store.CodeConnection
	code, err = f.d.db.SetConnection(t.Context(), code, 0)
	if err != nil {
		t.Fatal(err)
	}
	var onRead func(*http.Request)
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}, HTTPClient: &http.Client{Transport: discoveryUnitTransport(func(r *http.Request) (*http.Response, error) {
		if onRead != nil {
			onRead(r)
		}
		return http.DefaultTransport.RoundTrip(r)
	})}})
	repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	var sessions []store.Session
	for _, n := range []int{7, 8} {
		session, err := f.d.db.GetOrCreateSession(t.Context(), repo, n)
		if err != nil {
			t.Fatal(err)
		}
		ref := ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: strconv.Itoa(n * 10)}, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: strconv.Itoa(n)}}
		binding, err := f.d.db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.d.db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: session.ID, RepoID: repo.ID, BindingID: binding.ID, TicketVersionID: v.ID, CodeVersionID: code.ID, ActorID: 99, ActorLogin: "bot"}); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
	}
	return f, v, &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}, sessions, &onRead
}

func handoffRecoveryHold(t *testing.T, db *store.DB, v store.ConnectionVersion, n int) {
	t.Helper()
	if err := db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: strconv.Itoa(n * 10), TicketKey: strconv.Itoa(n), ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryDoctorDeferralPreservesOtherFailures(t *testing.T) {
	for _, mode := range []string{"store", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, v, q, _, onRead := recoveryHoldFixture(t)
			for _, n := range []int{7, 8} {
				handoffRecoveryHold(t, f.d.db, v, n)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			*onRead = func(r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/issues/7") {
					return
				}
				*onRead = nil
				if mode == "cancel" {
					cancel()
				} else if err := f.d.db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			err := f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{q.repo.ID: q}, map[int64]*candidate{})
			if err == nil || store.IsDoctorRepairHold(err) {
				t.Fatalf("non-hold failure hidden: %v", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

// Dispatch is the boundary under test; leave execution locked so this fixture
// does not pretend to run an agent or publish a report.
type recoveryHoldExecutor struct{ requests chan turn.Request }

func (e recoveryHoldExecutor) Execute(_ context.Context, req turn.Request) (turn.Result, error) {
	e.requests <- req
	return turn.Result{Disposition: turn.DispositionLocked}, nil
}

func TestRecoveryDoctorHoldTickContinuesToLinear(t *testing.T) {
	f, v, q, sessions, onRead := recoveryHoldFixture(t)
	ctx := t.Context()
	remote := newLinearWorkflowServer(t)
	handler := remote.server.Config.Handler
	remote.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read request", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if bytes.Contains(body, []byte("reactionCreate")) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"reactionCreate":{"success":true}}}`))
			return
		}
		handler.ServeHTTP(w, r)
	})
	codeURL, err := url.Parse(string(v.Origin))
	if err != nil {
		t.Fatal(err)
	}
	factory := projectconnection.NewFactory(projectconnection.Options{DB: f.d.db, Logger: f.d.log, HTTPClient: &http.Client{Transport: linearMixedProviderTransport{githubURL: codeURL}}, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "LINEAR_TOKEN" {
			return "test-linear-token", true
		}
		return "offline-token", true
	}}})
	if _, err := f.d.db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.db.SetApprover(ctx, store.ApproverAppointment{RepoID: v.RepoID, Actor: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "42"}, Handle: "owner", VerifiedAt: time.Now(), Provenance: "explicit"}, v.Revision, 0); err != nil {
		t.Fatal(err)
	}
	other, err := f.d.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "linear", RemoteURL: "git@github.com:acme/linear.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code, err := f.d.db.CurrentConnection(ctx, v.RepoID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	code.ID, code.Revision, code.RepoID, code.Origin = 0, 0, other.ID, "https://api.github.com"
	code.ClonePath, code.RemoteURL, code.WorktreeRoot = other.Path, other.RemoteURL, other.WorktreeRoot
	if _, err := f.d.db.SetConnection(ctx, code, 0); err != nil {
		t.Fatal(err)
	}
	linearSource, err := f.d.db.SetConnection(ctx, store.ConnectionVersion{RepoID: other.ID, Role: store.TicketConnection, Provider: ticket.Linear, Origin: ticket.Origin(remote.server.URL), Destination: "team-uuid/project-uuid", RemoteID: "project-uuid", Enabled: true, Provenance: "explicit", CredentialSource: "env", CredentialEnv: "LINEAR_TOKEN", Linear: store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"spike":"label-spike"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (projectconnection.ApproverService{DB: f.d.db, Factory: factory}).Set(ctx, other.ID, "human-uuid", linearSource.Revision, 0); err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.Linear, Origin: linearSource.Origin, RemoteID: "parent-uuid"}, Locator: ticket.Locator{Provider: ticket.Linear, Origin: linearSource.Origin, Container: linearSource.Destination, Key: "parent-uuid"}}
	if _, err := f.d.db.AddTicketBinding(ctx, store.TicketBinding{RepoID: other.ID, ConnectionVersionID: linearSource.ID, Ref: ref, Enabled: true, Provenance: "explicit"}); err != nil {
		t.Fatal(err)
	}
	linearSession, err := AdmitProjectTicket(ctx, f.d.db, factory, other, ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append(sessions, linearSession) {
		if _, _, err := f.d.db.BootstrapWatermark(ctx, s.ID, 0); err != nil {
			t.Fatal(err)
		}
	}
	remote.addReply("fresh-reply", "/spike continue this Linear ticket", "human-uuid")
	// Retain the HTTP callback factory for GitHub so the same tick can activate
	// the hold after the lookup. The mixed factory delegates to it for loopback.
	factory = projectconnection.NewFactory(projectconnection.Options{DB: f.d.db, Logger: f.d.log, HTTPClient: &http.Client{Transport: discoveryUnitTransport(func(r *http.Request) (*http.Response, error) {
		if *onRead != nil {
			(*onRead)(r)
		}
		return (linearMixedProviderTransport{githubURL: codeURL}).RoundTrip(r)
	})}, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "LINEAR_TOKEN" {
			return "test-linear-token", true
		}
		return "offline-token", true
	}}})
	*onRead = func(r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issues/7") {
			*onRead = nil
			approveEffect(t, f.d.db, sessions[0], store.DoctorRepairStep{Kind: "poll_flag", Target: fmt.Sprintf("session:%d", sessions[0].ID), Before: "0", After: "1"}, store.DoctorEvidence{TaskRef: sessions[0].TaskRef})
		}
	}
	for _, n := range []int{7, 8} {
		handoffRecoveryHold(t, f.d.db, v, n)
	}
	if err := f.d.db.HandoffTicketObservation(ctx, store.PendingTicketObservation{RepoID: other.ID, ConnectionVersionID: linearSource.ID, Provider: linearSource.Provider, Origin: linearSource.Origin, Container: linearSource.Destination, ContainerID: linearSource.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	requests := make(chan turn.Request, 4)
	now := time.Now().Add(2 * time.Hour)
	d, err := New(Options{DB: f.d.db, Forge: f.client, Filter: f.d.filter, Connections: factory, Turn: recoveryHoldExecutor{requests}, Reporter: stubReporter{}, InstanceID: "hold-mixed", Logger: f.d.log, Now: func() time.Time { return now }, MaxConcurrentRuns: 4, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.db.InitPollCursor(ctx, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	d.schedule.ListingFinished(q.repo.ID, now)
	d.schedule.ListingFinished(other.ID, now)
	if _, err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.Quiesce(ctx); err != nil {
		t.Fatal(err)
	}
	close(requests)
	seen := map[int64]bool{}
	for req := range requests {
		seen[req.SessionID] = true
	}
	if len(seen) != 2 || !seen[sessions[1].ID] || !seen[linearSession.ID] {
		t.Fatalf("mixed-provider dispatch blocked: %v\n%s", seen, f.logs.String())
	}
	pending, err := f.d.db.ListPendingTicketObservations(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	var retained bool
	for _, p := range pending {
		if p.TicketID == "70" {
			retained = p.Generation == 1
		}
	}
	if !retained {
		t.Fatalf("hold race lost intent: %+v", pending)
	}
}

// Uses real adapters and an isolated database; only provider HTTP is faked.
func linearObservationFixture(t *testing.T) (*discoveryHTTPFixture, *linearWorkflowServer, store.ConnectionVersion, store.TicketSubscription, *repoQueue) {
	t.Helper()
	f := newDiscoveryHTTPFixture(t)
	remote := newLinearWorkflowServer(t)
	if _, err := f.d.db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	codeRemote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":99,"login":"bot"}`))
		case "/repos/acme/linear-code":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/linear-code"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(codeRemote.Close)
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
	v.Linear = store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan","spike":"label-spike"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}
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

	sub, err := f.d.db.PutTicketSubscription(t.Context(), store.TicketSubscription{ID: "linear-current", RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, CallbackURL: "https://callback.invalid/webhook", DesiredEnabled: true, SecretService: "test", SecretAccount: "test", SecretVersion: "1"}, 0)
	linearWorkflowCheck(t, err)
	_, err = f.d.db.InitTicketRecoveryPosition(t.Context(), v, f.now)
	linearWorkflowCheck(t, err)
	remote.mu.Lock()
	remote.issues["parent-uuid"]["description"] = "Observe this issue"
	remote.mu.Unlock()
	return f, remote, v, sub, &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}
}

func handoffLinearObservation(t *testing.T, f *discoveryHTTPFixture, v store.ConnectionVersion, sub store.TicketSubscription) {
	t.Helper()
	linearWorkflowCheck(t, f.d.db.HandoffSubscriptionObservation(t.Context(), sub.ID, sub.Revision, store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: f.now}))
}

func TestRecoveryLinearObservationStates(t *testing.T) {
	for _, kind := range []string{"backlog", "triage", "started", "unstarted", "completed", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			f, remote, v, sub, q := linearObservationFixture(t)
			stateID := "state-" + kind
			if kind == "unstarted" {
				stateID = "state-open"
			}
			remote.mu.Lock()
			remote.issues["parent-uuid"]["state"] = map[string]any{"id": stateID, "type": kind, "name": kind}
			remote.mu.Unlock()
			identity := ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"}
			if _, err := f.d.db.GetTicketBinding(t.Context(), identity); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("preexisting binding: %v", err)
			}
			if _, err := f.d.db.GetSessionByTicket(t.Context(), identity); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("preexisting session: %v", err)
			}
			handoffLinearObservation(t, f, v, sub)
			seen := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(t.Context()), map[int64]*repoQueue{v.RepoID: q}, seen))
			if kind == "completed" || kind == "canceled" {
				if len(seen) != 0 {
					t.Fatal("terminal issue admitted")
				}
				if _, err := f.d.db.GetSessionByTicket(t.Context(), identity); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("terminal session: %v", err)
				}
				pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
				linearWorkflowCheck(t, err)
				if len(pending) != 0 {
					t.Fatalf("terminal observation retained: %+v", pending)
				}
				reason := "completed"
				if kind == "canceled" {
					reason = "not_planned"
				}
				assertObservationWarning(t, f.logs.String(), v, "parent-uuid", "closed_unadmitted", "closed", reason, "consumed")
				return
			}
			session, err := f.d.db.GetSessionByTicket(t.Context(), identity)
			linearWorkflowCheck(t, err)
			binding, err := f.d.db.GetTicketBinding(t.Context(), identity)
			linearWorkflowCheck(t, err)
			if len(seen) != 1 || seen[session.ID] == nil || binding.ConnectionVersionID != v.ID || seen[session.ID].issue.State != "open" {
				t.Fatalf("open admission: %+v %+v", session, seen)
			}
			task, err := f.d.materialise(t.Context(), seen[session.ID], f.now)
			linearWorkflowCheck(t, err)
			linearWorkflowCheck(t, f.d.bootstrap(sessionContext(t.Context(), session.ID), task))
			if !strings.Contains(f.logs.String(), "first sight of a task") {
				t.Fatal("missing first sight")
			}
			handoffLinearObservation(t, f, v, sub)
			linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(t.Context()), map[int64]*repoQueue{v.RepoID: q}, seen))
			replay, err := f.d.db.GetSessionByTicket(t.Context(), identity)
			linearWorkflowCheck(t, err)
			if len(seen) != 1 || replay.ID != session.ID {
				t.Fatal("replay duplicated admission")
			}
			remote.addReply("eligible-reply", "/spike process this observation", "human-uuid")
			requests := make(chan turn.Request, 1)
			runner, err := New(Options{DB: f.d.db, Forge: f.client, Filter: f.d.filter, Connections: f.d.connections, Turn: recoveryHoldExecutor{requests}, Reporter: stubReporter{}, InstanceID: "linear-observation", Logger: f.d.log, Now: func() time.Time { return f.now }, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions})
			linearWorkflowCheck(t, err)
			runner.discovery.tick(f.now)
			runner.discovery.observe(github.RequestObservation{CoreRateValid: true, Limit: 5000, Remaining: 5000, Reset: f.now.Add(time.Hour), StatusCode: http.StatusOK})
			_, err = runner.drain(sessionContext(t.Context(), session.ID), task, 0)
			linearWorkflowCheck(t, err)
			select {
			case req := <-requests:
				if req.SessionID != session.ID || req.Command.Name != "/spike" {
					t.Fatalf("wrong owner execution: %+v", req)
				}
			default:
				t.Fatalf("owner reply did not reach executor: %s", f.logs.String())
			}
			remote.mu.Lock()
			gotState := linearWorkflowObject(t, remote.issues["parent-uuid"]["state"])["id"]
			remote.mu.Unlock()
			if gotState != stateID {
				t.Fatalf("forced state transition: %v", gotState)
			}
		})
	}
}

func assertObservationWarning(t *testing.T, logs string, v store.ConnectionVersion, ticketID, reason, state, stateReason, disposition string) {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		var entry map[string]any
		if json.Unmarshal([]byte(line), &entry) != nil || entry["reason"] != reason {
			continue
		}
		for key, want := range map[string]any{"level": "WARN", "project_id": float64(v.RepoID), "connection_version_id": float64(v.ID), "provider": string(v.Provider), "origin": string(v.Origin), "ticket_id": ticketID, "generation": float64(1), "state": state, "state_reason": stateReason, "disposition": disposition} {
			if entry[key] != want {
				t.Errorf("%s = %v, want %v: %s", key, entry[key], want, line)
			}
		}
		return
	}
	t.Fatalf("missing rejection warning %s: %s", reason, logs)
}

func TestRecoveryObservationRejectionWarnings(t *testing.T) {
	for _, reason := range []string{"subscription_revision", "related_subscription_revision", "connection_disabled_or_replaced", "container_mismatch", "retained_source_mismatch"} {
		t.Run(reason, func(t *testing.T) {
			f, _, v, sub, q := linearObservationFixture(t)
			ctx := t.Context()
			observation := store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: v.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: f.now}
			state, stateReason := "unavailable", "unavailable"
			switch reason {
			case "subscription_revision":
				observation.SubscriptionID, observation.SubscriptionRevision = sub.ID, sub.Revision
				sub.SecretVersion = "2"
				_, err := f.d.db.PutTicketSubscription(ctx, sub, sub.Revision)
				linearWorkflowCheck(t, err)
			case "related_subscription_revision":
				observation.SubscriptionID, observation.SubscriptionRevision = sub.ID, sub.Revision
				observation.RelatedSubscriptionID, observation.RelatedSubscriptionRevision = "missing-related", 1
			case "connection_disabled_or_replaced":
				replacement := v
				replacement.Enabled = false
				_, err := f.d.db.SetConnection(ctx, replacement, v.Revision)
				linearWorkflowCheck(t, err)
			case "retained_source_mismatch":
				handoffLinearObservation(t, f, v, sub)
				linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{v.RepoID: q}, map[int64]*candidate{}))
				fallthrough
			case "container_mismatch":
				v.RemoteID = "different-project"
				var err error
				v, err = f.d.db.SetConnection(ctx, v, v.Revision)
				linearWorkflowCheck(t, err)
				observation.ConnectionVersionID, observation.ContainerID = v.ID, v.RemoteID
			}
			linearWorkflowCheck(t, f.d.db.HandoffTicketObservation(ctx, observation))
			seen := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{v.RepoID: q}, seen))
			assertObservationWarning(t, f.logs.String(), v, "parent-uuid", reason, state, stateReason, "retained")
			pending, err := f.d.db.ListPendingTicketObservations(ctx, v.ID)
			linearWorkflowCheck(t, err)
			if len(pending) != 1 || pending[0].Generation != 1 {
				t.Fatalf("rejection lost retry: %+v", pending)
			}
			if reason != "retained_source_mismatch" && len(seen) != 0 {
				t.Fatal("rejected observation admitted")
			}
		})
	}
}

// The existing sight diagnostic occurs immediately before the discard clear.
// Intercept it to exercise cancellation and a concurrent new generation there.
type observationBoundaryHandler struct {
	slog.Handler
	beforeClear func()
}

func (h observationBoundaryHandler) Handle(ctx context.Context, record slog.Record) error {
	if strings.HasPrefix(record.Message, "skipping an issue that is not open;") {
		h.beforeClear()
	}
	return h.Handler.Handle(ctx, record)
}

func TestRecoveryClosedObservationClearBoundary(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("clear-fails=%t", failure), func(t *testing.T) {
			f, remote, v, sub, q := linearObservationFixture(t)
			remote.mu.Lock()
			remote.issues["parent-uuid"]["state"] = map[string]any{"id": "state-completed", "type": "completed"}
			remote.mu.Unlock()
			handoffLinearObservation(t, f, v, sub)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			q.log = slog.New(observationBoundaryHandler{Handler: f.d.log.Handler(), beforeClear: func() {
				if failure {
					cancel()
				} else {
					handoffLinearObservation(t, f, v, sub)
				}
			}})
			err := f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{v.RepoID: q}, map[int64]*candidate{})
			disposition, generation := "consumed", int64(2)
			if failure {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("clear failure lost: %v", err)
				}
				disposition, generation = "retained", 1
				if strings.Contains(f.logs.String(), `"disposition":"consumed"`) || !strings.Contains(f.logs.String(), `"error":"context canceled"`) {
					t.Fatalf("failure diagnostic: %s", f.logs.String())
				}
			} else {
				linearWorkflowCheck(t, err)
			}
			assertObservationWarning(t, f.logs.String(), v, "parent-uuid", "closed_unadmitted", "closed", "completed", disposition)
			pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
			linearWorkflowCheck(t, err)
			if len(pending) != 1 || pending[0].Generation != generation {
				t.Fatalf("generation lost: %+v", pending)
			}
		})
	}
}

func TestRecoveryObservationRereadWarnings(t *testing.T) {
	for _, reason := range []string{"ticket_not_found", "ticket_identity_mismatch"} {
		t.Run(reason, func(t *testing.T) {
			f, v, q, _, _ := recoveryHoldFixture(t)
			handoffRecoveryHold(t, f.d.db, v, 9)
			f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}, HTTPClient: &http.Client{Transport: discoveryUnitTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/issues/9") && reason == "ticket_not_found" {
					r.URL.Path = "/missing"
				}
				response, err := http.DefaultTransport.RoundTrip(r)
				if err != nil || !strings.HasSuffix(r.URL.Path, "/issues/9") {
					return response, err
				}
				defer response.Body.Close()
				var issue map[string]any
				if err := json.NewDecoder(response.Body).Decode(&issue); err != nil {
					return nil, err
				}
				issue["id"] = 100
				raw, err := json.Marshal(issue)
				if err != nil {
					return nil, err
				}
				response.Body = io.NopCloser(bytes.NewReader(raw))
				return response, nil
			})}})

			seen := map[int64]*candidate{}
			linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(t.Context()), map[int64]*repoQueue{v.RepoID: q}, seen))
			state, stateReason := "unavailable", "unavailable"
			if reason == "ticket_identity_mismatch" {
				state, stateReason = "open", ""
			}
			assertObservationWarning(t, f.logs.String(), v, "90", reason, state, stateReason, "retained")
			pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
			linearWorkflowCheck(t, err)
			if len(pending) != 1 || pending[0].Generation != 1 || len(seen) != 0 {
				t.Fatalf("reread rejection lost retry or admitted: %+v %+v", pending, seen)
			}
		})
	}
}
