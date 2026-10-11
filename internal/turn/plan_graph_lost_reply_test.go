package turn

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
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
