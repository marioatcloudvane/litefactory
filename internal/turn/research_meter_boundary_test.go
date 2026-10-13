package turn

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func researchMeterFixture(t *testing.T) (*fixture, agent.Request) {
	t.Helper()
	f := newFixture(t, nil)
	seg, err := f.db.GetOrCreateSegment(t.Context(), f.session.ID, 1, "research", store.AgentClaude)
	mustProgress(t, err)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: f.session.ID, SegmentID: seg.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 22, LogPath: func(int) string { return "research.log" }})
	mustProgress(t, err)
	at := time.Now().UTC()
	def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("b", 64), CreatedAt: at})
	mustProgress(t, err)
	inst, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, Status: store.GraphRunning, CurrentNode: "work", StartedAt: at, NextDueAt: at})
	mustProgress(t, err)
	attempt, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: inst.ID, Node: "work", RunID: run.ID, Limit: 3, StartedAt: at})
	mustProgress(t, err)
	ref, err := naming.ParseRef(f.session.TaskRef)
	mustProgress(t, err)
	return f, agent.Request{Delivery: fixtureNativeDelivery(t, ResearchWorkflowPolicy()), Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: seg.Ordinal, Session: agent.SessionRef{Ref: seg.SessionRef}, Mode: "research", WorktreePath: t.TempDir(), Prompt: "Investigate", Invocation: agent.InvocationResearchNode, Graph: &agent.GraphInvocation{InstanceID: inst.ID, AttemptID: attempt.ID, RunID: run.ID, Node: "work"}}
}

func researchMeterRows(t *testing.T, db *store.DB, run int64) []store.MeterRow {
	t.Helper()
	snap, err := db.NewMeterSnapshot(t.Context(), store.MeterFilter{RunID: run}, store.MeterSnapshotOptions{})
	mustProgress(t, err)
	rows, err := snap.Next(t.Context(), 256)
	mustProgress(t, err)
	mustProgress(t, snap.Close(t.Context()))
	return rows
}

func TestResearchMeterBindingRejectsForeignAndTerminalAttempts(t *testing.T) {
	f, req := researchMeterFixture(t)
	g := req.Graph
	for _, tc := range []struct {
		name string
		run  int64
		kind string
		link store.MeterGraphLink
	}{
		{"instance", g.RunID, "claude", store.MeterGraphLink{InstanceID: g.InstanceID + 1, AttemptID: g.AttemptID, Node: g.Node}},
		{"node", g.RunID, "claude", store.MeterGraphLink{InstanceID: g.InstanceID, AttemptID: g.AttemptID, Node: "foreign"}},
		{"run", g.RunID + 1, "claude", store.MeterGraphLink{InstanceID: g.InstanceID, AttemptID: g.AttemptID, Node: g.Node}},
		{"provider", g.RunID, "codex", store.MeterGraphLink{InstanceID: g.InstanceID, AttemptID: g.AttemptID, Node: g.Node}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.db.StartGraphMeterAttempt(t.Context(), tc.run, tc.kind, tc.link); err == nil {
				t.Fatal("accepted forged binding")
			}
		})
	}
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), g.AttemptID, store.GraphCompletion{Status: store.GraphFailed, FinishedAt: time.Now().UTC(), InstanceStatus: store.GraphRunning}))
	if _, err := f.db.StartGraphMeterAttempt(t.Context(), g.RunID, "claude", store.MeterGraphLink{InstanceID: g.InstanceID, AttemptID: g.AttemptID, Node: g.Node}); err == nil {
		t.Fatal("accepted terminal attempt")
	}
	if rows := researchMeterRows(t, f.db, g.RunID); len(rows) != 0 {
		t.Fatalf("rejected bindings created usage: %+v", rows)
	}
}

func TestResearchMeterFailedLaunchUnknownAndNoRetry(t *testing.T) {
	f, req := researchMeterFixture(t)
	launchErr := errors.New("fixture launch failed")
	runner := newStubRunner(func(context.Context, agent.Request) (*agent.RunResult, error) { return nil, launchErr })
	_, err := RunResearchMetered(t.Context(), f.db, agent.Kind(store.AgentClaude), req, runner)
	if !errors.Is(err, launchErr) || runner.callCount() != 1 {
		t.Fatalf("err=%v calls=%d", err, runner.callCount())
	}
	rows := researchMeterRows(t, f.db, req.Graph.RunID)
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	r := rows[0]
	if r.Outcome != "failure" || r.CollectionStatus != "unknown" || r.RowKind != "coverage" || r.Tokens.Output != nil || r.EstimatedUSD != nil || r.ExecutionPath != "graph" || r.GraphAttemptID == nil || *r.GraphAttemptID != req.Graph.AttemptID || r.AttemptFinishedAt == "" {
		t.Fatalf("failed launch attribution=%+v", r)
	}
	if r.AgentVersion != "" || r.SourceInvocationID != "" {
		t.Fatal("invented provider metadata")
	}
}

func TestResearchMeterResumeFallbackAndMultiNodeTotals(t *testing.T) {
	f, req := researchMeterFixture(t)
	var ids []string
	var paths []agent.RunPath
	runner := newStubRunner(func(ctx context.Context, r agent.Request) (*agent.RunResult, error) {
		ids = append(ids, r.Usage.AttemptID)
		paths = append(paths, r.Path)
		cov := agent.UsageCoverage{Status: agent.CollectionPartial, Version: "fixture-cli-v1", SourceFormat: "fixture-events-v1", Diagnostics: []string{"missing_evidence"}}
		v := agent.UsageInvocation{AttemptID: r.Usage.AttemptID, ID: "root", SourceID: "fixture", SourceInvocationID: "provider-root", Coverage: cov}
		mustProgress(t, r.Usage.Emit(ctx, agent.UsageEvent{Invocation: &v}))
		n := int64(len(ids) * 3)
		model := "fixture-model"
		o := agent.UsageObservation{AttemptID: r.Usage.AttemptID, SourceID: "fixture", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &n}, Coverage: cov}
		for range 2 {
			mustProgress(t, r.Usage.Emit(ctx, agent.UsageEvent{Observation: &o}))
		}
		result := &agent.RunResult{Outcome: agent.OutcomeSucceeded, Usage: &cov}
		if len(ids) == 2 {
			return result, agent.ErrSessionNotResumable
		}
		return result, nil
	})
	req.Path = agent.PathResume
	req.Session.Resume = "compatible-token"
	_, err := RunResearchMetered(t.Context(), f.db, agent.Kind(store.AgentClaude), req, runner)
	mustProgress(t, err)
	_, err = RunResearchMetered(t.Context(), f.db, agent.Kind(store.AgentClaude), req, runner)
	if !errors.Is(err, agent.ErrSessionNotResumable) || runner.callCount() != 2 {
		t.Fatalf("refusal auto retried: %v %d", err, runner.callCount())
	}
	req.Path = agent.PathColdAfterResumeRefused
	req.Session.Resume = ""
	_, err = RunResearchMetered(t.Context(), f.db, agent.Kind(store.AgentClaude), req, runner)
	mustProgress(t, err)
	old := *req.Graph
	at := time.Now().UTC()
	mustProgress(t, f.db.CompleteGraphNodeAttempt(t.Context(), old.AttemptID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: at, InstanceStatus: store.GraphRunning, Transition: &store.GraphTransition{InstanceID: old.InstanceID, FromNode: "work", ToNode: "next", OnOutcome: "success", At: at}}))
	next, err := f.db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: old.InstanceID, Node: "next", RunID: old.RunID, Limit: 3, StartedAt: at})
	mustProgress(t, err)
	req.Graph = &agent.GraphInvocation{InstanceID: old.InstanceID, AttemptID: next.ID, RunID: old.RunID, Node: "next"}
	req.Path = agent.PathCold
	_, err = RunResearchMetered(t.Context(), f.db, agent.Kind(store.AgentClaude), req, runner)
	mustProgress(t, err)
	rows := researchMeterRows(t, f.db, old.RunID)
	if len(rows) != 4 || runner.callCount() != 4 {
		t.Fatalf("rows=%+v calls=%d", rows, runner.callCount())
	}
	seen := map[string]bool{}
	var total int64
	for _, r := range rows {
		if seen[r.AttemptID] {
			t.Fatal("duplicate invocation")
		}
		seen[r.AttemptID] = true
		if r.Tokens.Output == nil {
			t.Fatal("missing output")
		}
		total += *r.Tokens.Output
		if r.ExecutionPath != "graph" || r.RunID != old.RunID || r.GraphInstanceID == nil || *r.GraphInstanceID != old.InstanceID || r.AgentVersion != "fixture-cli-v1" || r.SourceFormat != "fixture-events-v1" || r.SourceInvocationID != "provider-root" || r.AttemptFinishedAt == "" || r.Tokens.InputUncached != nil {
			t.Fatalf("lost attribution=%+v", r)
		}
		if r.AttemptID == ids[1] && r.Outcome != "failure" {
			t.Fatal("refused resume recorded success")
		}
		if r.AttemptID != ids[1] && r.Outcome != "success" {
			t.Fatal("successful invocation not retained")
		}
		wantNode, wantAttempt := "work", old.AttemptID
		if r.AttemptID == ids[3] {
			wantNode, wantAttempt = "next", next.ID
		}
		if r.GraphNode != wantNode || r.GraphAttemptID == nil || *r.GraphAttemptID != wantAttempt {
			t.Fatal("usage copied between nodes")
		}
	}
	if total != 30 || len(seen) != 4 || paths[0] != agent.PathResume || paths[2] != agent.PathColdAfterResumeRefused {
		t.Fatalf("total=%d ids=%v paths=%v", total, ids, paths)
	}
}

// These fixtures exercise the real activity, result service, graph settlement
// and metering without a provider or wall-clock backoff.
func TestCapacityInvocation(t *testing.T) {
	for _, tc := range []struct {
		name                                                                    string
		successAt, acceptAt, cancelWait, cancelProcess                          int
		ownerStop, budgetWait, resumeRefused, readError, nontransient, baseline bool
		calls                                                                   int
	}{
		{name: "exhausted", calls: 4},
		{name: "earlier attempt baseline", baseline: true, calls: 4},
		{name: "success", successAt: 2, calls: 2},
		{name: "nontransient", nontransient: true, calls: 2},
		{name: "accepted before exit", acceptAt: 1, calls: 1},
		{name: "accepted during wait", acceptAt: 2, calls: 1},
		{name: "read failure", readError: true, calls: 1},
		{name: "cancel wait 1", cancelWait: 1, calls: 1},
		{name: "cancel wait 2", cancelWait: 2, calls: 2},
		{name: "cancel wait 3", cancelWait: 3, calls: 3},
		{name: "cancel process 1", cancelProcess: 1, calls: 1},
		{name: "cancel process 2", cancelProcess: 2, calls: 2},
		{name: "cancel process 3", cancelProcess: 3, calls: 3},
		{name: "cancel process 4", cancelProcess: 4, calls: 4},
		{name: "owner stop", ownerStop: true, calls: 1},
		{name: "budget expires during wait", budgetWait: true, calls: 1},
		{name: "resume refused after capacity", resumeRefused: true, calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.budgetWait {
				old := agentBudgetClock
				agentBudgetClock = func(time.Duration) time.Duration { return 250 * time.Millisecond }
				defer func() { agentBudgetClock = old }()
			}
			f := newRetainedFixture(t, "plan")
			if tc.baseline {
				f.submit(t, f.result)
				f.opts.Attempt = agentResultRun(t, f.db, f.session, "plan", "attempt-2", 2)
				var err error
				f.service, err = BindAgentResultService(t.Context(), f.opts)
				mustProgress(t, err)
			}
			at := time.Now().UTC()
			def, err := f.db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Mode: "plan", ProfileVersion: 1, Path: "plan.dot", Digest: strings.Repeat("e", 64), CreatedAt: at})
			mustProgress(t, err)
			inst, err := f.db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: f.session.ID, DefinitionID: def.ID, CurrentNode: "work", StartedAt: at})
			mustProgress(t, err)
			profile := &graphexec.Profile{Mode: f.policy.Mode, Team: "builtin", ProfileVersion: 1, Digest: def.Digest, Nodes: []graphexec.Node{{Name: "work", Action: ResearchAgentAction, Limit: 1, Attrs: map[string]string{graphexec.BudgetAttribute: "15"}}}}
			run, err := f.db.GetRun(t.Context(), f.opts.Attempt.RunID)
			mustProgress(t, err)
			seg, err := f.db.GetSegment(t.Context(), run.SegmentID)
			mustProgress(t, err)
			mustProgress(t, f.db.SetSegmentResumeToken(t.Context(), seg.ID, "original"))
			ref, err := naming.ParseRef(f.session.TaskRef)
			mustProgress(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			closed, prepared, calls := 0, 0, 0
			var waits []time.Duration
			var first agent.Request
			var deadline time.Time
			assertActive := func() {
				if closed != 0 {
					t.Fatal("helpers closed during invocation")
				}
				attempt, e := f.db.LatestGraphNodeAttempt(t.Context(), inst.ID, "work")
				mustProgress(t, e)
				if attempt.Status != store.GraphRunning {
					t.Fatalf("settled during retry: %+v", attempt)
				}
			}
			runner := newStubRunner(func(c context.Context, r agent.Request) (*agent.RunResult, error) {
				calls++
				assertActive()
				d, ok := c.Deadline()
				if !ok {
					t.Fatal("missing invocation budget")
				}
				cov := agent.UsageCoverage{Status: agent.CollectionPartial, Version: "fixture", SourceFormat: "fixture"}
				v := agent.UsageInvocation{AttemptID: r.Usage.AttemptID, ID: "root", SourceID: "fixture", SourceInvocationID: "provider-root", Coverage: cov}
				mustProgress(t, r.Usage.Emit(c, agent.UsageEvent{Invocation: &v}))
				n := int64(calls)
				o := agent.UsageObservation{AttemptID: r.Usage.AttemptID, SourceID: "fixture", EventKey: "tokens", InvocationID: "root", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &n}, Coverage: cov}
				mustProgress(t, r.Usage.Emit(c, agent.UsageEvent{Observation: &o}))
				r.Usage = nil // Every process has a distinct meter capture, checked below.
				if calls == 1 {
					first, deadline = r, d
				} else if !reflect.DeepEqual(first, r) || !deadline.Equal(d) {
					t.Fatal("retry changed request or deadline")
				}
				if tc.acceptAt == 1 || tc.successAt == calls {
					f.submit(t, f.result)
				}
				if tc.readError {
					f.service.binding.Mode = "invalid-mode"
				}
				if tc.cancelProcess == calls {
					cancel()
				}
				if tc.successAt == calls {
					return &agent.RunResult{Outcome: agent.OutcomeSucceeded}, nil
				}
				if tc.resumeRefused && calls == 2 {
					return &agent.RunResult{Outcome: agent.OutcomeFailed}, agent.ErrSessionNotResumable
				}
				if tc.nontransient && calls == 2 {
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "ordinary failure"}, nil
				}
				return &agent.RunResult{Outcome: agent.OutcomeFailed, Model: "fixture-model", Transient: &agent.TransientFailure{Cause: "model is at capacity", Model: "fixture-model"}, ResumeToken: "do-not-promote"}, nil
			})
			other := newStubRunner(func(context.Context, agent.Request) (*agent.RunResult, error) {
				t.Fatal("changed provider")
				return nil, nil
			})
			handler := AgentActivity{Policy: f.policy, WaitCapacity: func(c context.Context, d time.Duration) error {
				assertActive()
				waits = append(waits, d)
				if tc.ownerStop {
					_, e := f.db.RequestOwnerStop(t.Context(), f.session.TaskRef)
					mustProgress(t, e)
				}
				if tc.budgetWait {
					<-c.Done()
					return c.Err()
				}
				if tc.acceptAt == 2 {
					f.submit(t, f.result)
				}
				if tc.cancelWait == len(waits) {
					cancel()
				}
				return c.Err()
			}, Prepare: func(context.Context, Options, ActivityRequest) (ResearchInvocation, error) {
				prepared++
				r := agent.Request{Delivery: fixtureNativeDelivery(t, f.policy), Ref: ref, Mode: "plan", RunOrdinal: run.Ordinal, SegmentOrdinal: seg.Ordinal, Session: agent.SessionRef{Ref: seg.SessionRef, Resume: "original"}, Path: agent.PathResume, WorktreePath: t.TempDir(), Prompt: "Work", Permissions: agent.Permissions{Profile: "plan"}}
				cold := r
				cold.Prompt = "must not use cold fallback"
				return ResearchInvocation{Request: r, ColdRequest: &cold, Kind: "claude", Results: f.service, Close: func() error { closed++; return nil }}, nil
			}}
			var registry ActivityRegistry
			mustProgress(t, registry.Register(ResearchAgentAction, handler))
			runners := runnerSet(runner)
			runners["alternate"] = other
			out, runErr := ExecuteNodeAction(ctx, Options{DB: f.db, Runners: runners}, GraphNodeRequest{Profile: profile, Activities: registry, InstanceID: inst.ID, RunID: run.ID, At: at})
			if calls != tc.calls || prepared != 1 || closed != 1 {
				t.Fatalf("calls=%d prepared=%d closed=%d err=%v", calls, prepared, closed, runErr)
			}
			for i, d := range waits {
				if d != (30*time.Second)<<i {
					t.Fatalf("waits=%v", waits)
				}
			}
			if tc.acceptAt != 0 || tc.successAt != 0 {
				if out.Activity.Outcome != "success" {
					t.Fatalf("accepted result not routed: %+v %v", out, runErr)
				}
			}
			if tc.budgetWait && !errors.Is(runErr, ErrAgentTimedOut) {
				t.Fatalf("budget not enforced: %v", runErr)
			}
			if tc.name == "exhausted" {
				if len(waits) != 3 {
					t.Fatalf("waits=%v", waits)
				}
				if runErr == nil || !strings.Contains(runErr.Error(), "claude model fixture-model: model is at capacity; retried 3 times") {
					t.Fatalf("cause=%v", runErr)
				}
				rendered := renderPlanFailure(planFailureCandidate(Request{}, runErr), "", ref, "")
				if strings.Contains(rendered, "To retry, reply") {
					t.Fatal(rendered)
				}
			}
			rows := researchMeterRows(t, f.db, run.ID)
			if len(rows) != calls {
				t.Fatalf("meter rows=%d calls=%d", len(rows), calls)
			}
			seen := map[string]bool{}
			var tokens int64
			for _, row := range rows {
				if seen[row.AttemptID] || row.Tokens.Output == nil || row.GraphAttemptID == nil || *row.GraphAttemptID != first.Graph.AttemptID || row.RunID != run.ID {
					t.Fatalf("lost per-process usage or binding: %+v", row)
				}
				seen[row.AttemptID] = true
				tokens += *row.Tokens.Output
			}
			if tokens != int64(calls*(calls+1)/2) {
				t.Fatalf("lost retry tokens: %d", tokens)
			}
			attempts, e := f.db.ListGraphNodeAttempts(t.Context(), inst.ID)
			mustProgress(t, e)
			if len(attempts) != 1 {
				t.Fatalf("attempts=%+v", attempts)
			}
		})
	}
}
