package turn

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const graphTestDOT = `digraph {team=test;mode=implement;profile_version=1;start=work;
 work [lf_action=lf_implement_work,lf_outcomes="done,failed",lf_custom=original];
 end [lf_terminal=true]; work -> end [on=done]; work -> end [on=failed];}`

// graphTestHandler scripts outcomes per node while the fixture's real store
// performs every reservation and completion. The hook can inspect durable state
// during dispatch, demonstrating that no database transaction remains open.
type graphTestHandler struct {
	results map[string][]ActivityResult
	errs    map[string]error
	calls   []ActivityRequest
	hook    func(context.Context, Options, ActivityRequest)
}

func (h *graphTestHandler) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	h.calls = append(h.calls, req)
	if h.hook != nil {
		h.hook(ctx, opts, req)
	}
	results := h.results[req.Node]
	var result ActivityResult
	if len(results) > 0 {
		result = results[0]
		h.results[req.Node] = results[1:]
	}
	return result, h.errs[req.Node]
}

// graphRequest extends the established turn fixture, retaining its migrated
// SQLite database and registered session instead of creating a parallel fixture.
func (f *fixture) graphRequest(dot string, handler *graphTestHandler) GraphNodeRequest {
	f.t.Helper()
	p, err := graphexec.ParseProfile([]byte(dot))
	if err != nil {
		f.t.Fatal(err)
	}
	var registry graphexec.Registry
	if err = registry.Register(graphexec.ActionSpec{Name: "lf_implement_work", Mode: agent.ModeImplement, Outcomes: []string{"done", "failed", "again"}}); err != nil {
		f.t.Fatal(err)
	}
	if err = graphexec.Validate(p, registry); err != nil {
		f.t.Fatal(err)
	}
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	def, err := f.db.CreateGraphDefinition(f.t.Context(), store.GraphDefinition{Team: p.Team, Mode: string(p.Mode), ProfileVersion: p.ProfileVersion, Path: "test/implement.dot", Digest: p.Digest, CreatedAt: at})
	if err != nil {
		f.t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(f.t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: p.Start, StartedAt: at, NextDueAt: at})
	if err != nil {
		f.t.Fatal(err)
	}
	req := GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at, NextDueAt: at.Add(time.Minute), ResumeRef: "runtime-reference"}
	if handler != nil {
		if err = req.Activities.Register("lf_implement_work", handler); err != nil {
			f.t.Fatal(err)
		}
	}
	return req
}

func TestGraphActivityRegistry(t *testing.T) {
	var registry ActivityRegistry
	handler := &graphTestHandler{}
	var typedNil *graphTestHandler
	for _, tc := range []struct {
		name    string
		handler ActivityHandler
	}{{"", handler}, {"  ", handler}, {"work", nil}, {"work", typedNil}} {
		if err := registry.Register(tc.name, tc.handler); err == nil {
			t.Fatalf("accepted invalid registration %+v", tc)
		}
	}
	if err := registry.Register("work", handler); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("work", &graphTestHandler{}); err == nil {
		t.Fatal("replaced handler")
	}
	got, ok := registry.Resolve("work")
	if !ok || got != handler {
		t.Fatal("registered handler missing")
	}
	if _, ok = registry.Resolve("missing"); ok {
		t.Fatal("unknown handler resolved")
	}
	var absent *ActivityRegistry
	if err := absent.Register("work", handler); err == nil {
		t.Fatal("nil registry accepted")
	}
}

func TestGraphNodeDispatchPersistenceAndCloning(t *testing.T) {
	f := newFixture(t, nil)
	payload := []byte("conducted")
	h := &graphTestHandler{results: map[string][]ActivityResult{"work": {{Outcome: "done", Kind: store.GraphOutputText, Output: payload}}}}
	req := f.graphRequest(graphTestDOT, h)
	f.opts.Tiers = config.Tiers{ModelByTier: map[config.Kind]map[config.Tier]string{config.KindClaude: {config.TierFast: "original"}}}
	f.opts.Runners = RunnerSet{agent.Kind(store.AgentClaude): nil}
	h.hook = func(ctx context.Context, opts Options, r ActivityRequest) {
		attempt, err := opts.DB.GetGraphNodeAttempt(ctx, r.NodeAttemptID)
		if err != nil || attempt.Status != store.GraphRunning {
			t.Fatalf("reservation not committed: %+v %v", attempt, err)
		}
		if r.InstanceID != req.InstanceID || r.Team != "test" || r.Mode != agent.ModeImplement || r.Node != "work" || r.ResumeRef != req.ResumeRef {
			t.Fatalf("request: %+v", r)
		}
		r.Attrs["lf_custom"] = "mutated"
		opts.Tiers.ModelByTier[config.KindClaude][config.TierFast] = "mutated"
		delete(opts.Runners, agent.Kind(store.AgentClaude))
	}
	ctx, cancel := context.WithTimeout(t.Context(), testutil.Wait)
	defer cancel()
	result, err := ExecuteNodeAction(ctx, f.opts, req)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	if string(result.Activity.Output) != "conducted" {
		t.Fatal("result aliases handler output")
	}
	node, _ := req.Profile.Node("work")
	if node.Attrs["lf_custom"] != "original" || f.opts.Tiers.ModelByTier[config.KindClaude][config.TierFast] != "original" || len(f.opts.Runners) != 1 {
		t.Fatal("handler mutated shared configuration")
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].ID != result.AttemptID || attempts[0].Attempt != 1 || attempts[0].Status != store.GraphSucceeded || attempts[0].Outcome != "done" {
		t.Fatalf("attempts: %+v", attempts)
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), result.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 1 || outputs[0].Kind != store.GraphOutputText || string(outputs[0].Payload) != "conducted" {
		t.Fatalf("outputs: %+v", outputs)
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].FromNode != "work" || transitions[0].ToNode != "end" || transitions[0].OnOutcome != "done" {
		t.Fatalf("transitions: %+v", transitions)
	}
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if instance.CurrentNode != "end" || instance.Status != store.GraphRunning || !instance.NextDueAt.Equal(req.NextDueAt) {
		t.Fatalf("instance: %+v", instance)
	}
	terminal, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Status != store.GraphSucceeded || terminal.AttemptID != 0 || len(h.calls) != 1 {
		t.Fatalf("terminal: %+v calls=%d", terminal, len(h.calls))
	}
	instance, err = f.db.GetGraphInstance(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, attempts) || instance.Status != store.GraphSucceeded || !instance.NextDueAt.IsZero() || !instance.FinishedAt.Equal(req.At) {
		t.Fatalf("terminal persistence: %+v %+v", instance, after)
	}
}

func TestGraphNodeRefusalsAndFailureOutcomes(t *testing.T) {
	dispatchErr := errors.New("transport unavailable")
	for _, tc := range []struct {
		name                             string
		unknown                          bool
		result                           ActivityResult
		err                              error
		wantStatus, wantNode, diagnostic string
	}{
		{name: "unknown activity", unknown: true, wantStatus: store.GraphFailed, wantNode: "work", diagnostic: "unknown graph activity"},
		{name: "undeclared outcome", result: ActivityResult{Outcome: "surprise"}, wantStatus: store.GraphFailed, wantNode: "work", diagnostic: "undeclared"},
		{name: "handler diagnostic", result: ActivityResult{Diagnostic: "worker could not acquire input"}, err: dispatchErr, wantStatus: store.GraphFailed, wantNode: "work", diagnostic: "worker could not acquire input"},
		{name: "error diagnostic fallback", err: dispatchErr, wantStatus: store.GraphFailed, wantNode: "work", diagnostic: dispatchErr.Error()},
		{name: "conducted failure", result: ActivityResult{Outcome: "failed", Diagnostic: "checks failed"}, wantStatus: store.GraphSucceeded, wantNode: "end", diagnostic: "checks failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			h := &graphTestHandler{results: map[string][]ActivityResult{"work": {tc.result}}, errs: map[string]error{"work": tc.err}}
			req := f.graphRequest(graphTestDOT, h)
			if tc.unknown {
				req.Activities = ActivityRegistry{}
			}
			result, err := ExecuteNodeAction(t.Context(), f.opts, req)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 || attempts[0].Status != tc.wantStatus {
				t.Fatalf("attempts: %+v", attempts)
			}
			if tc.wantStatus == store.GraphFailed && attempts[0].Outcome != "" {
				t.Fatal("failed attempt retained outcome")
			}
			instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			if instance.CurrentNode != tc.wantNode {
				t.Fatalf("instance: %+v", instance)
			}
			transitions, err := f.db.ListGraphTransitions(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			wantTransitions := 0
			if tc.wantStatus == store.GraphSucceeded {
				wantTransitions = 1
			}
			if len(transitions) != wantTransitions {
				t.Fatalf("transitions: %+v", transitions)
			}
			outputs, err := f.db.ListGraphOutputs(t.Context(), result.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if len(outputs) != 1 || outputs[0].Kind != store.GraphOutputDiagnostic || !strings.Contains(string(outputs[0].Payload), tc.diagnostic) || !strings.Contains(result.Activity.Diagnostic, tc.diagnostic) {
				t.Fatalf("diagnostic lost: %+v %+v", result, outputs)
			}
		})
	}
}

func TestGraphNodeCompletionFailureIsAtomic(t *testing.T) {
	f := newFixture(t, nil)
	h := &graphTestHandler{results: map[string][]ActivityResult{"work": {{Outcome: "done", Kind: "unsupported", Output: []byte("result")}}}}
	req := f.graphRequest(graphTestDOT, h)
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err == nil || !strings.Contains(err.Error(), "complete graph activity") {
		t.Fatalf("completion error: %v", err)
	}
	attempt, err := f.db.GetGraphNodeAttempt(t.Context(), result.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), result.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := f.db.ListGraphTransitions(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status != store.GraphRunning || len(outputs) != 0 || len(transitions) != 0 || instance.CurrentNode != "work" {
		t.Fatalf("partial completion: %+v %+v %+v %+v", attempt, outputs, transitions, instance)
	}
	if _, err = ExecuteNodeAction(t.Context(), f.opts, req); !errors.Is(err, store.ErrGraphAttemptRunning) {
		t.Fatalf("reserved attempt redispatched: %v", err)
	}
	if len(h.calls) != 1 {
		t.Fatal("duplicate dispatch")
	}
}

func TestGraphNodeRejectsMismatchedPins(t *testing.T) {
	for _, field := range []string{"digest", "team", "mode", "version"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t, nil)
			h := &graphTestHandler{}
			req := f.graphRequest(graphTestDOT, h)
			switch field {
			case "digest":
				req.Profile.Digest = strings.Repeat("0", 64)
			case "team":
				req.Profile.Team = "other"
			case "mode":
				req.Profile.Mode = agent.ModeResearch
			case "version":
				req.Profile.ProfileVersion++
			}
			if _, err := ExecuteNodeAction(t.Context(), f.opts, req); err == nil || !strings.Contains(err.Error(), "pinned definition") {
				t.Fatalf("pin mismatch: %v", err)
			}
			attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 0 || len(h.calls) != 0 {
				t.Fatal("mismatched pin dispatched")
			}
		})
	}
}

func TestGraphNodeDispatchLimit(t *testing.T) {
	f := newFixture(t, nil)
	h := &graphTestHandler{results: map[string][]ActivityResult{"work": {{Outcome: "again"}, {Outcome: "again"}}}}
	dot := `digraph {team=test;mode=implement;profile_version=1;start=work;
 work [lf_action=lf_implement_work,lf_outcomes="again,done",lf_limit=1];
 end [lf_terminal=true];work -> work [on=again];work -> end [on=done];}`
	req := f.graphRequest(dot, h)
	if _, err := ExecuteNodeAction(t.Context(), f.opts, req); err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != store.GraphFailed || result.AttemptID != 0 || !strings.Contains(result.Activity.Diagnostic, "limit 1 exhausted") || len(h.calls) != 1 {
		t.Fatalf("limit result: %+v calls=%d", result, len(h.calls))
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != store.GraphSucceeded || instance.Status != store.GraphFailed || instance.CurrentNode != "work" || !instance.NextDueAt.IsZero() || !instance.FinishedAt.Equal(req.At) {
		t.Fatalf("limit persistence: %+v %+v", attempts, instance)
	}
}

// Failed owner attempts renew allowances; topology changes within an attempt do not.
func TestResearchBudgetFailureBoundary(t *testing.T) {
	for _, node := range []graphexec.Node{
		{Name: "report", Action: ResearchCommentAction},
		{Name: "investigate", Action: ResearchAgentAction, Attrs: map[string]string{"lf_agent_markdown": "research.md"}},
	} {
		for _, variant := range []string{"failed", "failed then topology", "topology only", "succeeded"} {
			t.Run(node.Name+"/"+variant, func(t *testing.T) {
				ctx := t.Context()
				f := newFixture(t, nil)
				_, err := f.db.AdmitResearchPath(ctx, f.session.ID, "graph")
				mustProgress(t, err)
				pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64)})
				mustProgress(t, err)
				launch := store.ResearchLaunch{SessionID: f.session.ID, TriggerID: 10, DefinitionID: pin.ID, Start: node.Name, ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte("{}")}
				first, err := f.db.AdmitResearchLaunch(ctx, launch)
				mustProgress(t, err)
				at := time.Now()
				spent, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: first.Instance.ID, Node: node.Name, Limit: 1, StartedAt: at})
				mustProgress(t, err)
				status := store.GraphFailed
				switch variant {
				case "topology only":
					status = store.GraphRunning
				case "succeeded":
					status = store.GraphSucceeded
				}
				mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, spent.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: status}))
				before, err := f.db.ListGraphNodeAttempts(ctx, first.Instance.ID)
				mustProgress(t, err)
				launch.TriggerID++
				launch.TopologyDigest = strings.Repeat("d", 64)
				next, err := f.db.AdmitResearchLaunch(ctx, launch)
				mustProgress(t, err)
				if next.PredecessorID != first.Instance.ID {
					t.Fatal("missing predecessor")
				}
				if variant == "failed then topology" {
					launch.TriggerID++
					launch.TopologyDigest = strings.Repeat("e", 64)
					next, err = f.db.AdmitResearchLaunch(ctx, launch)
					mustProgress(t, err)
				}
				ancestors, err := researchBudgetAncestors(ctx, f.db, next.Instance.ID, node)
				mustProgress(t, err)
				start := store.GraphAttemptStart{InstanceID: next.Instance.ID, Node: node.Name, Limit: 1, StartedAt: at, ResearchPriorNodes: ancestors}
				attempt, err := f.db.StartGraphNodeAttempt(ctx, start)
				if variant == "topology only" {
					if !errors.Is(err, store.ErrGraphAttemptLimit) {
						t.Fatalf("topology renewed exhausted allowance: %v", err)
					}
				} else {
					mustProgress(t, err)
					mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning}))
					if _, err = f.db.StartGraphNodeAttempt(ctx, start); !errors.Is(err, store.ErrGraphAttemptLimit) {
						t.Fatalf("fresh attempt lost its own limit: %v", err)
					}
				}
				after, err := f.db.ListGraphNodeAttempts(ctx, first.Instance.ID)
				mustProgress(t, err)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("historical dispatch evidence changed")
				}
			})
		}
	}
}

func TestResearchFailedRetryWaitsForPublicationDebt(t *testing.T) {
	ctx := t.Context()
	f := newFixture(t, nil)
	_, err := f.db.AdmitResearchPath(ctx, f.session.ID, "graph")
	mustProgress(t, err)
	pin, err := f.db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64)})
	mustProgress(t, err)
	launch := store.ResearchLaunch{SessionID: f.session.ID, TriggerID: 10, DefinitionID: pin.ID, Start: "investigate", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte("{}")}
	first, err := f.db.AdmitResearchLaunch(ctx, launch)
	mustProgress(t, err)
	at := time.Now()
	attempt, err := f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: first.Instance.ID, Node: "investigate", StartedAt: at})
	mustProgress(t, err)
	mustProgress(t, f.db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: at, InstanceStatus: store.GraphFailed}))
	mustProgress(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "unpublished-failure", InstanceID: first.Instance.ID, SourceAttemptID: attempt.ID, Node: "failed", Kind: "failure", Candidate: []byte(researchDoneJSON)}))
	launch.TriggerID++
	if _, err = f.db.AdmitResearchLaunch(ctx, launch); !errors.Is(err, store.ErrWorkflowEffectsPending) {
		t.Fatalf("retry bypassed unresolved publication: %v", err)
	}
	latest, err := f.db.LatestResearchInstance(ctx, f.session.ID)
	mustProgress(t, err)
	if latest.ID != first.Instance.ID || latest.Status != store.GraphFailed {
		t.Fatalf("blocked retry changed instance: %+v", latest)
	}
}

// A discovery-budget deferral inside a Plan activity is transient: the
// instance stays running at the node with a later due time instead of failing.
func TestGraphNodePlanDiscoveryDeferralRetries(t *testing.T) {
	f := newFixture(t, nil)
	const dot = `digraph {team=test;mode=plan;profile_version=1;start=story;
 story [lf_action=lf_plan_story,lf_outcomes="done",lf_limit=3];
 end [lf_terminal=true]; story -> end [on=done];}`
	p, err := graphexec.ParseProfile([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}
	var registry graphexec.Registry
	if err = registry.Register(graphexec.ActionSpec{Name: "lf_plan_story", Mode: agent.ModePlan, Outcomes: []string{"done"}}); err != nil {
		t.Fatal(err)
	}
	if err = graphexec.Validate(p, registry); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 17, 20, 50, 40, 0, time.UTC)
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: p.Team, Mode: string(p.Mode), ProfileVersion: p.ProfileVersion, Path: "test/plan.dot", Digest: p.Digest, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: p.Start, StartedAt: at, NextDueAt: at})
	if err != nil {
		t.Fatal(err)
	}
	deferred := fmt.Errorf("ticket: read container: %w", fmt.Errorf("github: inspect repository: %w", ticket.ErrDiscoveryDeferred))
	h := &graphTestHandler{errs: map[string]error{"story": deferred}}
	req := GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at, StopOnFailure: true, RetryDueAt: at.Add(time.Minute)}
	if err = req.Activities.Register("lf_plan_story", h); err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil {
		t.Fatalf("deferral returned run error: %v", err)
	}
	if !result.Deferred || result.NextNode != "story" || result.Status == store.GraphFailed {
		t.Fatalf("result: %+v", result)
	}
	got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.GraphRunning || got.CurrentNode != "story" || !got.NextDueAt.After(at) {
		t.Fatalf("instance: %+v", got)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != store.GraphFailed {
		t.Fatalf("attempts: %+v", attempts)
	}
	// Other dispatch errors keep the existing terminal failure.
	h.errs["story"] = errors.New("transport unavailable")
	req.At = at.Add(2 * time.Minute)
	if _, err = ExecuteNodeAction(t.Context(), f.opts, req); err == nil {
		t.Fatal("ordinary dispatch error was not reported")
	}
	if got, err = f.db.GetGraphInstance(t.Context(), instance.ID); err != nil || got.Status != store.GraphFailed {
		t.Fatalf("ordinary failure instance: %+v %v", got, err)
	}
}

// parentLockTestHandler serializes on a real Plan parent story-progress lock,
// like the production lf_plan_story effect, and records how it was entered.
type parentLockTestHandler struct {
	db           *store.DB
	parent, dir  string
	locked       bool
	err          error
	lockedCalls  *[]bool
	parentBusies *[]bool
}

func (h parentLockTestHandler) Execute(ctx context.Context, _ Options, _ ActivityRequest) (ActivityResult, error) {
	*h.lockedCalls = append(*h.lockedCalls, h.locked)
	release, err := h.db.LockStoryProgress(ctx, h.parent, h.dir)
	*h.parentBusies = append(*h.parentBusies, errors.Is(err, store.ErrStoryProgressBusy))
	if err == nil {
		if err = release(); err != nil {
			return ActivityResult{}, err
		}
	}
	if h.err != nil {
		return ActivityResult{}, h.err
	}
	return ActivityResult{Outcome: "done"}, nil
}

func (h parentLockTestHandler) lockParent(ctx context.Context) (func() error, error) {
	return lockPlanStory(ctx, 5*time.Millisecond, planStoryLockWait, func() (func() error, error) {
		return h.db.LockStoryProgress(ctx, h.parent, h.dir)
	})
}

func (h parentLockTestHandler) withParentLocked() ActivityHandler {
	h.locked = true
	return h
}

// A busy Plan parent defers lf_plan_story before any attempt is reserved, so
// contention spends neither the node limit nor the revision budget (#997).
func TestGraphNodePlanStoryBusyParentDefersWithoutAttempt(t *testing.T) {
	previous := planStoryLockWait
	planStoryLockWait = 20 * time.Millisecond
	defer func() { planStoryLockWait = previous }()
	f := newFixture(t, nil)
	lockDir := t.TempDir()
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	start := func(t *testing.T, action string) (*graphexec.Profile, store.GraphInstance) {
		t.Helper()
		dot := `digraph {team=test;mode=plan;profile_version=1;start=story;
 story [lf_action=` + action + `,lf_outcomes="done",lf_limit=1];
 end [lf_terminal=true]; story -> end [on=done];}`
		p, err := graphexec.ParseProfile([]byte(dot))
		if err != nil {
			t.Fatal(err)
		}
		var registry graphexec.Registry
		if err = registry.Register(graphexec.ActionSpec{Name: action, Mode: agent.ModePlan, Outcomes: []string{"done"}}); err != nil {
			t.Fatal(err)
		}
		if err = graphexec.Validate(p, registry); err != nil {
			t.Fatal(err)
		}
		def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: p.Team, Mode: string(p.Mode), ProfileVersion: p.ProfileVersion, Path: "test/" + action + ".dot", Digest: p.Digest, CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		instance, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: p.Start, StartedAt: at, NextDueAt: at})
		if err != nil {
			t.Fatal(err)
		}
		return p, instance
	}
	var lockedCalls, parentBusies []bool
	h := parentLockTestHandler{db: f.db, parent: f.session.TaskRef, dir: lockDir, lockedCalls: &lockedCalls, parentBusies: &parentBusies}
	release, err := f.db.LockStoryProgress(t.Context(), f.session.TaskRef, lockDir)
	if err != nil {
		t.Fatal(err)
	}
	p, instance := start(t, graphexec.PlanStoryAction)
	req := GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at, StopOnFailure: true, RetryDueAt: at.Add(time.Minute)}
	if err = req.Activities.Register(graphexec.PlanStoryAction, h); err != nil {
		t.Fatal(err)
	}
	// AC-1/AC-3: busy beyond the wait, repeatedly, without exhausting lf_limit=1.
	for range 3 {
		result, err := ExecuteNodeAction(t.Context(), f.opts, req)
		if err != nil {
			t.Fatalf("busy parent returned run error: %v", err)
		}
		if !result.Deferred || result.NextNode != "story" || result.Status == store.GraphFailed || result.AttemptID != 0 {
			t.Fatalf("result: %+v", result)
		}
		got, err := f.db.GetGraphInstance(t.Context(), instance.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.GraphRunning || got.CurrentNode != "story" {
			t.Fatalf("instance: %+v", got)
		}
		attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(attempts) != 0 || len(lockedCalls) != 0 {
			t.Fatalf("busy parent reserved attempts=%+v or ran handler=%v", attempts, lockedCalls)
		}
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	// Once free, one attempt runs with the parent lock already held for it.
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if err != nil || result.Deferred || result.NextNode != "end" || result.AttemptID == 0 {
		t.Fatalf("released parent: %+v %v", result, err)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].Status != store.GraphSucceeded || !slices.Equal(lockedCalls, []bool{true}) || !slices.Equal(parentBusies, []bool{true}) {
		t.Fatalf("attempts=%+v locked=%v busy=%v", attempts, lockedCalls, parentBusies)
	}
	if again, err := f.db.LockStoryProgress(t.Context(), f.session.TaskRef, lockDir); err != nil {
		t.Fatalf("parent lock not released after the step: %v", err)
	} else if err = again(); err != nil {
		t.Fatal(err)
	}

	// AC-2: a plain dispatch error under a free parent still fails the step.
	p, instance = start(t, graphexec.PlanStoryAction)
	req = GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at, StopOnFailure: true, RetryDueAt: at.Add(time.Minute)}
	failing := h
	failing.err = errors.New("transport unavailable")
	if err = req.Activities.Register(graphexec.PlanStoryAction, failing); err != nil {
		t.Fatal(err)
	}
	if _, err = ExecuteNodeAction(t.Context(), f.opts, req); err == nil {
		t.Fatal("ordinary dispatch error was not reported")
	}
	if got, err := f.db.GetGraphInstance(t.Context(), instance.ID); err != nil || got.Status != store.GraphFailed {
		t.Fatalf("ordinary failure instance: %+v %v", got, err)
	}

	// AC-2/AC-3: another Plan action whose handler reports a busy parent fails as today.
	p, instance = start(t, "lf_plan_other")
	req = GraphNodeRequest{Profile: p, InstanceID: instance.ID, At: at, StopOnFailure: true, RetryDueAt: at.Add(time.Minute)}
	other := &graphTestHandler{errs: map[string]error{"story": fmt.Errorf("other: %w", store.ErrStoryProgressBusy)}}
	if err = req.Activities.Register("lf_plan_other", other); err != nil {
		t.Fatal(err)
	}
	result, err = ExecuteNodeAction(t.Context(), f.opts, req)
	if err == nil || result.Deferred || !errors.Is(err, store.ErrStoryProgressBusy) {
		t.Fatalf("other Plan action busy error: %+v %v", result, err)
	}
	if got, err := f.db.GetGraphInstance(t.Context(), instance.ID); err != nil || got.Status != store.GraphFailed {
		t.Fatalf("other Plan action instance: %+v %v", got, err)
	}
}

// An owner stop cancels the worker context mid-activity. The attempt must still
// settle, and an Implement instance must be cancelled as restart recovery does,
// so the story is not stuck behind a running attempt until a daemon restart.
func TestGraphNodeOwnerStopSettlesCancelledAttempt(t *testing.T) {
	f := newFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "owner-stop-worker", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: string(agent.ModeImplement), Agent: store.AgentClaude, TriggerCommentID: 973, LogPath: func(int) string { return "stop.log" }})
	mustProgress(t, err)
	h := &graphTestHandler{
		errs: map[string]error{"work": context.Canceled},
		hook: func(context.Context, Options, ActivityRequest) {
			_, err := f.db.RequestOwnerStop(t.Context(), f.session.TaskRef)
			mustProgress(t, err)
			cancel()
		},
	}
	req := f.graphRequest(graphTestDOT, h)
	req.RunID = run.ID
	result, err := ExecuteNodeAction(ctx, f.opts, req)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "complete graph activity") {
		t.Fatalf("error = %v", err)
	}
	attempts, err := f.db.ListGraphNodeAttempts(t.Context(), req.InstanceID)
	mustProgress(t, err)
	if len(attempts) != 1 || attempts[0].Status != store.GraphFailed {
		t.Fatalf("attempt not settled: %+v", attempts)
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), result.AttemptID)
	mustProgress(t, err)
	if len(outputs) != 1 || outputs[0].Kind != store.GraphOutputDiagnostic || !strings.Contains(string(outputs[0].Payload), store.ReasonOwnerStop) {
		t.Fatalf("owner-stop diagnostic lost: %+v", outputs)
	}
	instance, err := f.db.GetGraphInstance(t.Context(), req.InstanceID)
	mustProgress(t, err)
	if instance.Status != store.GraphCancelled || result.Status != store.GraphCancelled {
		t.Fatalf("instance = %+v result = %+v", instance, result)
	}
	stopped, err := f.db.GetRun(t.Context(), run.ID)
	mustProgress(t, err)
	if _, ok := stopped.OwnerStop(); !ok {
		t.Fatalf("run lost owner stop: %+v", stopped)
	}
}

// A shorter parent deadline can expire during setup. The provider never starts,
// yet the real graph must retain its timeout transition and cleanup diagnostic
// so an idle held story can publish its own notice after the caller returns.
func TestGraphParentDeadlineTimeoutSettlesAndKeepsDiagnostic(t *testing.T) {
	f, forge, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("expired setup launched a provider")
		return nil, nil
	}))
	seedImplementStory(t, f)
	segment, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "parent-timeout-worker", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: string(agent.ModeImplement), Agent: store.AgentClaude, TriggerCommentID: 974, LogPath: func(int) string { return "timeout.log" }})
	mustProgress(t, err)
	req := f.graphRequest(graphTestDOT, nil)
	req.RunID = run.ID
	policy := ImplementWorkflowPolicy()
	policy.AfterAgentFailure = implementAfterAgentFailure
	handler := AgentActivity{Policy: policy, Prepare: func(ctx context.Context, _ Options, _ ActivityRequest) (ResearchInvocation, error) {
		<-ctx.Done()
		return ResearchInvocation{}, ctx.Err()
	}}
	mustProgress(t, req.Activities.Register("lf_implement_work", handler))
	f.opts.WorkerTimeoutSeconds = 1800
	f.opts.WorkerTimeoutSource = "settings.worker_timeout_seconds"
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	result, err := ExecuteNodeAction(ctx, f.opts, req)
	if err != nil || result.NextNode != "end" || result.Activity.Outcome != "failed" {
		t.Fatalf("deadline lost graph completion: %+v %v", result, err)
	}
	attempt, err := f.db.GetGraphNodeAttempt(t.Context(), result.AttemptID)
	mustProgress(t, err)
	if attempt.Status != store.GraphSucceeded || attempt.Outcome != "failed" {
		t.Fatalf("timeout outcome not durable: %+v", attempt)
	}
	outputs, err := f.db.ListGraphOutputs(t.Context(), attempt.ID)
	mustProgress(t, err)
	found := false
	for _, output := range outputs {
		if output.Kind == store.GraphOutputDiagnostic && strings.Contains(string(output.Payload), "Process evidence unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-process diagnostic lost: %+v", outputs)
	}
	row := implementWorkerRow(t, f)
	if row.TimeoutAttemptID != attempt.ID || row.Attempts != 0 {
		t.Fatalf("timeout hold not durable: %+v", row)
	}
	// A newly constructed factory-only runtime needs no expired worker context.
	mustProgress(t, RecoverImplementTimeoutPark(t.Context(), f.opts, row))
	parked := implementWorkerRow(t, f)
	if parked.State != store.ImplementParked || parked.TimeoutCommentID == "" || forge.attemptCount() != 1 {
		t.Fatalf("notice recovery lost held work: %+v", parked)
	}
}

func TestGraphParentDeadlineKeepsCleanupFailureWhenHookCannotSettle(t *testing.T) {
	f, forge, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("timeout settlement launched a provider")
		return nil, nil
	}))
	seedImplementStory(t, f)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	const diagnostic = "Process evidence unavailable. Cleanup collection failed: collector deadline exceeded."
	handler := &graphTestHandler{
		results: map[string][]ActivityResult{"work": {{Diagnostic: diagnostic}}},
		errs:    map[string]error{"work": errors.Join(ErrAgentTimedOut, context.DeadlineExceeded)},
		hook: func(ctx context.Context, _ Options, request ActivityRequest) {
			<-ctx.Done()
			// The stop was durable before bounded collection exhausted its allowance.
			mustProgress(t, f.db.RecordAttemptTimeout(t.Context(), store.AttemptTimeout{NodeAttemptID: request.NodeAttemptID, Budget: 91 * time.Second, TimedOutAt: time.Now()}))
		},
	}
	req := f.graphRequest(graphTestDOT, handler)
	req.StopOnFailure = true
	result, err := ExecuteNodeAction(ctx, f.opts, req)
	if !errors.Is(err, ErrAgentTimedOut) || strings.Contains(err.Error(), "complete graph activity") {
		t.Fatalf("timeout failed local settlement: %+v %v", result, err)
	}
	row := implementWorkerRow(t, f)
	if strings.Contains(row.LastFailure, diagnostic) {
		t.Fatal("fixture did not leave short atomic stop evidence")
	}
	mustProgress(t, RecoverImplementTimeoutPark(t.Context(), f.opts, row))
	if !strings.Contains(forge.only(t).body, "collector deadline exceeded") {
		t.Fatalf("notice lost cleanup evidence: %+v", forge.only(t))
	}
}

func TestGraphTimeoutSettlementRestoresMissingStopBeforeRetry(t *testing.T) {
	f, _, _ := assembledImplementFixture(t, lifecycleMCPHost(func(context.Context, agent.Request) (*agent.RunResult, error) {
		t.Fatal("settlement invoked provider")
		return nil, nil
	}))
	seedImplementStory(t, f)
	const allowance = 91 * time.Second
	deadline := time.Now().UTC().Add(-time.Second)
	handler := &graphTestHandler{
		results: map[string][]ActivityResult{"work": {{cleanupDeadline: deadline, Diagnostic: "Timeout evidence collection unavailable."}}},
		errs:    map[string]error{"work": ErrAgentTimedOut},
		hook: func(ctx context.Context, _ Options, request ActivityRequest) {
			_, err := f.db.EnsureAttemptTiming(ctx, store.AttemptTiming{NodeAttemptID: request.NodeAttemptID, StartedAt: deadline.Add(-allowance), Deadline: deadline, Allowance: allowance, Source: "node"})
			mustProgress(t, err)
		},
	}
	req := f.graphRequest(graphTestDOT, handler)
	req.StopOnFailure = true
	result, err := ExecuteNodeAction(t.Context(), f.opts, req)
	if !errors.Is(err, ErrAgentTimedOut) || strings.Contains(err.Error(), "complete graph activity") {
		t.Fatalf("settlement lost expiry: %+v %v", result, err)
	}
	receipt, err := f.db.GetAttemptTimeout(t.Context(), result.AttemptID)
	mustProgress(t, err)
	row := implementWorkerRow(t, f)
	if receipt.Budget != allowance || row.TimeoutAttemptID != result.AttemptID || row.Attempts != 0 {
		t.Fatalf("missing receipt enabled retry: %+v %+v", receipt, row)
	}
	workers, err := f.db.ListImplementStoriesNeedingWorker(t.Context())
	mustProgress(t, err)
	for _, worker := range workers {
		if worker.WorkerSession == row.WorkerSession {
			t.Fatal("timed-out worker became runnable")
		}
	}
}
