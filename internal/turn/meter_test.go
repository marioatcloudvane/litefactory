package turn

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestMeterPersistsDistinctFallbackAttempts(t *testing.T) {
	var f *fixture
	var ids []string
	runner := newStubRunner(func(ctx context.Context, req harness.Request) (*harness.RunResult, error) {
		if req.Usage == nil {
			t.Fatal("missing prelaunch capture")
		}
		ids = append(ids, req.Usage.AttemptID)
		coverage := agent.UsageCoverage{Status: agent.CollectionPartial, Diagnostics: []string{"missing_evidence"}}
		v := agent.UsageInvocation{AttemptID: req.Usage.AttemptID, ID: "root", SourceID: "s", SourceInvocationID: "root", Coverage: coverage}
		if err := req.Usage.Emit(ctx, agent.UsageEvent{Invocation: &v}); err != nil {
			t.Fatal(err)
		}
		tokens := int64(7)
		model := "test-model"
		o := agent.UsageObservation{AttemptID: req.Usage.AttemptID, SourceID: "s", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &tokens}, Coverage: coverage}
		for i := 0; i < 2; i++ {
			if err := req.Usage.Emit(ctx, agent.UsageEvent{Observation: &o}); err != nil {
				t.Fatal(err)
			}
		}
		result := succeeded(req, f.paths.Home())
		result.Usage = &coverage
		if len(ids) == 1 {
			return result, agent.ErrSessionNotResumable
		}
		return result, nil
	})
	f = newFixture(t, runner)
	result := f.execute(f.request())
	if !result.Succeeded() {
		t.Fatalf("outcome changed %+v", result)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatal(ids)
	}
	firstAttempt, err := f.db.GetMeterAttempt(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if firstAttempt.Outcome != "failure" {
		t.Fatal("resume refusal recorded as success")
	}
	snap, err := f.db.NewMeterSnapshot(t.Context(), store.MeterFilter{RunID: result.Run.ID}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snap.Next(t.Context(), 256)
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want two attempts got %+v", rows)
	}
	for _, row := range rows {
		if row.Tokens.Output == nil || *row.Tokens.Output != 7 {
			t.Fatal("replay duplicated or lost usage")
		}
		if row.CollectionStatus == "complete" {
			t.Fatal("partial promoted to complete")
		}
	}
}
