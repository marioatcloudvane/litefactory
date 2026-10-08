package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
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
