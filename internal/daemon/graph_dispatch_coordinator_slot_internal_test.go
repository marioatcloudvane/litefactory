package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// coordinatorSlotGraph is a coordinator graph with bookkeeping nodes and one
// agent stage, as the shipped Implement graph has.
const coordinatorSlotGraph = `digraph {team=test;mode=implement;profile_version=1;start=await;
 await [lf_action=lf_implement_await,lf_outcomes="go",lf_limit=8];
 sync_start_resolve [lf_action=lf_agent,lf_outcomes="success",lf_agent_markdown="sync-resolver",lf_limit=3];
 await -> sync_start_resolve [lf_on=go]; sync_start_resolve -> await [lf_on=success];}`

// coordinatorSlotExecutor is a turn that holds until released, so a test can
// look at the pools while workers run.
type coordinatorSlotExecutor struct {
	mu       sync.Mutex
	started  chan int64
	release  chan struct{}
	sessions []int64
}

func (e *coordinatorSlotExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	e.mu.Lock()
	e.sessions = append(e.sessions, req.SessionID)
	e.mu.Unlock()
	e.started <- req.SessionID
	select {
	case <-e.release:
	case <-ctx.Done():
	}
	return turn.Result{}, nil
}

type coordinatorSlotFixture struct {
	d    *Daemon
	db   *store.DB
	repo store.Repo
	turn *coordinatorSlotExecutor
	logs *syncLogBuffer
	now  time.Time
	pin  store.GraphDefinition
	// held are run-pool slots the test took to stand in for story workers.
	held     int
	released sync.Once
}

type syncLogBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newCoordinatorSlotFixture(t *testing.T) *coordinatorSlotFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4900")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method == http.MethodGet && len(parts) == 5 && parts[3] == "issues" {
			number, _ := strconv.Atoi(parts[4])
			_, _ = fmt.Fprintf(w, `{"id":%d,"number":%d,"title":"t","state":"open","user":{"id":7,"login":"someone","type":"User"},"labels":[{"name":"Mode: Implement"}]}`, 1000+number, number)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	client, err := github.New(github.Options{APIBase: server.URL, Token: secrets.NewToken("offline-token")})
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncLogBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99}, logger)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(t.TempDir(), "w")})
	if err != nil {
		t.Fatal(err)
	}
	f := &coordinatorSlotFixture{db: db, repo: repo, logs: logs, now: time.Now().UTC(), turn: &coordinatorSlotExecutor{started: make(chan int64, 32), release: make(chan struct{})}}
	f.d, err = New(Options{DB: db, Forge: client, Filter: filter, Turn: f.turn, Reporter: stubReporter{}, InstanceID: "coordinator-slot", Logger: logger, Now: func() time.Time { return f.now }, MaxConcurrentRuns: MinConcurrentRuns, DefaultAgent: agent.Kind(store.AgentClaude), Agents: turn.RunnerSet{agent.Kind(store.AgentClaude): nil}, Classifiers: map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions})
	if err != nil {
		t.Fatal(err)
	}
	if f.pin, err = db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "implement", ProfileVersion: 1, Path: "test/implement.dot", Digest: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.releaseWorkers()
		for ; f.held > 0; f.held-- {
			f.d.pool.done()
		}
		_ = f.d.Quiesce(context.Background())
	})
	return f
}

// fillRunPool takes every free run-pool slot, as story workers would.
func (f *coordinatorSlotFixture) fillRunPool(t *testing.T) {
	t.Helper()
	for f.d.pool.try() {
		f.held++
	}
}

// freeRunSlot gives one held run-pool slot back.
func (f *coordinatorSlotFixture) freeRunSlot(t *testing.T) {
	t.Helper()
	if f.held == 0 {
		t.Fatal("no run slot held")
	}
	f.held--
	f.d.pool.done()
}

func (f *coordinatorSlotFixture) releaseWorkers() {
	f.released.Do(func() { close(f.turn.release) })
}

// coordinator parks a coordinator instance on its own issue at node, due at due,
// with the graph retained the way a captured launch retains it.
func (f *coordinatorSlotFixture) coordinator(t *testing.T, issue int, node string, due time.Time) store.GraphDispatch {
	t.Helper()
	return f.coordinatorWithGraph(t, issue, node, due, coordinatorSlotGraph)
}

func (f *coordinatorSlotFixture) coordinatorWithGraph(t *testing.T, issue int, node string, due time.Time, graph string) store.GraphDispatch {
	t.Helper()
	ctx := t.Context()
	session, err := f.db.GetOrCreateSession(ctx, f.repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.db.BootstrapWatermark(ctx, session.ID, 0); err != nil {
		t.Fatal(err)
	}
	files, err := json.Marshal(map[string][]byte{"implement.dot": []byte(graph)})
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: int64(500 + issue), DefinitionID: f.pin.ID, Start: node, ContentDigest: strings.Repeat("c", 64), TopologyDigest: strings.Repeat("d", 64), Files: files})
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err := f.db.ParkCapturedGraphInstanceAfter(ctx, admitted.Instance.ID, node, due, due, 0, admitted.LaunchID)
	if err != nil {
		t.Fatal(err)
	}
	return dispatch
}

// stories parks n story-worker dispatches due at due: each its own running
// story with its own worker session.
func (f *coordinatorSlotFixture) stories(t *testing.T, n int, due time.Time) []store.GraphDispatch {
	t.Helper()
	ctx := t.Context()
	var out []store.GraphDispatch
	for i := range n {
		// One parent per story keeps every story within the running cap.
		parent := fmt.Sprintf("github:acme/api#%d", 900+i)
		seed := store.ImplementStorySeed{Key: "story-001", Branch: fmt.Sprintf("lf/%d-story-001", 900+i)}
		if err := f.db.EnsureImplementStories(ctx, parent, 1, []store.ImplementStorySeed{seed}, f.now); err != nil {
			t.Fatal(err)
		}
		row, err := f.db.GetImplementStory(ctx, parent, 1, seed.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.TransitionImplementStory(ctx, store.ImplementTransition{Parent: parent, Generation: 1, Key: seed.Key, From: store.ImplementWaiting, To: store.ImplementRunning, ExpectedRevision: row.Revision, At: f.now}); err != nil {
			t.Fatal(err)
		}
		session, err := f.db.GetOrCreateSession(ctx, f.repo, 100+i)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.db.SetImplementWorkerSession(ctx, parent, 1, seed.Key, session.ID, f.now); err != nil {
			t.Fatal(err)
		}

		if _, err = f.db.AdmitWorkflowPath(ctx, session.ID, "graph"); err != nil {
			t.Fatal(err)
		}
		files, err := json.Marshal(map[string][]byte{"implement.dot": []byte(coordinatorSlotGraph)})
		if err != nil {
			t.Fatal(err)
		}
		admitted, err := f.db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "implement", SessionID: session.ID, TriggerID: 22, DefinitionID: f.pin.ID, Start: "await", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: files})
		if err != nil {
			t.Fatal(err)
		}
		dispatch, err := f.db.ParkCapturedGraphInstanceAfter(ctx, admitted.Instance.ID, "await", due.Add(time.Duration(i)*time.Millisecond), due, 0, admitted.LaunchID)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, dispatch)
	}
	return out
}

// dispatch runs the tick's graph-step phase once.
func (f *coordinatorSlotFixture) dispatch(t *testing.T) TickResult {
	t.Helper()
	var result TickResult
	if err := f.d.dispatchGraphSteps(t.Context(), map[int64]store.Repo{f.repo.ID: f.repo}, f.now, &result); err != nil {
		t.Fatalf("%v\n%s", err, f.logs)
	}
	return result
}

func (f *coordinatorSlotFixture) awaitStart(t *testing.T) int64 {
	t.Helper()
	select {
	case session := <-f.turn.started:
		return session
	case <-time.After(10 * time.Second):
		t.Fatalf("no worker started\n%s", f.logs)
		return 0
	}
}

func (f *coordinatorSlotFixture) status(t *testing.T, dispatch store.GraphDispatch) string {
	t.Helper()
	d, err := f.db.LatestGraphDispatch(t.Context(), dispatch.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != dispatch.ID {
		return fmt.Sprintf("replaced by %d (%s)", d.ID, d.Status)
	}
	return d.Status
}

// settle gives back the held run slots, releases running workers and waits
// for both pools to drain.
func (f *coordinatorSlotFixture) settle(t *testing.T) {
	t.Helper()
	for ; f.held > 0; f.held-- {
		f.d.pool.done()
	}
	f.releaseWorkers()
	if err := f.d.Quiesce(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// C1, C2, C5: with every run slot taken, a due coordinator step starts on the
// reserved slot in the same tick, leaves the run pool as it was, finishes, and
// is joined by Quiesce.
func TestCoordinatorStepRunsOnTheReservedSlotWhileTheRunPoolIsFull(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	dispatch := f.coordinator(t, 1, "await", f.now.Add(-time.Minute))
	f.fillRunPool(t)
	before := len(f.d.pool.slots)

	if result := f.dispatch(t); result.Dispatched != 1 || result.Deferred != 0 {
		t.Fatalf("the coordinator step is dispatched, not deferred: %+v\n%s", result, f.logs)
	}
	if session := f.awaitStart(t); session != dispatch.SessionID {
		t.Fatalf("started session %d, want %d", session, dispatch.SessionID)
	}
	if got := len(f.d.coordinator.slots); got != 1 {
		t.Fatalf("the reserved slot holds the coordinator worker: %d", got)
	}
	if got := len(f.d.pool.slots); got != before {
		t.Fatalf("the run pool changed from %d to %d", before, got)
	}
	// C5: Quiesce does not return while the reserved-slot worker runs.
	short, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	for ; f.held > 0; f.held-- {
		f.d.pool.done()
	}
	if len(f.d.pool.slots) != 0 {
		t.Fatal("only the reserved-slot worker is left running")
	}
	if err := f.d.Quiesce(short); err == nil {
		t.Fatal("Quiesce returned while the coordinator worker was running")
	}
	f.settle(t)
	if got := f.status(t, dispatch); got != "finished" {
		t.Fatalf("the coordinator step finished: %s\n%s", got, f.logs)
	}
	if len(f.d.pool.slots) != 0 || len(f.d.coordinator.slots) != 0 {
		t.Fatalf("a slot leaked: pool=%d coordinator=%d", len(f.d.pool.slots), len(f.d.coordinator.slots))
	}
}

// C3, C6: eight older story dispatches blocked on a full run pool neither start
// nor keep a later coordinator step out of the tick.
func TestCoordinatorStepIsSelectedPastBlockedStoryDispatches(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	stories := f.stories(t, graphDispatchLimit, f.now.Add(-time.Hour))
	dispatch := f.coordinator(t, 1, "await", f.now.Add(-time.Minute))
	f.fillRunPool(t)

	result := f.dispatch(t)
	if result.Dispatched != 1 || result.Deferred != graphDispatchLimit {
		t.Fatalf("one coordinator dispatched, every story deferred: %+v\n%s", result, f.logs)
	}
	if session := f.awaitStart(t); session != dispatch.SessionID {
		t.Fatalf("started session %d, want the coordinator's %d", session, dispatch.SessionID)
	}
	for _, story := range stories {
		if got := f.status(t, story); got != "pending" {
			t.Fatalf("story dispatch %d is %s, want pending", story.ID, got)
		}
	}
	if !strings.Contains(f.logs.String(), `"reason":"temporary capacity deferral: pool=shared`) {
		t.Fatalf("a story deferral names its reason:\n%s", f.logs)
	}
	f.settle(t)
	if got := f.status(t, dispatch); got != "finished" {
		t.Fatalf("the coordinator step finished: %s", got)
	}
}

// C7: the reserved slot carries bookkeeping only. A coordinator's agent stage
// waits for run capacity while another coordinator's bookkeeping runs on the
// reserved slot, and takes a run slot once one is free.
func TestCoordinatorAgentStageNeverTakesTheReservedSlot(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	a := f.coordinator(t, 1, "sync_start_resolve", f.now.Add(-2*time.Minute))
	b := f.coordinator(t, 2, "await", f.now.Add(-time.Minute))
	f.fillRunPool(t)

	result := f.dispatch(t)
	if result.Dispatched != 1 || result.Deferred != 1 {
		t.Fatalf("B dispatched, A deferred: %+v\n%s", result, f.logs)
	}
	if session := f.awaitStart(t); session != b.SessionID {
		t.Fatalf("started session %d, want B's %d", session, b.SessionID)
	}
	if got := f.status(t, a); got != "pending" {
		t.Fatalf("A is %s, want pending", got)
	}
	if !strings.Contains(f.logs.String(), fmt.Sprintf(`"msg":"graph step deferred for capacity","repo":"acme/api","task":"github:acme/api#1","session_id":%d,"graph_dispatch_id":%d,"graph_node":"sync_start_resolve","reason":"temporary capacity deferral: pool=shared`, a.SessionID, a.ID)) {
		t.Fatalf("A's deferral is logged with its reason:\n%s", f.logs)
	}
	if len(f.d.coordinator.slots) != 1 {
		t.Fatal("B holds the reserved slot")
	}

	// A run slot frees while B still holds the reserved slot: A takes the run
	// slot, never the reserved one.
	f.freeRunSlot(t)
	if result := f.dispatch(t); result.Dispatched != 1 {
		t.Fatalf("A dispatched on the free run slot: %+v\n%s", result, f.logs)
	}
	if session := f.awaitStart(t); session != a.SessionID {
		t.Fatalf("started session %d, want A's %d", session, a.SessionID)
	}
	if len(f.d.pool.slots) != cap(f.d.pool.slots) || len(f.d.coordinator.slots) != 1 {
		t.Fatalf("A is on the run pool and B on the reserved slot: pool=%d coordinator=%d", len(f.d.pool.slots), len(f.d.coordinator.slots))
	}
	f.settle(t)
}

// C7: with the reserved slot free and one run slot free, an agent stage takes
// the run slot and leaves the reserved slot alone.
func TestCoordinatorAgentStageTakesARunSlotWhenTheReservedSlotIsFree(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	a := f.coordinator(t, 1, "sync_start_resolve", f.now.Add(-time.Minute))
	f.fillRunPool(t)
	f.freeRunSlot(t)
	if result := f.dispatch(t); result.Dispatched != 1 {
		t.Fatalf("A dispatched: %+v\n%s", result, f.logs)
	}
	if session := f.awaitStart(t); session != a.SessionID {
		t.Fatalf("started session %d, want A's %d", session, a.SessionID)
	}
	if len(f.d.coordinator.slots) != 0 || len(f.d.pool.slots) != cap(f.d.pool.slots) {
		t.Fatalf("A ran on the reserved slot: pool=%d coordinator=%d", len(f.d.pool.slots), len(f.d.coordinator.slots))
	}
	f.settle(t)
}

// C4: with the reserved slot and the run pool both full, a coordinator step is
// deferred with a log line naming the dispatch and the reason.
func TestCoordinatorDeferralForCapacityIsLogged(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	dispatch := f.coordinator(t, 1, "await", f.now.Add(-time.Minute))
	f.fillRunPool(t)
	if !f.d.coordinator.try() {
		t.Fatal("could not take the reserved slot")
	}
	defer f.d.coordinator.done()
	if result := f.dispatch(t); result.Deferred != 1 || result.Dispatched != 0 {
		t.Fatalf("deferred: %+v", result)
	}
	want := fmt.Sprintf(`"level":"INFO","msg":"graph step deferred for capacity","repo":"acme/api","task":"github:acme/api#1","session_id":%d,"graph_dispatch_id":%d,"graph_node":"await","reason":"temporary capacity deferral: pool=shared`, dispatch.SessionID, dispatch.ID)
	if !strings.Contains(f.logs.String(), want) {
		t.Fatalf("missing %s in:\n%s", want, f.logs)
	}
	if got := f.status(t, dispatch); got != "pending" {
		t.Fatalf("deferred dispatch is %s", got)
	}
}

// C8: a worker on the reserved slot does not chain its session's owed agent
// stage; the tick starts it later on the run pool. A run-pool worker chains it.
func TestReservedSlotWorkerLeavesAnOwedAgentStageToTheTick(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	owed := f.coordinator(t, 1, "sync_start_resolve", f.now.Add(-time.Minute))
	session, err := f.db.GetSession(t.Context(), owed.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("reserved slot", func(t *testing.T) {
		task := &task{repo: f.repo, forgeRepo: github.Repo{Owner: "acme", Name: "api"}, session: session, log: f.d.log, workerLog: f.d.log, tickAt: f.now, slot: f.d.coordinator, worker: &workerState{}}
		f.d.continueGraphSteps(t.Context(), task)
		if got := f.status(t, owed); got != "pending" {
			t.Fatalf("the reserved-slot worker took the agent stage: %s", got)
		}
		f.turn.mu.Lock()
		ran := len(f.turn.sessions)
		f.turn.mu.Unlock()
		if ran != 0 {
			t.Fatal("the reserved-slot worker ran a turn")
		}
	})
	t.Run("tick on the run pool", func(t *testing.T) {
		if result := f.dispatch(t); result.Dispatched != 1 {
			t.Fatalf("the tick starts the agent stage: %+v\n%s", result, f.logs)
		}
		if got := f.awaitStart(t); got != owed.SessionID {
			t.Fatalf("started %d", got)
		}
		if len(f.d.coordinator.slots) != 0 || len(f.d.pool.slots) != 1 {
			t.Fatalf("the agent stage is on the run pool: pool=%d coordinator=%d", len(f.d.pool.slots), len(f.d.coordinator.slots))
		}
		f.settle(t)
	})
}

func TestWeightedCoordinatorAdmissionFallsBackAtFullCost(t *testing.T) {
	f := newCoordinatorSlotFixture(t)
	f.d.pool = newPool(3)
	graph := strings.Replace(coordinatorSlotGraph, "await [lf_action=", `await [lf_slot_budget="3",lf_action=`, 1)
	dispatch := f.coordinatorWithGraph(t, 21, "await", f.now, graph)
	reservation, reason, err := f.d.graphStepSlot(t.Context(), dispatch)
	if err != nil || reservation == nil {
		t.Fatalf("full shared admission: %s %v", reason, err)
	}
	defer reservation.Release()
	if reservation.pool != f.d.pool {
		t.Fatal("oversized bookkeeping entered reserved lane")
	}
	if used, total := reservation.Capacity(); used != 3 || total != 3 {
		t.Fatalf("capacity=%d/%d", used, total)
	}
	if used, _ := f.d.coordinator.capacity(); used != 0 {
		t.Fatalf("reserved capacity used=%d", used)
	}
	cheap := f.coordinator(t, 22, "await", f.now)
	another, reason, err := f.d.graphStepSlot(t.Context(), cheap)
	if err != nil || another == nil {
		t.Fatalf("cheap reserved work: %s %v", reason, err)
	}
	defer another.Release()
	if another.pool != f.d.coordinator {
		t.Fatal("cheap bookkeeping did not use its reserved lane")
	}
}
