package brainstorm_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// mixedClock is real time plus an offset the confirmation flow can advance.
type mixedClock struct{ offset atomic.Int64 }

func (c *mixedClock) now() time.Time          { return time.Now().UTC().Add(time.Duration(c.offset.Load())) }
func (c *mixedClock) advance(d time.Duration) { c.offset.Add(int64(d)) }

// overlap records how many jobs execute at once and the highest count seen.
type overlap struct{ active, peak atomic.Int64 }

func (o *overlap) enter() {
	n := o.active.Add(1)
	for {
		old := o.peak.Load()
		if n <= old || o.peak.CompareAndSwap(old, n) {
			return
		}
	}
}
func (o *overlap) leave() { o.active.Add(-1) }

// hold keeps a job running long enough for a concurrent claimer to try to overlap it.
const hold = 50 * time.Millisecond

type mixedAdapter struct {
	o     *overlap
	calls atomic.Int64
}

func (a *mixedAdapter) Resolve(context.Context, owneraction.Principal, durableworkflow.Definition, durableworkflow.Resolved, durableworkflow.Continuation, time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{Summary: "Draft standard", TargetRevision: "one", Payload: `{"safe":"facts"}`}, nil
}
func (a *mixedAdapter) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return nil
}
func (a *mixedAdapter) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (durableworkflow.Output, error) {
	a.calls.Add(1)
	a.o.enter()
	defer a.o.leave()
	time.Sleep(hold)
	return durableworkflow.Output{State: "succeeded", Summary: "workflow done"}, nil
}
func (a *mixedAdapter) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (durableworkflow.Output, bool, error) {
	return durableworkflow.Output{}, false, nil
}

// mixedAdvisory is a fake advisory coordinator that settles its claim after a hold.
type mixedAdvisory struct {
	o        *overlap
	db       *store.DB
	now      func() time.Time
	busy     atomic.Bool
	executes atomic.Int64
}

func (c *mixedAdvisory) Kind() string            { return "advisory" }
func (c *mixedAdvisory) Deadline() time.Duration { return time.Minute }
func (c *mixedAdvisory) Idle() bool              { return !c.busy.Load() }
func (c *mixedAdvisory) ExecuteClaim(ctx context.Context, a store.WorkflowAttempt, _ store.WorkflowJob) error {
	c.executes.Add(1)
	c.busy.Store(true)
	defer c.busy.Store(false)
	c.o.enter()
	defer c.o.leave()
	time.Sleep(hold)
	return c.db.FinishWorkflowAttempt(ctx, a, store.WorkflowFinish{State: "succeeded", Result: `{"answer":"fixture"}`, Questions: `[]`, Summary: "fixture"}, c.now())
}
func (c *mixedAdvisory) RecoverClaim(ctx context.Context, a store.WorkflowAttempt) error {
	return c.db.ReconcileWorkflowAttempt(ctx, a, store.WorkflowFinish{State: "uncertain", Result: `{}`, Questions: `[]`, Summary: "uncertain"}, c.now())
}

// mixedRunner is the fake read-only scan runner; it never holds a receipt.
type mixedRunner struct {
	o        *overlap
	runs     atomic.Int64
	recovers atomic.Int64
}

func (r *mixedRunner) Run(_ context.Context, req brainstorm.RunRequest) (brainstorm.RunResult, error) {
	r.runs.Add(1)
	if r.o != nil {
		r.o.enter()
		defer r.o.leave()
		time.Sleep(hold)
	}
	return brainstorm.RunResult{Answer: "Retries happen twice, then the job stops.", Research: json.RawMessage(`{"request":` + strconv.Quote(req.Question) + `,"findings":[{"id":"F1","text":"two retries"}]}`), Commit: "main"}, nil
}
func (r *mixedRunner) Recover(context.Context, brainstorm.RunRequest) (brainstorm.RunResult, bool, error) {
	r.recovers.Add(1)
	return brainstorm.RunResult{}, false, nil
}

type mixedTeam struct{}

func (mixedTeam) Capture(context.Context, string) (brainstorm.TeamCapture, error) {
	return brainstorm.TeamCapture{Version: "fixture-1", Role: "research role fixture", Files: map[string]string{"skills/brainstorm/SKILL.md": "fixture"}}, nil
}

type mixedFixture struct {
	db        *store.DB
	clock     *mixedClock
	o         *overlap
	adapter   *mixedAdapter
	workflows *durableworkflow.Service
	actions   *owneraction.Service
	owner     owneraction.Principal
	reader    brainstorm.Principal
	admitted  int
}

func newMixedFixture(t *testing.T) *mixedFixture {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	f := &mixedFixture{db: db, clock: &mixedClock{}, o: &overlap{}}
	f.adapter = &mixedAdapter{o: f.o}
	f.owner = owneraction.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", DiscordOwnerID: "owner-discord", ChannelID: "channel"}
	f.reader = brainstorm.Principal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client-1", GrantID: "grant-1"}
	f.workflows = f.newWorkflows(t)
	return f
}

// newWorkflows builds a shared durable worker over the fixture database, as a
// daemon start or restart does.
func (f *mixedFixture) newWorkflows(t *testing.T) *durableworkflow.Service {
	t.Helper()
	registry, err := durableworkflow.NewRegistry(durableworkflow.BuiltinDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	s, err := durableworkflow.New(durableworkflow.Options{DB: f.db, Registry: registry, Adapters: map[string]durableworkflow.Adapter{"standards-authoring": f.adapter}, Now: f.clock.now, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.actions = owneraction.New(f.db, s, f.clock.now)
	f.actions.SetDiscord("owner-discord", []string{"channel"})
	s.BindActions(f.actions)
	return s
}

func (f *mixedFixture) newBrainstorm(t *testing.T, runner brainstorm.Runner) *brainstorm.Service {
	t.Helper()
	s, err := brainstorm.New(brainstorm.Options{DB: f.db, Now: f.clock.now, Team: mixedTeam{}, Runner: runner, Deadline: time.Minute, Protect: func(_ context.Context, s string) (string, error) { return s, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// queueWorkflow proposes and confirms one owner workflow so it waits as a queued job.
func (f *mixedFixture) queueWorkflow(t *testing.T, key string) string {
	t.Helper()
	proposal, err := f.workflows.Propose(t.Context(), f.owner, "standards-authoring", "1", "", map[string]any{"intent": "draft testing"}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.actions.Available(t.Context(), proposal.Proposal.ID, "message"); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(5 * time.Second)
	confirmed, err := f.actions.Confirm(t.Context(), f.owner, proposal.Proposal.ID)
	if err != nil || confirmed.State != "admitted" {
		t.Fatalf("confirm %+v %v", confirmed, err)
	}
	if _, err = f.actions.Step(t.Context(), proposal.Proposal.ID); err != nil {
		t.Fatal(err)
	}
	return proposal.Job
}

// queueAdvisory admits one advisory job on its own task conversation.
func (f *mixedFixture) queueAdvisory(t *testing.T) string {
	t.Helper()
	f.admitted++
	n := strconv.Itoa(f.admitted)
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: f.owner.OwnerGeneration, RepositoryRef: "github:acme/api", TaskRef: "github:acme/api#" + n}
	c, err := f.db.OpenDiscussion(t.Context(), store.DiscussionConversation{DiscussionScope: scope, ID: "conversation-" + n, ClientID: "client", RequestKey: "open-" + n, CreatedAt: f.clock.now()})
	if err != nil {
		t.Fatal(err)
	}
	a := store.AdvisoryAdmission{DiscussionScope: scope, ID: "admission-" + n, ConversationID: c.ID, Operation: "ask", RequestKey: "ask-" + n, Digest: "digest-" + n, Basis: `{"stage":"research"}`, Context: `[]`, Question: "Why?", GrantID: "grant", ConsentRevision: "1", JobID: "advisory-job-" + n, ExecutionID: "advisory-execution-" + n, CreatedAt: f.clock.now()}
	if a, err = f.db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	return a.JobID
}

// openBrainstorm opens a brainstorm for the repository with one idea in it.
func (f *mixedFixture) openBrainstorm(t *testing.T, s *brainstorm.Service, repository string) string {
	t.Helper()
	v, err := s.Open(t.Context(), f.reader, repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Add(t.Context(), f.reader, brainstorm.AddRequest{BrainstormID: v.Brainstorm.ID, Kind: brainstorm.KindIdea, Text: "retry failed imports", RequestKey: "idea-1", ExpectedRevision: v.Brainstorm.Revision}); err != nil {
		t.Fatal(err)
	}
	return v.Brainstorm.ID
}

func ask(t *testing.T, s *brainstorm.Service, p brainstorm.Principal, id, key string) brainstorm.ScanView {
	t.Helper()
	v, err := s.Ask(t.Context(), p, brainstorm.AskRequest{BrainstormID: id, Question: "How do retries work? (" + key + ")", RequestKey: key})
	if err != nil {
		t.Fatalf("ask %s: %v", key, err)
	}
	return v
}

func scanState(t *testing.T, s *brainstorm.Service, p brainstorm.Principal, scanID string) brainstorm.ScanItem {
	t.Helper()
	v, err := s.ScanGet(t.Context(), p, scanID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	return v.Scan
}

func jobState(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	j, err := db.ReadWorkflowJob(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j.State
}

func TestMixedJobsRunOneAtATime(t *testing.T) {
	f := newMixedFixture(t)
	advisory := &mixedAdvisory{o: f.o, db: f.db, now: f.clock.now}
	if err := f.workflows.BindAdvisory(advisory); err != nil {
		t.Fatal(err)
	}
	runner := &mixedRunner{o: f.o}
	scans := f.newBrainstorm(t, runner)
	if err := f.workflows.Register(scans); err != nil {
		t.Fatal(err)
	}
	id := f.openBrainstorm(t, scans, "github:acme/api")

	workflowJob := f.queueWorkflow(t, "mixed-1")
	advisoryJob := f.queueAdvisory(t)
	scan := ask(t, scans, f.reader, id, "q-1")
	if scan.Scan.State != brainstorm.ScanWaiting {
		t.Fatalf("scan admitted as %+v", scan.Scan)
	}
	advisoryJob2 := f.queueAdvisory(t)
	scan2 := ask(t, scans, f.reader, id, "q-2")

	// Several claimers race the one shared worker until every job is finished.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	finished := func() bool {
		for _, j := range []string{workflowJob, advisoryJob, advisoryJob2, scan.Scan.ScanID, scan2.Scan.ScanID} {
			var state string
			if j == scan.Scan.ScanID || j == scan2.Scan.ScanID {
				state = scanState(t, scans, f.reader, j).State
				if state == brainstorm.ScanAnswered {
					continue
				}
			} else if state = jobState(t, f.db, j); state == "succeeded" {
				continue
			}
			return false
		}
		return true
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				if _, err := f.workflows.RunOnce(ctx); err != nil && ctx.Err() == nil {
					errs <- err
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	for !finished() {
		if ctx.Err() != nil {
			t.Fatalf("jobs did not finish: workflow=%s advisory=%s,%s scans=%+v,%+v", jobState(t, f.db, workflowJob), jobState(t, f.db, advisoryJob), jobState(t, f.db, advisoryJob2), scanState(t, scans, f.reader, scan.Scan.ScanID), scanState(t, scans, f.reader, scan2.Scan.ScanID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if f.o.peak.Load() != 1 {
		t.Fatalf("jobs overlapped: peak concurrency %d", f.o.peak.Load())
	}
	if f.adapter.calls.Load() != 1 || advisory.executes.Load() != 2 || runner.runs.Load() != 2 {
		t.Fatalf("executions workflow=%d advisory=%d scans=%d", f.adapter.calls.Load(), advisory.executes.Load(), runner.runs.Load())
	}
}

func TestMixedUnknownKindIsNeverExecuted(t *testing.T) {
	f := newMixedFixture(t)
	// This worker has no brainstorm_scan coordinator installed.
	advisory := &mixedAdvisory{o: f.o, db: f.db, now: f.clock.now}
	if err := f.workflows.BindAdvisory(advisory); err != nil {
		t.Fatal(err)
	}
	runner := &mixedRunner{}
	scans := f.newBrainstorm(t, runner)
	id := f.openBrainstorm(t, scans, "github:acme/api")
	scan := ask(t, scans, f.reader, id, "q-1")

	ran, err := f.workflows.RunOnce(t.Context())
	if err != nil || !ran {
		t.Fatalf("RunOnce = %v %v", ran, err)
	}
	if runner.runs.Load() != 0 || advisory.executes.Load() != 0 {
		t.Fatalf("unknown kind executed: scans=%d advisory=%d", runner.runs.Load(), advisory.executes.Load())
	}
	if item := scanState(t, scans, f.reader, scan.Scan.ScanID); item.State != brainstorm.ScanFailed {
		t.Fatalf("unknown-kind scan = %+v", item)
	}
	if _, err = f.db.RunningWorkflowAttempt(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("refused claim kept the shared slot: %v", err)
	}
	// A later worker that knows the kind still never reruns the refused scan.
	later := f.newWorkflows(t)
	if err = later.Register(scans); err != nil {
		t.Fatal(err)
	}
	if ran, err = later.RunOnce(t.Context()); err != nil || ran || runner.runs.Load() != 0 {
		t.Fatalf("refused scan rerun: ran=%v err=%v runs=%d", ran, err, runner.runs.Load())
	}
}

func TestMixedPerBrainstormQueueLimit(t *testing.T) {
	f := newMixedFixture(t)
	runner := &mixedRunner{}
	scans := f.newBrainstorm(t, runner)
	if err := f.workflows.Register(scans); err != nil {
		t.Fatal(err)
	}
	id := f.openBrainstorm(t, scans, "github:acme/api")
	var first brainstorm.ScanView
	for i := 1; i <= brainstorm.MaxQueuedScans; i++ {
		v := ask(t, scans, f.reader, id, "q-"+strconv.Itoa(i))
		if v.Scan.State != brainstorm.ScanWaiting {
			t.Fatalf("ask %d = %+v", i, v.Scan)
		}
		if i == 1 {
			first = v
		}
	}
	if _, err := scans.Ask(t.Context(), f.reader, brainstorm.AskRequest{BrainstormID: id, Question: "One more?", RequestKey: "q-6"}); !errors.Is(err, brainstorm.ErrQueueFull) {
		t.Fatalf("sixth ask while five wait = %v", err)
	}
	// A retry of an admitted question is a replay, not a new admission.
	if v, err := scans.Ask(t.Context(), f.reader, brainstorm.AskRequest{BrainstormID: id, Question: "How do retries work? (q-1)", RequestKey: "q-1"}); err != nil || !v.Replayed || v.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("replay while full = %+v %v", v, err)
	}
	// The limit is per brainstorm: another repository's brainstorm still admits.
	other := f.openBrainstorm(t, scans, "github:acme/web")
	ask(t, scans, f.reader, other, "other-1")

	// A cancelled waiting question frees its place and is never run.
	if v, err := scans.ScanCancel(t.Context(), f.reader, first.Scan.ScanID); err != nil || v.Scan.State != brainstorm.ScanCancelled {
		t.Fatalf("cancel waiting = %+v %v", v, err)
	}
	sixth := ask(t, scans, f.reader, id, "q-6")
	if _, err := scans.Ask(t.Context(), f.reader, brainstorm.AskRequest{BrainstormID: id, Question: "Seventh?", RequestKey: "q-7"}); !errors.Is(err, brainstorm.ErrQueueFull) {
		t.Fatalf("ask after refill = %v", err)
	}
	// Once one question is answered, another may wait.
	if ran, err := f.workflows.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("RunOnce = %v %v", ran, err)
	}
	ask(t, scans, f.reader, id, "q-7")
	if item := scanState(t, scans, f.reader, first.Scan.ScanID); item.State != brainstorm.ScanCancelled {
		t.Fatalf("cancelled scan = %+v", item)
	}
	if item := scanState(t, scans, f.reader, sixth.Scan.ScanID); item.State != brainstorm.ScanWaiting {
		t.Fatalf("sixth scan = %+v", item)
	}
}

func TestMixedRestartWithRunningScanRecoversUnsureWithoutRerun(t *testing.T) {
	f := newMixedFixture(t)
	runner := &mixedRunner{}
	scans := f.newBrainstorm(t, runner)
	if err := f.workflows.Register(scans); err != nil {
		t.Fatal(err)
	}
	id := f.openBrainstorm(t, scans, "github:acme/api")
	scan := ask(t, scans, f.reader, id, "q-1")
	// The daemon claimed the scan and stopped before any answer was kept.
	if _, _, err := f.db.ClaimWorkflowAttempt(t.Context(), f.clock.now(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if item := scanState(t, scans, f.reader, scan.Scan.ScanID); item.State != brainstorm.ScanRunning {
		t.Fatalf("claimed scan = %+v", item)
	}

	// Restart: a new worker and coordinator over the same database.
	restartRunner := &mixedRunner{}
	restarted := f.newBrainstorm(t, restartRunner)
	workflows := f.newWorkflows(t)
	if err := workflows.Register(restarted); err != nil {
		t.Fatal(err)
	}
	if err := workflows.RecoverRunning(t.Context()); err != nil {
		t.Fatal(err)
	}
	item := scanState(t, restarted, f.reader, scan.Scan.ScanID)
	if item.State != brainstorm.ScanUnsure || item.Reason == "" {
		t.Fatalf("recovered scan = %+v", item)
	}
	if restartRunner.recovers.Load() != 1 || restartRunner.runs.Load() != 0 || runner.runs.Load() != 0 {
		t.Fatalf("recovery recovers=%d runs=%d old runs=%d", restartRunner.recovers.Load(), restartRunner.runs.Load(), runner.runs.Load())
	}
	if ran, err := workflows.RunOnce(t.Context()); err != nil || ran {
		t.Fatalf("unsure scan was claimed again: ran=%v err=%v", ran, err)
	}
	if restartRunner.runs.Load() != 0 {
		t.Fatal("unsure scan was rerun")
	}
	page, err := restarted.Scans(t.Context(), f.reader, id, "", 0)
	if err != nil || len(page.Scans) != 1 || page.Scans[0].State != brainstorm.ScanUnsure {
		t.Fatalf("listed scans = %+v %v", page, err)
	}
}
