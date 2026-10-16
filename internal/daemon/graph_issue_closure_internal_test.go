package daemon

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// linearOwnerWait is a Linear task whose finished graph waits for its owner,
// driven by a daemon whose Linear requests are counted (#918).
type linearOwnerWait struct {
	f        *discoveryHTTPFixture
	remote   *linearWorkflowServer
	v        store.ConnectionVersion
	sub      store.TicketSubscription
	q        *repoQueue
	d        *Daemon
	now      time.Time
	session  store.Session
	instance store.GraphInstance
	linear   *atomic.Int64
	requests chan turn.Request
}

func newLinearOwnerWait(t *testing.T) *linearOwnerWait {
	t.Helper()
	ctx := t.Context()
	f, remote, v, sub, q := linearObservationFixture(t)
	handoffLinearObservation(t, f, v, sub)
	seen := map[int64]*candidate{}
	linearWorkflowCheck(t, f.d.drainTicketObservations(f.d.discoveryContext(ctx), map[int64]*repoQueue{v.RepoID: q}, seen))
	for _, c := range seen {
		if c.discoveryReservation != nil {
			c.discoveryReservation.finish()
		}
	}
	session, err := f.d.db.GetSessionByTicket(ctx, ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: "parent-uuid"})
	linearWorkflowCheck(t, err)
	_, _, err = f.d.db.BootstrapWatermark(ctx, session.ID, 0)
	linearWorkflowCheck(t, err)

	at := f.now
	def, err := f.d.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "wait", Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
	linearWorkflowCheck(t, err)
	instance, err := f.d.db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: def.ID, CurrentNode: "waiting", StartedAt: at})
	linearWorkflowCheck(t, err)
	attempt, err := f.d.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "waiting", Limit: 3, StartedAt: at})
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, f.d.db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
	linearWorkflowCheck(t, f.d.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "linear-wait-question", InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "waiting", Kind: "question", Candidate: []byte("{}")}))
	linearWorkflowCheck(t, f.d.db.ClearSessionNeedsPoll(ctx, session.ID))
	waits, err := f.d.db.ListGraphOwnerWaitSessions(ctx)
	linearWorkflowCheck(t, err)
	if len(waits) != 1 || waits[0].ID != session.ID {
		t.Fatalf("fixture has no live owner wait: %+v", waits)
	}

	w := &linearOwnerWait{f: f, remote: remote, v: v, sub: sub, q: q, session: session, instance: instance, linear: &atomic.Int64{}, requests: make(chan turn.Request, 16)}
	linearHost := strings.TrimPrefix(remote.server.URL, "http://")
	factory := projectconnection.NewFactory(projectconnection.Options{DB: f.d.db, Logger: f.d.log, HTTPClient: &http.Client{Transport: discoveryUnitTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case linearHost:
			w.linear.Add(1)
			return http.DefaultTransport.RoundTrip(r)
		case "api.github.com":
			body := `{}`
			switch r.URL.Path {
			case "/user":
				body = `{"id":99,"login":"bot"}`
			case "/repos/acme/linear-code":
				body = `{"id":1,"full_name":"acme/linear-code"}`
			}
			h := http.Header{"Content-Type": {"application/json"}, "X-Ratelimit-Limit": {"5000"}, "X-Ratelimit-Remaining": {"4900"}, "X-Ratelimit-Resource": {"core"}, "X-Ratelimit-Reset": {"4102444800"}}
			return &http.Response{StatusCode: http.StatusOK, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})}, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
		if name == "LINEAR_TOKEN" {
			return "test-linear-token", true
		}
		return "offline-token", true
	}}})
	w.now = time.Now().Add(2 * time.Hour)
	w.d, err = New(Options{DB: f.d.db, Forge: f.client, Filter: f.d.filter, Connections: factory, Turn: recoveryHoldExecutor{w.requests}, Reporter: stubReporter{}, InstanceID: "linear-owner-wait", Logger: f.d.log, Now: func() time.Time { return w.now }, DiscoveryInterval: MaxDiscoveryInterval, MaxConcurrentRuns: 4, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions})
	linearWorkflowCheck(t, err)
	_, err = f.d.db.InitPollCursor(ctx, w.now.Add(-time.Minute))
	linearWorkflowCheck(t, err)
	// The listing just ran: no heartbeat in this test is a listing tick
	// unless it says so.
	w.d.schedule.ListingFinished(q.repo.ID, w.now)
	// The admitting observation and backlog flag settle on the first tick.
	w.heartbeat(t)
	pending, err := f.d.db.ListPendingTicketObservations(ctx, v.ID)
	linearWorkflowCheck(t, err)
	if s, err := f.d.db.GetSession(ctx, session.ID); err != nil || s.NeedsPoll || len(pending) != 0 {
		t.Fatalf("fixture did not settle: %+v %+v %v", s, pending, err)
	}
	return w
}

// heartbeat runs one tick 30 seconds after the previous one and returns how
// many Linear requests it made and which sessions it dispatched.
func (w *linearOwnerWait) heartbeat(t *testing.T) (int64, map[int64]bool) {
	t.Helper()
	w.now = w.now.Add(DefaultInterval)
	before := w.linear.Load()
	if _, err := w.d.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := w.d.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	dispatched := map[int64]bool{}
	for len(w.requests) > 0 {
		dispatched[(<-w.requests).SessionID] = true
	}
	return w.linear.Load() - before, dispatched
}

// candidates runs only the owner-wait source for one repository queue.
func (w *linearOwnerWait) candidates(t *testing.T, listing bool) []*candidate {
	t.Helper()
	q := &repoQueue{repo: w.q.repo, forgeRepo: w.q.forgeRepo, log: w.q.log, listing: listing}
	linearWorkflowCheck(t, w.d.graphOwnerWaits(t.Context(), map[int64]*repoQueue{q.repo.ID: q}, map[int64]*candidate{}))
	return q.candidates
}

// C1, C2: an idle Linear owner wait is read at the listing cadence, not on
// every heartbeat.
func TestGraphOwnerWaitLinearIdleHeartbeatsReadNothing(t *testing.T) {
	w := newLinearOwnerWait(t)
	if got := w.candidates(t, false); len(got) != 0 {
		t.Fatalf("Linear owner wait offered on a heartbeat without listing: %d", len(got))
	}
	if got := w.candidates(t, true); len(got) != 1 || got[0].session.ID != w.session.ID {
		t.Fatalf("Linear owner wait not offered on its listing tick: %+v", got)
	}
	for i := range 10 {
		if n, dispatched := w.heartbeat(t); n != 0 || len(dispatched) != 0 {
			t.Fatalf("heartbeat %d: %d Linear requests, dispatched %v\n%s", i, n, dispatched, w.f.logs.String())
		}
	}
	if strings.Contains(w.f.logs.String(), "issue read deferred by the discovery budget") {
		t.Fatalf("idle owner wait reached the discovery budget:\n%s", w.f.logs.String())
	}
	w.now = w.now.Add(MaxDiscoveryInterval)
	if n, _ := w.heartbeat(t); n == 0 {
		t.Fatal("the listing tick did not read the Linear owner wait")
	}
	if got, err := w.d.db.GetGraphInstance(t.Context(), w.instance.ID); err != nil || got.Status == store.GraphCancelled {
		t.Fatalf("open issue changed graph: %+v %v", got, err)
	}
}

// C3: an owner reply that arrives by webhook dispatches the waiting task on
// the next heartbeat, with no listing due.
func TestGraphOwnerWaitLinearWebhookReplyDispatchesNextTick(t *testing.T) {
	w := newLinearOwnerWait(t)
	if n, dispatched := w.heartbeat(t); n != 0 || len(dispatched) != 0 {
		t.Fatalf("idle heartbeat: %d Linear requests, dispatched %v", n, dispatched)
	}
	w.remote.addReply("owner-reply", "/spike continue this Linear ticket", "human-uuid")
	linearWorkflowCheck(t, w.d.db.HandoffSubscriptionObservation(t.Context(), w.sub.ID, w.sub.Revision, store.PendingTicketObservation{RepoID: w.v.RepoID, ConnectionVersionID: w.v.ID, Provider: w.v.Provider, Origin: w.v.Origin, Container: w.v.Destination, ContainerID: w.v.RemoteID, TicketID: "parent-uuid", TicketKey: "parent-uuid", ObservedAt: w.now}))
	n, dispatched := w.heartbeat(t)
	if n == 0 || !dispatched[w.session.ID] {
		t.Fatalf("webhook reply not dispatched on the next tick: %d Linear requests, dispatched %v\n%s", n, dispatched, w.f.logs.String())
	}
}

// C5: closing the Linear issue still cancels the waiting graph on the next
// listing tick.
func TestGraphOwnerWaitLinearClosureCancelsOnListingTick(t *testing.T) {
	w := newLinearOwnerWait(t)
	w.remote.mu.Lock()
	w.remote.issues["parent-uuid"]["state"] = map[string]any{"id": "state-completed", "type": "completed", "name": "Done"}
	w.remote.mu.Unlock()
	w.heartbeat(t)
	if got, err := w.d.db.GetGraphInstance(t.Context(), w.instance.ID); err != nil || got.Status == store.GraphCancelled {
		t.Fatalf("closure observed without a listing: %+v %v", got, err)
	}
	w.now = w.now.Add(MaxDiscoveryInterval)
	w.heartbeat(t)
	got, err := w.d.db.GetGraphInstance(t.Context(), w.instance.ID)
	if err != nil || got.Status != store.GraphCancelled {
		t.Fatalf("closed Linear issue did not cancel its graph: %+v %v\n%s", got, err, w.f.logs.String())
	}
}

// C4: without a Linear ticket connection the wait is offered on every tick.
func TestGraphOwnerWaitGitHubOfferedEveryTick(t *testing.T) {
	w := newLinearOwnerWait(t)
	_, err := w.d.db.DisableConnection(context.WithoutCancel(t.Context()), w.v.RepoID, store.TicketConnection, w.v.Revision)
	linearWorkflowCheck(t, err)
	gh := w.v
	gh.ID, gh.Provider, gh.Origin, gh.Destination, gh.RemoteID, gh.Enabled = 0, ticket.GitHub, "https://api.github.com", "acme/linear-code", "1", true
	gh.CredentialEnv, gh.Linear = "CODE_TOKEN", store.LinearConfiguration{}
	current, err := w.d.db.CurrentConnection(t.Context(), w.v.RepoID, store.TicketConnection)
	linearWorkflowCheck(t, err)
	gh.Revision = current.Revision
	_, err = w.d.db.SetConnection(t.Context(), gh, current.Revision)
	linearWorkflowCheck(t, err)
	if got := w.candidates(t, false); len(got) != 1 || got[0].session.ID != w.session.ID {
		t.Fatalf("GitHub owner wait not offered on a heartbeat: %+v", got)
	}
}
