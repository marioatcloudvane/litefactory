package turn

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/testutil"
	"reflect"
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
