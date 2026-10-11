package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
