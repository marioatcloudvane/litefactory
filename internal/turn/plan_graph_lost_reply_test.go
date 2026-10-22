package turn

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// lostReplyPlan is one graph Plan attempt served by the real shared result
// service behind the real private broker. Only the reply is injected: the service,
// the store, the broker and the client journal are the production ones.
type lostReplyPlan struct {
	t       *testing.T
	db      *store.DB
	session store.Session
	opts    AgentResultServiceOptions
	service *AgentResultService
	broker  *submission.Broker

	mu      sync.Mutex
	lose    string // "", "after" (service processes, reply is lost) or "before" (request never reaches the service)
	handled []string
}

func graphPlanResolver(story, candidate, requestID string) func(context.Context) (ar.Context, error) {
	binding := &ar.PlanGraphBinding{Story: story, Stage: "story", Candidate: candidate, RequestID: requestID, Upstream: []string{}, SharedRevision: "initial"}
	return func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, PlanGraph: binding.Clone()}, nil
	}
}

func newLostReplyPlan(t *testing.T) *lostReplyPlan {
	t.Helper()
	db, session, o := agentResultFixture(t, "plan")
	o.Resolve = graphPlanResolver("story-001", "plan-1-1", "plan-request-1-1")
	o.ReadTargets, o.EditTargets = planGraphResultTargets(), planGraphResultTargets()
	service, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	l := &lostReplyPlan{t: t, db: db, session: session, opts: o, service: service}
	l.broker = lostReplyBroker(t, func(ctx context.Context, q submission.Request) (json.RawMessage, error) {
		l.mu.Lock()
		lose := ""
		if q.Method == "submit" {
			lose = l.lose
			l.lose = ""
			l.handled = append(l.handled, q.RequestID)
		}
		l.mu.Unlock()
		if lose == "before" {
			return nil, errors.New("request lost before the service saw it")
		}
		// The shared adapter marks its calls restricted, so a graph story request
		// or set must be accepted and replayed under that restriction too.
		out, err := service.handleResult(context.WithValue(ctx, mcpCommunicationKey{}, true), q)
		if lose == "after" && err == nil {
			return nil, errors.New("reply lost after the service recorded the request")
		}
		return out, err
	})
	return l
}

func lostReplyBroker(t *testing.T, handler submission.Handler) *submission.Broker {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "lf-lost-")
	mustProgress(t, err)
	dir, err = filepath.EvalSymlinks(dir)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, os.RemoveAll(dir)) })
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowResult: true, Handler: handler, MaxWorkers: 2, RequestTimeout: time.Minute})
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, broker.Close()) })
	return broker
}

// call is one helper invocation from a client that keeps nothing in memory: its
// only state is the journal under the attempt directory, which is what a restarted
// client has too.
func (l *lostReplyPlan) call(method string, data []byte) submission.Response {
	l.t.Helper()
	r, err := submission.Call(l.t.Context(), l.broker.DescriptorPath(), method, data)
	mustProgress(l.t, err)
	return r
}

func (l *lostReplyPlan) revisions() []int64 {
	l.t.Helper()
	revisions, err := l.db.ResultRevisionNumbers(l.t.Context(), l.service.Binding())
	mustProgress(l.t, err)
	return revisions
}

func decodeReceipt(t *testing.T, r submission.Response) AgentResultSubmission {
	t.Helper()
	if r.Error != "" {
		t.Fatalf("broker error: %s", r.Error)
	}
	var got AgentResultSubmission
	mustProgress(t, json.Unmarshal(r.Result, &got))
	return got
}

// A deliberately awkward layout: whitespace and key order are part of the bytes a
// retry must preserve.
const lostReplyStoryRequest = "{ \"result\" : {\"outcome\":\"completed\",\n \"comment\":\"Proposed.\",\"payload\":{\"plan\":{\"storyrequest\":{\"operation\":\"create\",\"draft\":" + graphBindingDraft + "}}}} }"

func TestGraphPlanLostReply_RecoversAcceptedCandidateBeforeRetry(t *testing.T) {
	l := newLostReplyPlan(t)
	data := []byte(lostReplyStoryRequest)

	l.mu.Lock()
	l.lose = "after"
	l.mu.Unlock()
	first := l.call("submit", data)
	if first.Error == "" || first.RequestID == "" || len(first.Result) != 0 {
		t.Fatalf("reply was not lost: %+v", first)
	}
	id := first.RequestID
	if got := l.revisions(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("the recorded request was not retained once: %v", got)
	}

	// The same identity finds the accepted candidate without submitting anything.
	recovered := decodeReceipt(t, l.call("receipt", []byte(`{"request_id":"`+id+`"}`)))
	if recovered.Receipt == nil || recovered.Receipt.RequestID != id || recovered.Receipt.AppliedRevision != 1 || recovered.Receipt.InputState != "accepted" {
		t.Fatalf("receipt did not recover the accepted candidate: %+v", recovered.Receipt)
	}
	if string(recovered.Receipt.Payload) != string(data) {
		t.Fatalf("recovered request lost the exact submitted bytes: %q", recovered.Receipt.Payload)
	}
	// Recovery reports only what is established: input was accepted, and nothing
	// downstream (engineering, acceptance, publication) was observed.
	if latest := recovered.Receipt.Latest; latest.Input != "accepted" || latest.Engineering != "pending" || latest.Acceptance != "pending" || latest.Publication != "pending" {
		t.Fatalf("receipt claimed more than input acceptance: %+v", latest)
	}
	q := recovered.Snapshot.Result.Payload.Plan.StoryRequest
	if q == nil || q.Candidate != "plan-1-1" || q.RequestID != "plan-request-1-1" || q.Stage != "story" {
		t.Fatalf("recovered candidate is not the factory-bound one: %+v", q)
	}

	// A restarted client resends the identical bytes: same identity, one result.
	retry := l.call("submit", data)
	if retry.RequestID != id {
		t.Fatalf("retry changed the request identity: %q vs %q", retry.RequestID, id)
	}
	replayed := decodeReceipt(t, retry)
	if !reflect.DeepEqual(recovered, replayed) {
		t.Fatalf("retry did not replay the recorded outcome:\n%+v\n%+v", recovered, replayed)
	}
	if got := l.revisions(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("retry created another result: %v", got)
	}

	// The terminal acceptance reads exactly that revision and nothing else.
	snapshot, revision, err := l.service.retainedCurrent(t.Context())
	mustProgress(t, err)
	if revision != 1 || !ar.SameResult(snapshot.Result, recovered.Snapshot.Result) {
		t.Fatalf("terminal acceptance did not read the recovered revision: %d", revision)
	}
	node, accepted, err := AgentActivity{Policy: PlanWorkflowPolicy()}.acceptRetained(t.Context(), l.service)
	mustProgress(t, err)
	if accepted != 1 || node.Response != "done" {
		t.Fatalf("recovered result was not accepted from the store: %d %+v", accepted, node)
	}

	// Different bytes under a recorded identity are a correlation fault, never a
	// second result.
	if _, err := l.service.Submit(t.Context(), id, []byte(strings.Replace(lostReplyStoryRequest, "Proposed.", "Changed.", 1))); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatalf("changed bytes under a recorded identity: %v", err)
	}
	if got := l.revisions(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("correlation fault retained a result: %v", got)
	}
}

func TestGraphPlanLostReply_RecoversAcrossDaemonRestart(t *testing.T) {
	l := newLostReplyPlan(t)
	data := []byte(lostReplyStoryRequest)
	l.mu.Lock()
	l.lose = "after"
	l.mu.Unlock()
	id := l.call("submit", data).RequestID
	if id == "" {
		t.Fatal("no correlation kept")
	}
	binding := l.service.Binding()

	// Everything in memory is gone: reopen the store, reconnect a reader, and bind
	// the next attempt of the same graph instance.
	reopened, err := store.Open(t.Context(), l.db.Path())
	mustProgress(t, err)
	defer func() { mustProgress(t, reopened.Close()) }()

	reader, err := OpenAgentResultReader(t.Context(), reopened, binding, planGraphResultTargets())
	mustProgress(t, err)
	got, err := reader.Receipt(t.Context(), binding.RunID, id)
	mustProgress(t, err)
	if got.Receipt == nil || got.Receipt.AppliedRevision != 1 || got.Receipt.InputState != "accepted" {
		t.Fatalf("reader lost the recorded outcome: %+v", got.Receipt)
	}
	replay, err := reader.Submit(t.Context(), id, data)
	mustProgress(t, err)
	if !reflect.DeepEqual(got, replay) {
		t.Fatal("identical bytes did not replay the recorded outcome after restart")
	}
	if _, err := reader.Submit(t.Context(), "new-identity", data); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("a recovering reader gained write authority: %v", err)
	}

	o := l.opts
	o.DB = reopened
	o.Attempt = agentResultRun(t, reopened, l.session, "plan", "restart", 2)
	o.Resolve = graphPlanResolver("story-001", "plan-1-2", "plan-request-1-2")
	next, err := BindAgentResultService(t.Context(), o)
	mustProgress(t, err)
	recovered, err := next.Receipt(t.Context(), binding.RunID, id)
	mustProgress(t, err)
	if recovered.Receipt == nil || recovered.Receipt.AppliedRevision != 1 || recovered.Receipt.InputState != "accepted" {
		t.Fatalf("next attempt could not recover the earlier receipt: %+v", recovered.Receipt)
	}
	// The earlier attempt's accepted candidate is a baseline to update. It is never
	// the new attempt's own result, so a terminal answer cannot claim success on it.
	if _, _, err := next.retainedCurrent(t.Context()); !errors.Is(err, errNoRetainedResult) {
		t.Fatalf("an earlier attempt's result stood in for this attempt's: %v", err)
	}
	// Another attempt never replays a request it did not make. The same create bytes
	// meet the existing revision and are refused with the baseline to update.
	foreign, err := next.Submit(t.Context(), id, data)
	mustProgress(t, err)
	if foreign.Validation.Valid || foreign.Snapshot != nil || foreign.Validation.Baseline == nil || foreign.Validation.Baseline.Revision != 1 {
		t.Fatalf("another attempt replayed a foreign request: %+v", foreign)
	}
	numbers, err := reopened.ResultRevisionNumbers(t.Context(), next.Binding())
	mustProgress(t, err)
	if !slices.Equal(numbers, []int64{1}) {
		t.Fatalf("recovery created a result: %v", numbers)
	}
}

func TestGraphPlanLostReply_UncertainRecoveryStaysUncertain(t *testing.T) {
	l := newLostReplyPlan(t)
	data := []byte(lostReplyStoryRequest)

	l.mu.Lock()
	l.lose = "before"
	l.mu.Unlock()
	first := l.call("submit", data)
	if first.Error == "" || first.RequestID == "" {
		t.Fatalf("expected an unanswered request: %+v", first)
	}
	id := first.RequestID

	// Nothing was recorded, so recovery finds nothing and says so: no receipt, no
	// result, and no success claimed.
	lookup := l.call("receipt", []byte(`{"request_id":"`+id+`"}`))
	if got := decodeReceipt(t, lookup); got.Validation.Valid || got.Receipt != nil || got.Snapshot != nil || len(got.Validation.Diagnostics.Diagnostics) != 1 || got.Validation.Diagnostics.Diagnostics[0].Code != "not_recorded" {
		t.Fatalf("recovery claimed an outcome that was never recorded: %+v", lookup)
	}
	if got := l.revisions(); len(got) != 0 {
		t.Fatalf("uncertainty created a result: %v", got)
	}
	if _, _, err := l.service.retainedCurrent(t.Context()); !errors.Is(err, errNoRetainedResult) {
		t.Fatalf("terminal acceptance found a result that was never accepted: %v", err)
	}
	if _, _, err := (AgentActivity{Policy: PlanWorkflowPolicy()}).acceptRetained(t.Context(), l.service); err == nil {
		t.Fatal("a terminal envelope stood in for an unrecovered result")
	}

	// Retrying the identical bytes keeps the identity and yields exactly one result.
	retry := l.call("submit", data)
	if retry.RequestID != id {
		t.Fatalf("retry changed the request identity: %q vs %q", retry.RequestID, id)
	}
	if got := decodeReceipt(t, retry); got.Receipt == nil || got.Receipt.AppliedRevision != 1 {
		t.Fatalf("retry was not accepted: %+v", got)
	}
	if got := l.revisions(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("retry left %v", got)
	}
	if again := l.call("submit", data); again.RequestID != id || !slices.Equal(l.revisions(), []int64{1}) {
		t.Fatalf("second retry was not a replay: %+v %v", again, l.revisions())
	}
}

func TestGraphPlanLostReply_CorrectedRejectionUsesNewIdentity(t *testing.T) {
	l := newLostReplyPlan(t)
	// The agent supplied a factory-owned member, so this payload is refused.
	rejected := []byte(strings.Replace(lostReplyStoryRequest, `"operation":"create",`, `"operation":"create","stage":"technical",`, 1))

	first := decodeReceipt(t, l.call("submit", rejected))
	if first.Validation.Valid || first.Receipt == nil || first.Receipt.InputState != "rejected" || first.Snapshot != nil {
		t.Fatalf("a factory-owned member was accepted: %+v", first)
	}
	rejectedID := first.Receipt.RequestID
	if got := l.revisions(); len(got) != 0 {
		t.Fatalf("a refused payload was retained: %v", got)
	}
	// Resending the refused bytes is the same request with the same recorded refusal.
	if again := l.call("submit", rejected); again.RequestID != rejectedID {
		t.Fatalf("refused bytes changed identity: %q vs %q", again.RequestID, rejectedID)
	}

	corrected := []byte(lostReplyStoryRequest)
	fixed := l.call("submit", corrected)
	if fixed.RequestID == "" || fixed.RequestID == rejectedID {
		t.Fatalf("corrected content reused the refused identity: %q", fixed.RequestID)
	}
	accepted := decodeReceipt(t, fixed)
	if !accepted.Validation.Valid || accepted.Receipt == nil || accepted.Receipt.AppliedRevision != 1 || accepted.Receipt.InputState != "accepted" {
		t.Fatalf("corrected content was not accepted: %+v", accepted)
	}
	if got := l.revisions(); !slices.Equal(got, []int64{1}) {
		t.Fatalf("correction left %v", got)
	}
	// The refusal stays recoverable under its own identity and is still a refusal.
	old := decodeReceipt(t, l.call("receipt", []byte(`{"request_id":"`+rejectedID+`"}`)))
	if old.Receipt == nil || old.Receipt.InputState != "rejected" || old.Receipt.AppliedRevision != 0 {
		t.Fatalf("refusal record changed: %+v", old.Receipt)
	}
}

func TestPlanNodeGuidance_TellsAgentToRecoverBeforeRetry(t *testing.T) {
	for _, want := range []string{"request_id", "look up", "before retrying", "identical bytes", "corrected", "new request identity"} {
		if !strings.Contains(planNodeGuidance, want) {
			t.Errorf("graph Plan guidance lacks %q", want)
		}
	}
}

// deferringPublisher refuses publication with the discovery-budget deferral the
// incident saw, before anything is marked sent, while on is set.
type deferringPublisher struct {
	*github.Client
	on *atomic.Bool
}

func (p deferringPublisher) BindPublication(context.Context, string, int64, naming.Ref, int64) error {
	if p.on.Load() {
		return fmt.Errorf("graph step issue observation failed: %w", ticket.ErrDiscoveryDeferred)
	}
	return nil
}

// parkedPlanStory is a Plan story whose technical_reply publication was
// deferred: its instance is running, parked on that final comment node with a
// scheduled retry that, in these tests, never runs.
type parkedPlanStory struct {
	f      *fixture
	forge  *fakeForge
	base   Request
	now    time.Time
	parked store.GraphInstance
}

func newParkedPlanStory(t *testing.T) *parkedPlanStory {
	t.Helper()
	f, _, base, _, forge := graphPlanFixtureWithForge(t, "agent")
	p := &parkedPlanStory{f: f, forge: forge, base: base, now: time.Now().UTC()}
	f.opts.MCP.Now = func() time.Time { return p.now }
	on := &atomic.Bool{}
	reporter, err := NewReporter(ReporterOptions{BotUserID: 77, DB: f.db, Forge: deferringPublisher{forge.client, on}, Renderer: f.opts.ResearchReporter.renderer, Logger: f.opts.Logger})
	mustProgress(t, err)
	f.opts.ResearchReporter = reporter
	f.opts.GraphRetryDelay = time.Minute
	executeGraphPlan(t, f, base)
	executeGraphPlan(t, f, graphPlanApprove(t, f, base, base.Trigger.CommentID+1))
	question := "Explain the technical stage"
	discussionRunner(t, f, "technical", question)
	on.Store(true)
	executeGraphPlan(t, f, p.answer(t, 9951, question))
	// The forge works again, but nothing drives the parked instance.
	on.Store(false)
	p.parked = p.latest(t)
	if p.parked.Status != store.GraphRunning || p.parked.CurrentNode != "technical_reply" || p.parked.NextDueAt.IsZero() {
		t.Fatalf("instance is not parked on technical_reply with a retry: %+v", p.parked)
	}
	return p
}

func (p *parkedPlanStory) answer(t *testing.T, id int64, body string) Request {
	t.Helper()
	req := asStoryTask(t, p.f, nextTrigger(p.base, id, body), "story-001")
	binding, err := p.f.db.GetPlanStoryDraft(t.Context(), p.f.ref().String(), "story-001")
	mustProgress(t, err)
	req.Trigger.CreatedAt = p.now
	req.Trigger.PlanReply = &planbrief.PlanOwnerReply{ID: id, OwnerID: 7, ActorID: 7, IssueID: binding.IssueID, Key: "story-001", Body: body}
	return req
}

func (p *parkedPlanStory) latest(t *testing.T) store.GraphInstance {
	t.Helper()
	session := p.answer(t, 1, "x").SessionID
	instance, err := p.f.db.LatestWorkflowInstance(t.Context(), session, "plan")
	mustProgress(t, err)
	return instance
}

func (p *parkedPlanStory) attempts(t *testing.T, instance int64) int {
	t.Helper()
	attempts, err := p.f.db.ListGraphNodeAttempts(t.Context(), instance)
	mustProgress(t, err)
	return len(attempts)
}

// bindFinished reproduces the defect's state without the prevention: the
// comment's launch is bound to the parked instance, and replaying it finishes
// that instance at 'waiting' without any agent pass.
func (p *parkedPlanStory) bindFinished(t *testing.T, req Request) {
	t.Helper()
	capture, err := p.f.db.WorkflowCaptureForAdmission(t.Context(), req.SessionID, req.Trigger.CommentID, "plan")
	mustProgress(t, err)
	_, err = p.f.db.AdmitWorkflowLaunch(t.Context(), store.WorkflowLaunch{Mode: "plan", SessionID: req.SessionID, TriggerID: req.Trigger.CommentID, DefinitionID: p.parked.DefinitionID, Start: "select_set", ContentDigest: capture.ContentDigest, TopologyDigest: capture.TopologyDigest, Files: capture.Files, Provenance: capture.Provenance, Reply: req.Trigger.Body})
	mustProgress(t, err)
	calls := discussionRunner(t, p.f, "technical", req.Trigger.Body)
	executeGraphPlan(t, p.f, req)
	if finished := p.latest(t); finished.ID != p.parked.ID || finished.Status != store.GraphSucceeded || finished.CurrentNode != "waiting" || *calls != 0 {
		t.Fatalf("bound replay did not finish the parked instance without an agent: %+v calls=%d", finished, *calls)
	}
}

func (p *parkedPlanStory) probe(t *testing.T, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", store.DSN(p.f.db.Path()))
	mustProgress(t, err)
	defer func() { mustProgress(t, db.Close()) }()
	_, err = db.ExecContext(t.Context(), query, args...)
	mustProgress(t, err)
}

// C10: an owner answer that arrives while the latest instance is parked at a
// non-agent step is held for a bounded time, never bound to that instance, and
// then starts a successor whose agent pass receives it.
func TestPlanParkedWithoutAgent_HoldIsBoundedAndSuccessorReceivesReply(t *testing.T) {
	p := newParkedPlanStory(t)
	reply := "1A, 2A"
	req := p.answer(t, 9960, reply)
	calls := discussionRunner(t, p.f, "technical", reply)

	// (a) Within the bound the answer waits and nothing is bound or changed.
	_, err := Execute(t.Context(), p.f.opts, req)
	if !errors.Is(err, errPlanStepFinishing) {
		t.Fatalf("answer was not held behind the parked step: %v", err)
	}
	if _, found, e := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID); e != nil || found {
		t.Fatalf("held answer has a launch row: found=%v err=%v", found, e)
	}
	if still := p.latest(t); still.ID != p.parked.ID || still.Status != store.GraphRunning || *calls != 0 {
		t.Fatalf("hold changed the parked instance: %+v calls=%d", still, *calls)
	}

	// (b) Past the bound, with the instance never driven in between, the parked
	// instance is cancelled and a successor's agent receives the answer.
	p.now = p.now.Add(planParkedHold + time.Minute)
	executeGraphPlan(t, p.f, req)
	cancelled, err := p.f.db.GetGraphInstance(t.Context(), p.parked.ID)
	mustProgress(t, err)
	if cancelled.Status != store.GraphCancelled || cancelled.CurrentNode != "technical_reply" {
		t.Fatalf("parked instance was not released: %+v", cancelled)
	}
	bound, found, err := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || bound == p.parked.ID {
		t.Fatalf("answer was bound to the parked instance: found=%v instance=%d", found, bound)
	}
	predecessor, _, err := p.f.db.WorkflowPredecessor(t.Context(), bound)
	mustProgress(t, err)
	if predecessor != p.parked.ID || *calls != 1 {
		t.Fatalf("successor did not run an agent pass with the answer: predecessor=%d calls=%d", predecessor, *calls)
	}
}

// C10 (c): without a scheduled retry the parked instance cannot move on its own,
// so the first attempt does not hold.
func TestPlanParkedWithoutAgent_NoScheduledRetryDoesNotHold(t *testing.T) {
	p := newParkedPlanStory(t)
	p.probe(t, `UPDATE graph_instances SET next_due_at=NULL WHERE id=?`, p.parked.ID)
	reply := "1A, 2A"
	req := p.answer(t, 9961, reply)
	calls := discussionRunner(t, p.f, "technical", reply)
	executeGraphPlan(t, p.f, req)
	bound, found, err := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || bound == p.parked.ID || *calls != 1 {
		t.Fatalf("answer was held or bound to the parked instance: instance=%d calls=%d", bound, *calls)
	}
}

// C1, C2: a story reply whose launch is bound to a finished instance is refused
// visibly instead of retried; a later comment launches a successor.
func TestPlanFinishedLaunch_StoryReplyRetiredThenSuccessor(t *testing.T) {
	p := newParkedPlanStory(t)
	req := p.answer(t, 9970, "1A, 2A")
	p.bindFinished(t, req)
	attempts := p.attempts(t, p.parked.ID)
	_, err := Execute(t.Context(), p.f.opts, req)
	var refusal *PlanReplyRefusal
	if !errors.As(err, &refusal) || refusal.Reason != "This answer was not applied; please post it again." {
		t.Fatalf("finished binding was not retired: %v", err)
	}
	if p.attempts(t, p.parked.ID) != attempts || p.latest(t).ID != p.parked.ID {
		t.Fatal("retirement executed graph work")
	}
	next := p.answer(t, 9971, "1A, 2A again")
	calls := discussionRunner(t, p.f, "technical", next.Trigger.Body)
	executeGraphPlan(t, p.f, next)
	successor := p.latest(t)
	predecessor, _, err := p.f.db.WorkflowPredecessor(t.Context(), successor.ID)
	mustProgress(t, err)
	if successor.ID == p.parked.ID || predecessor != p.parked.ID || *calls != 1 {
		t.Fatalf("later comment did not launch a successor: %+v predecessor=%d calls=%d", successor, predecessor, *calls)
	}
}

// C3, #1007 C7: a non-story trigger replaying a finished launch is refused
// before launch with its reason, so the daemon reports it and consumes the
// comment instead of deferring a run-less graph result forever.
func TestPlanFinishedLaunch_NonStoryTriggerReturnsReason(t *testing.T) {
	p := newParkedPlanStory(t)
	req := p.answer(t, 9972, "1A, 2A")
	p.bindFinished(t, req)
	attempts := p.attempts(t, p.parked.ID)
	req.Trigger.PlanReply = nil
	out, err := Execute(t.Context(), p.f.opts, req)
	if err != nil || out.Err != nil || out.ResearchGraph || out.Disposition != DispositionPrepareFailed || out.Reason != planAnswerNotApplied {
		t.Fatalf("non-story replay: out=%+v err=%v", out, err)
	}
	if p.attempts(t, p.parked.ID) != attempts {
		t.Fatal("non-story replay executed graph work")
	}
}

// C11, C14: the finished binding is retired before any capture restore, so
// changed retained instructions or an unrestorable capture never surface as an
// invariant failure.
func TestPlanFinishedLaunch_RetiredBeforeCaptureRestore(t *testing.T) {
	for name, corrupt := range map[string]func(*parkedPlanStory, *testing.T, int64){
		"instructions differ": func(p *parkedPlanStory, t *testing.T, trigger int64) {
			files := []byte(`{"plan.dot":"ZGlncmFwaCB7fQ=="}`)
			sum := sha256.Sum256(files)
			p.probe(t, `UPDATE research_launch_definitions SET files=?,files_digest=? WHERE trigger_id=?`, files, hex.EncodeToString(sum[:]), trigger)
		},
		"capture unrestorable": func(p *parkedPlanStory, t *testing.T, trigger int64) {
			p.probe(t, `UPDATE research_launch_definitions SET files_digest='0000' WHERE trigger_id=?`, trigger)
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newParkedPlanStory(t)
			req := p.answer(t, 9973, "1A, 2A")
			p.bindFinished(t, req)
			corrupt(p, t, req.Trigger.CommentID)
			if _, err := p.f.db.WorkflowCaptureForAdmission(t.Context(), req.SessionID, req.Trigger.CommentID, "plan"); name == "capture unrestorable" && !errors.Is(err, store.ErrGraphInvariant) {
				t.Fatalf("capture is still restorable: %v", err)
			}
			_, err := Execute(t.Context(), p.f.opts, req)
			var refusal *PlanReplyRefusal
			if !errors.As(err, &refusal) || refusal.Reason != planAnswerNotApplied || errors.Is(err, store.ErrGraphInvariant) {
				t.Fatalf("finished binding was not retired before restore: %v", err)
			}
			req.Trigger.PlanReply = nil
			out, err := Execute(t.Context(), p.f.opts, req)
			if err != nil || out.ResearchGraph || out.Disposition != DispositionPrepareFailed || out.Reason != planAnswerNotApplied {
				t.Fatalf("non-story replay: out=%+v err=%v", out, err)
			}
		})
	}
}

// #1007 C4: an owner reply that failed-story retry routing turns into a fresh
// pass (discussion becomes false) is held and released like any other reply
// while the latest instance is parked at a non-agent step: it is never bound
// to the parked instance, and past the hold a successor's agent pass runs.
func TestPlanParkedWithoutAgent_FailedStoryRetryRoutingIsReleased(t *testing.T) {
	p := newParkedPlanStory(t)
	// A stale settled effect marker makes retryFailedStory route the reply as
	// a retry of failed work instead of a discussion.
	mustProgress(t, p.f.db.UpdatePlanWorkflowState(t.Context(), p.f.ref().String(), func(s *planbrief.PlanState) (bool, error) {
		for i := range s.Stories {
			if s.Stories[i].Key == "story-001" {
				s.Stories[i].Wait = nil
				s.Stories[i].Effect, s.Stories[i].EffectRequest = "stale", ""
			}
		}
		return true, nil
	}))
	req := p.answer(t, 9962, "Retry")
	calls := 0
	p.f.opts.Runners = runnerSet(lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
		calls++
		if req.Graph == nil || req.Graph.Node != "technical_agent" {
			t.Fatalf("retry reached another node: %+v", req.Graph)
		}
		return nil, errors.New("scripted retry agent stops here")
	}))

	_, err := Execute(t.Context(), p.f.opts, req)
	if !errors.Is(err, errPlanStepFinishing) {
		t.Fatalf("retry was not held behind the parked step: %v", err)
	}
	if _, found, e := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID); e != nil || found {
		t.Fatalf("held retry has a launch row: found=%v err=%v", found, e)
	}
	if still := p.latest(t); still.ID != p.parked.ID || still.Status != store.GraphRunning || calls != 0 {
		t.Fatalf("hold changed the parked instance: %+v calls=%d", still, calls)
	}

	p.now = p.now.Add(planParkedHold + time.Minute)
	_, _ = Execute(t.Context(), p.f.opts, req)
	if state := graphPlanState(t, p.f); state.Stories[0].Effect != "" {
		t.Fatalf("reply was not routed through failed-story retry: %+v", state.Stories[0])
	}
	cancelled, err := p.f.db.GetGraphInstance(t.Context(), p.parked.ID)
	mustProgress(t, err)
	if cancelled.Status != store.GraphCancelled {
		t.Fatalf("parked instance was not released: %+v", cancelled)
	}
	bound, found, err := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || bound == p.parked.ID {
		t.Fatalf("retry was bound to the parked instance: found=%v instance=%d", found, bound)
	}
	predecessor, _, err := p.f.db.WorkflowPredecessor(t.Context(), bound)
	mustProgress(t, err)
	if predecessor != p.parked.ID || calls != 1 {
		t.Fatalf("successor did not run one agent pass: predecessor=%d calls=%d", predecessor, calls)
	}
}

// discussionNodes are the nodes whose confirmed publication lets the daemon
// consume an owner discussion comment (daemon requirePlanDiscussionReported).
var discussionNodes = []string{"failed", "set_reply", "set_review", "set_ask", "story_reply", "story_review", "story_ask", "technical_reply", "technical_review", "technical_ask", "acceptance_reply", "acceptance_review", "acceptance_ask"}

func discussionReported(t *testing.T, f *fixture, runID int64) bool {
	t.Helper()
	for _, node := range discussionNodes {
		reported, err := f.db.GraphFailureReported(t.Context(), runID, node)
		mustProgress(t, err)
		if reported {
			return true
		}
	}
	return false
}

func (p *parkedPlanStory) answerPosts(t *testing.T) int {
	t.Helper()
	n := 0
	for _, post := range p.forge.all() {
		if strings.Contains(post.body, "This plan delivers bounded story shaping.") {
			n++
		}
	}
	return n
}

// #1007 C5: after the hold, the successor answers the owner reply with a
// confirmed publication from the run whose trigger is that reply, which is
// exactly what lets the daemon consume it.
func TestPlanParkedWithoutAgent_SuccessorPublishesReplyFromItsRun(t *testing.T) {
	p := newParkedPlanStory(t)
	reply := "1A, 2A"
	req := p.answer(t, 9963, reply)
	calls := discussionRunner(t, p.f, "technical", reply)
	p.now = p.now.Add(planParkedHold + time.Minute)
	before := p.answerPosts(t)
	out, err := Execute(t.Context(), p.f.opts, req)
	if err != nil || out.Err != nil {
		t.Fatalf("successor pass failed: %v / %v", err, out.Err)
	}
	run, found, err := p.f.db.GetRunByTrigger(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || out.Run.ID != run.ID || *calls != 1 {
		t.Fatalf("no run for the reply: found=%v out=%d run=%d calls=%d", found, out.Run.ID, run.ID, *calls)
	}
	reported, err := p.f.db.GraphFailureReported(t.Context(), run.ID, "technical_reply")
	mustProgress(t, err)
	if !reported || !discussionReported(t, p.f, run.ID) {
		t.Fatal("successor answer is not a confirmed publication of the reply's run")
	}
	if got := p.answerPosts(t) - before; got < 1 {
		t.Fatalf("answer not posted: %d", got)
	}
	posts := len(p.forge.all())
	again, err := Execute(t.Context(), p.f.opts, req)
	if err != nil || again.Run.ID != run.ID || *calls != 1 || len(p.forge.all()) != posts {
		t.Fatalf("replay of the handled reply repeated work: err=%v run=%d calls=%d", err, again.Run.ID, *calls)
	}
}

// #1007 C6: an uncertain first delivery of the successor's answer leaves the
// reply unreported with its own run on the successor; the retry reconciles the
// same delivery key, so exactly one answer is posted and no second agent runs.
func TestPlanParkedWithoutAgent_UncertainSuccessorDeliveryCompletesOnRetry(t *testing.T) {
	p := newParkedPlanStory(t)
	reply := "1A, 2A"
	req := p.answer(t, 9964, reply)
	calls := discussionRunner(t, p.f, "technical", reply)
	answer := p.f.opts.Runners
	p.f.opts.Runners = runnerSet(lifecycleMCPHost(func(ctx context.Context, r agent.Request) (*agent.RunResult, error) {
		// Only the successor's own answer meets the failing forge; the parked
		// instance's retained comment is recovered before this pass starts.
		p.forge.failWith(503)
		return answer[agent.Kind("claude")].Run(ctx, r)
	}))
	p.now = p.now.Add(planParkedHold + time.Minute)
	out, err := Execute(t.Context(), p.f.opts, req)
	if err == nil && out.Err == nil {
		t.Fatal("failed publication completed")
	}
	if errors.Is(err, store.ErrGraphInstanceTerminal) || errors.Is(out.Err, store.ErrGraphInstanceTerminal) {
		t.Fatalf("reply was bound to an ended instance: %v / %v", err, out.Err)
	}
	run, found, err := p.f.db.GetRunByTrigger(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || *calls != 1 || discussionReported(t, p.f, run.ID) {
		t.Fatalf("uncertain answer counted as reported: found=%v calls=%d", found, *calls)
	}
	// The reply has its own run, so a retry replays that run and reconciles its
	// retained delivery; it never re-enters admission for the successor.
	bound, found, err := p.f.db.WorkflowLaunchInstance(t.Context(), req.SessionID, req.Trigger.CommentID)
	mustProgress(t, err)
	if !found || bound == p.parked.ID || run.ID == 0 {
		t.Fatalf("pending reply is not on a successor with its own run: instance=%d run=%d", bound, run.ID)
	}
	pending, err := p.f.db.ListPendingReportDeliveries(t.Context())
	mustProgress(t, err)
	var held store.ReportDelivery
	for _, d := range pending {
		if strings.Contains(d.Body, "This plan delivers bounded story shaping.") {
			held = d
		}
	}
	if held.Key == "" || held.Completed {
		t.Fatal("answer publication not retained")
	}
	// The first post reached the forge though its response was lost.
	p.forge.mu.Lock()
	p.forge.posts = append(p.forge.posts, forgePost{path: fmt.Sprintf("/repos/acme/acme-api/issues/%d/comments", req.Issue.Number), body: held.Body, id: 6101})
	p.forge.mu.Unlock()
	p.forge.recover()
	var replayed Result
	if out.Repair != nil {
		replayed, err = out.Repair(t.Context())
	} else {
		replayed, err = Execute(t.Context(), p.f.opts, req)
	}
	if err != nil || replayed.Err != nil {
		t.Fatalf("retry failed: %v / %v", err, replayed.Err)
	}
	delivery, err := p.f.db.GetReportDelivery(t.Context(), held.Key)
	mustProgress(t, err)
	keyed := 0
	for _, post := range p.forge.all() {
		if strings.Contains(post.body, "key="+held.Key) {
			keyed++
		}
	}
	if !delivery.Completed || *calls != 1 || keyed != 1 || !discussionReported(t, p.f, run.ID) {
		t.Fatalf("retry duplicated or lost the answer: delivery=%+v calls=%d posts=%d", delivery, *calls, keyed)
	}
}
