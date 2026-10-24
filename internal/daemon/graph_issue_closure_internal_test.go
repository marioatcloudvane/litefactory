package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
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

// ownerWaitFleet is a GitHub repository whose tasks wait on their owner with
// nothing new to read, driven through the full Daemon.Tick (#1138). Every
// waiting session starts with its backlog flag set and no comment above its
// watermark; the fixture forge answers every issue as open.
type ownerWaitFleet struct {
	f        *discoveryHTTPFixture
	d        *Daemon
	repo     store.Repo
	now      time.Time
	waits    []store.Session
	parent   store.Session
	requests chan turn.Request
	// replies counts the owner comments a worker read and classified.
	replies *atomic.Int64
}

// newOwnerWaitFleet parks waits sessions at a running graph owner wait and adds
// one graph Plan parent with a retained draft and no due Plan work. hooked
// gives the repository a registered hook, a delivery since start and a secret;
// delivered=false keeps the hook but records no delivery.
func newOwnerWaitFleet(t *testing.T, waits int, hooked, delivered bool) *ownerWaitFleet {
	t.Helper()
	ctx := t.Context()
	f := newDiscoveryHTTPFixture(t)
	db := f.d.db
	_, err := db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	linearWorkflowCheck(t, err)
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "a", RemoteURL: "git@github.com:acme/a.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	linearWorkflowCheck(t, err)
	w := &ownerWaitFleet{f: f, repo: repo, requests: make(chan turn.Request, 64), replies: &atomic.Int64{}}
	at := f.now
	def, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "wait", Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
	linearWorkflowCheck(t, err)
	session := func(number int) store.Session { return w.session(t, number) }
	for i := range waits {
		s := session(10 + i)
		instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: s.ID, DefinitionID: def.ID, CurrentNode: "waiting", StartedAt: at})
		linearWorkflowCheck(t, err)
		attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: instance.ID, Node: "waiting", Limit: 3, StartedAt: at})
		linearWorkflowCheck(t, err)
		linearWorkflowCheck(t, db.FinishGraphNodeAttempt(ctx, attempt.ID, store.GraphSucceeded, "done", at))
		linearWorkflowCheck(t, db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "wait-question-" + strconv.Itoa(i), InstanceID: instance.ID, SourceAttemptID: attempt.ID, Node: "waiting", Kind: "question", Candidate: []byte("{}")}))
		w.waits = append(w.waits, s)
	}
	w.parent = w.planParent(t, 50)
	if owners, err := db.ListGraphOwnerWaitSessions(ctx); err != nil || len(owners) != waits {
		t.Fatalf("fixture owner waits: %d %v", len(owners), err)
	}
	if parents, err := db.ListGraphPlanSessions(ctx); err != nil || len(parents) != 1 {
		t.Fatalf("fixture Plan parents: %d %v", len(parents), err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	linearWorkflowCheck(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	w.now = time.Now().Add(2 * time.Hour)
	resolver := secrets.Resolver{Keychain: func(context.Context, string, string) (string, error) { return "fleet-signing-secret", nil }}
	w.d, err = New(Options{DB: db, Forge: f.client, Filter: f.d.filter, Turn: recoveryHoldExecutor{w.requests}, Reporter: stubReporter{}, InstanceID: "owner-wait-fleet", Logger: f.d.log, Now: func() time.Time { return w.now }, DiscoveryInterval: MaxDiscoveryInterval, MaxConcurrentRuns: 4, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): capturedClassifierFunc(func(context.Context, agent.Classification) (agent.Mode, error) {
		w.replies.Add(1)
		return agent.ModeSpike, nil
	})}, ToolProfiles: config.DefaultToolProfiles().Permissions,
		Webhook: &WebhookOptions{Listener: ln, Secrets: resolver, Now: func() time.Time { return w.now }}})
	linearWorkflowCheck(t, err)
	_, err = db.InitPollCursor(ctx, w.now.Add(-time.Minute))
	linearWorkflowCheck(t, err)
	if hooked {
		linearWorkflowCheck(t, db.SetRepoWebhook(ctx, repo.ID, 7, "https://callback.invalid/webhook", w.now))
	}
	// The listing just ran: no heartbeat here is a listing tick unless a test
	// makes it one.
	w.d.schedule.ListingFinished(repo.ID, w.now)
	if delivered {
		w.d.schedule.WebhookAccepted(repo.ID, w.now)
	}
	return w
}

// session is a quiet task on the fleet's repository with its backlog flag set.
func (w *ownerWaitFleet) session(t *testing.T, number int) store.Session {
	t.Helper()
	ctx := t.Context()
	s, err := w.f.d.db.GetOrCreateSession(ctx, w.repo, number)
	linearWorkflowCheck(t, err)
	// The fixture forge's only comment on an issue is number*1000+1.
	_, _, err = w.f.d.db.BootstrapWatermark(ctx, s.ID, int64(number*1000+1))
	linearWorkflowCheck(t, err)
	linearWorkflowCheck(t, w.f.d.db.SetSessionNeedsPoll(ctx, s.ID))
	return s
}

// planParent is a graph Plan parent with a retained draft and no due Plan work.
func (w *ownerWaitFleet) planParent(t *testing.T, number int) store.Session {
	t.Helper()
	s := w.session(t, number)
	_, err := w.f.d.db.ReservePlanStory(t.Context(), s.TaskRef, "story-001")
	linearWorkflowCheck(t, err)
	_, err = w.f.d.db.AdmitPlanWorkflowPath(t.Context(), s.ID, "graph")
	linearWorkflowCheck(t, err)
	return s
}

// fleetTick is what one full heartbeat did per session: the slots its workers
// acquired and the offers that were refused for capacity or by the discovery
// budget.
type fleetTick struct {
	slots, capacity, budget map[int64]int
	dispatched              map[int64]bool
}

func (t fleetTick) offers(session int64) int {
	return t.slots[session] + t.capacity[session] + t.budget[session]
}

// heartbeat runs one full tick 30 seconds after the previous one and counts,
// from the daemon's own records, what each session got.
func (w *ownerWaitFleet) heartbeat(t *testing.T) fleetTick {
	t.Helper()
	w.now = w.now.Add(DefaultInterval)
	return w.tick(t)
}

func (w *ownerWaitFleet) tick(t *testing.T) fleetTick { return w.tickThen(t, func() {}) }

// tickThen runs one full tick and calls then before waiting for its workers.
func (w *ownerWaitFleet) tickThen(t *testing.T, then func()) fleetTick {
	t.Helper()
	before := w.f.logs.Len()
	if _, err := w.d.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	then()
	if err := w.d.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := fleetTick{slots: map[int64]int{}, capacity: map[int64]int{}, budget: map[int64]int{}, dispatched: map[int64]bool{}}
	for _, line := range strings.Split(w.f.logs.String()[before:], "\n") {
		var rec struct {
			Msg     string `json:"msg"`
			Session int64  `json:"session_id"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Session == 0 {
			continue
		}
		switch rec.Msg {
		case "worker dispatched":
			got.slots[rec.Session]++
		case "work deferred for capacity":
			got.capacity[rec.Session]++
		case "issue read deferred by the discovery budget":
			got.budget[rec.Session]++
		}
	}
	for len(w.requests) > 0 {
		got.dispatched[(<-w.requests).SessionID] = true
	}
	return got
}

func (w *ownerWaitFleet) all() []store.Session { return append(slices.Clone(w.waits), w.parent) }

func (w *ownerWaitFleet) needsPoll(t *testing.T, s store.Session) bool {
	t.Helper()
	got, err := w.d.db.GetSession(t.Context(), s.ID)
	linearWorkflowCheck(t, err)
	return got.NeedsPoll
}

// sources runs the owner-wait and Plan-parent sources alone for the fleet's
// repository and returns the sessions they enrolled.
func (w *ownerWaitFleet) sources(t *testing.T, listing bool) (waits, parents map[int64]bool) {
	t.Helper()
	q := &repoQueue{repo: w.repo, forgeRepo: github.Repo{Owner: w.repo.Owner, Name: w.repo.Name}, log: w.d.log, listing: listing}
	linearWorkflowCheck(t, w.d.graphOwnerWaits(t.Context(), map[int64]*repoQueue{w.repo.ID: q}, map[int64]*candidate{}))
	waits = map[int64]bool{}
	for _, c := range q.candidates {
		waits[c.session.ID] = true
	}
	q = &repoQueue{repo: w.repo, forgeRepo: github.Repo{Owner: w.repo.Owner, Name: w.repo.Name}, log: w.d.log, listing: listing}
	linearWorkflowCheck(t, w.d.graphPlanParents(t.Context(), map[int64]*repoQueue{w.repo.ID: q}, map[int64]*candidate{}))
	parents = map[int64]bool{}
	for _, c := range q.candidates {
		parents[c.session.ID] = true
	}
	return waits, parents
}

// C5 (#1138): on a webhook-healthy repository, between listings, each waiting
// session with its backlog flag set is observed once and then takes no slot
// and gets no offer.
func TestOwnerWaitFleetWebhookObservedOnce(t *testing.T) {
	w := newOwnerWaitFleet(t, 6, true, true)
	slots, later := map[int64]int{}, map[int64]int{}
	cleared := map[int64]bool{}
	for i := range 5 {
		got := w.heartbeat(t)
		for _, s := range w.all() {
			slots[s.ID] += got.slots[s.ID]
			if cleared[s.ID] {
				later[s.ID] += got.offers(s.ID)
			}
			if !w.needsPoll(t, s) {
				cleared[s.ID] = true
			}
		}
		if len(got.dispatched) != 0 {
			t.Fatalf("tick %d ran a turn for a quiet task: %v", i, got.dispatched)
		}
	}
	for _, s := range w.all() {
		if slots[s.ID] != 1 || later[s.ID] != 0 || !cleared[s.ID] {
			t.Errorf("session %d (%s): %d slots, %d offers after its observation, flag cleared %t", s.ID, s.TaskRef, slots[s.ID], later[s.ID], cleared[s.ID])
		}
	}
	if t.Failed() {
		t.Log(w.f.logs.String())
	}
}

// C7 (#1138): on a non-listing tick of a webhook-healthy GitHub repository an
// owner wait with no other source is not enrolled; the listing tick enrolls it,
// and a candidate another source already found still gets the source.
func TestGraphOwnerWaitWebhookHealthyWaitsForListing(t *testing.T) {
	w := newOwnerWaitFleet(t, 1, true, true)
	w.heartbeat(t) // loads the webhook secret
	if waits, _ := w.sources(t, false); len(waits) != 0 {
		t.Fatalf("owner wait enrolled between listings on a webhook-healthy repository: %v", waits)
	}
	if waits, _ := w.sources(t, true); !waits[w.waits[0].ID] {
		t.Fatalf("owner wait not enrolled on its listing tick: %v", waits)
	}
	q := &repoQueue{repo: w.repo, forgeRepo: github.Repo{Owner: "acme", Name: "a"}, log: w.d.log}
	prior := q.candidate(w.waits[0], mustRef(t, w.waits[0].TaskRef))
	prior.addSource("backlog")
	linearWorkflowCheck(t, w.d.graphOwnerWaits(t.Context(), map[int64]*repoQueue{w.repo.ID: q}, map[int64]*candidate{w.waits[0].ID: prior}))
	if !slices.Equal(prior.sources, []string{"backlog", "graph_owner_wait"}) || len(q.candidates) != 0 {
		t.Fatalf("seen candidate sources %v, new candidates %d", prior.sources, len(q.candidates))
	}
}

func mustRef(t *testing.T, ref string) naming.Ref {
	t.Helper()
	r, err := naming.ParseRef(ref)
	linearWorkflowCheck(t, err)
	return r
}

// C10 (#1138): without a healthy webhook (no hook, or a hook with no delivery
// since start) owner waits and Plan parents are offered on every tick, and an
// owner reply is dispatched on the next heartbeat with no listing between.
func TestOwnerWaitFleetWithoutHealthyWebhookOfferedEveryTick(t *testing.T) {
	for _, tc := range []struct {
		name      string
		hooked    bool
		delivered bool
	}{{"no hook", false, false}, {"hook without delivery", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := newOwnerWaitFleet(t, 2, tc.hooked, tc.delivered)
			for i := range 3 {
				w.heartbeat(t)
				waits, parents := w.sources(t, false)
				if !waits[w.waits[0].ID] || !waits[w.waits[1].ID] || !parents[w.parent.ID] {
					t.Fatalf("heartbeat %d: waits %v parents %v", i, waits, parents)
				}
			}
			for _, s := range w.all() {
				if w.needsPoll(t, s) {
					t.Fatalf("session %d not observed", s.ID)
				}
			}
			// The owner replies on the first waiting task.
			w.f.mu.Lock()
			w.f.versions["/repos/acme/a/issues/10/comments"]++
			w.f.mu.Unlock()
			if got := w.heartbeat(t); got.slots[w.waits[0].ID] != 1 || w.replies.Load() != 1 {
				t.Fatalf("owner reply not read on the next heartbeat: %+v replies=%d\n%s", got, w.replies.Load(), w.f.logs.String())
			}
		})
	}
}

// C2 (#1138): on a webhook-healthy repository a waiting task still gets a
// worker between listings when a webhook hint or its backlog flag names it.
func TestOwnerWaitFleetWebhookNewInputStillDispatched(t *testing.T) {
	w := newOwnerWaitFleet(t, 2, true, true)
	for range 3 {
		w.heartbeat(t)
	}
	for _, s := range w.all() {
		if w.needsPoll(t, s) {
			t.Fatalf("session %d not observed", s.ID)
		}
	}
	if got := w.heartbeat(t); got.offers(w.waits[0].ID) != 0 {
		t.Fatalf("quiet wait offered: %+v", got)
	}
	// A webhook hint names the first wait, and the owner did reply there.
	w.f.mu.Lock()
	w.f.versions["/repos/acme/a/issues/10/comments"]++
	w.f.mu.Unlock()
	w.d.webhook.Wake(w.repo.ID, 10)
	if got := w.heartbeat(t); got.slots[w.waits[0].ID] != 1 || w.replies.Load() != 1 {
		t.Fatalf("hinted owner reply not read on the next heartbeat: %+v replies=%d\n%s", got, w.replies.Load(), w.f.logs.String())
	}
	// The backlog flag names the second.
	linearWorkflowCheck(t, w.d.db.SetSessionNeedsPoll(t.Context(), w.waits[1].ID))
	if got := w.heartbeat(t); got.slots[w.waits[1].ID] != 1 {
		t.Fatalf("flagged owner wait not offered on the next heartbeat: %+v", got)
	}
	if w.needsPoll(t, w.waits[1]) {
		t.Fatal("flagged owner wait not observed")
	}
}

// C11 (#1138): waiting sessions first deferred for capacity or by the
// discovery budget are observed once when capacity returns and are then left
// alone until new input or the listing.
func TestOwnerWaitFleetDeferredThenObservedOnce(t *testing.T) {
	w := newOwnerWaitFleet(t, 6, true, true)
	// Tick 1: every slot is held by other work, so every waiting session is
	// deferred for capacity. Tick 2: the discovery budget is spent, so every
	// one is deferred by the budget. Both keep the backlog flag.
	held := []*stepReservation{}
	for range 4 {
		r, reason := w.d.reserveInput()
		if r == nil {
			t.Fatalf("prefill: %s", reason)
		}
		held = append(held, r)
	}
	w.now = w.now.Add(DefaultInterval)
	starved := w.tickThen(t, func() {
		for _, r := range held {
			r.abandon()
		}
	})
	w.now = w.now.Add(DefaultInterval)
	w.d.discovery.tick(w.now)
	w.d.discovery.mu.Lock()
	w.d.discovery.tokens = 0
	w.d.discovery.mu.Unlock()
	spent := w.tick(t)
	for _, s := range w.all() {
		if starved.capacity[s.ID] != 1 || spent.budget[s.ID] != 1 || starved.slots[s.ID]+spent.slots[s.ID] != 0 {
			t.Fatalf("session %d: capacity tick %+v, budget tick %+v", s.ID, starved, spent)
		}
		if !w.needsPoll(t, s) {
			t.Fatalf("session %d lost its backlog flag on a deferral", s.ID)
		}
	}
	slots, later := map[int64]int{}, map[int64]int{}
	cleared := map[int64]bool{}
	for range 5 {
		got := w.heartbeat(t)
		for _, s := range w.all() {
			slots[s.ID] += got.slots[s.ID]
			if cleared[s.ID] {
				later[s.ID] += got.offers(s.ID)
			}
			if !w.needsPoll(t, s) {
				cleared[s.ID] = true
			}
		}
	}
	for _, s := range w.all() {
		if slots[s.ID] != 1 || later[s.ID] != 0 || !cleared[s.ID] {
			t.Errorf("session %d: %d slots after the starved tick, %d offers after its observation, flag cleared %t", s.ID, slots[s.ID], later[s.ID], cleared[s.ID])
		}
	}
}
