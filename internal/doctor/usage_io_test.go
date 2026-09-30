package doctor

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
)

type observedUsageRunner struct {
	retainedResultRunner
	attempt string
}

func (r *observedUsageRunner) Run(ctx context.Context, in Invocation, results *ResultService) (InvocationOutcome, error) {
	r.attempt = in.MeterAttempt.ID
	if in.Usage == nil {
		r.t.Fatal("missing shared meter capture")
	}
	coverage := agent.UsageCoverage{Status: agent.CollectionPartial, SourceFormat: "fixture-stream", Version: "fixture-version", Diagnostics: []string{"test_only"}}
	event := agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: r.attempt, ID: "root", SourceID: "fixture", SourceInvocationID: "root", Coverage: coverage}}
	if err := in.Usage.Emit(ctx, event); err != nil {
		return InvocationOutcome{}, err
	}
	model := "observed-model"
	input, output := int64(5), int64(2)
	observation := agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: r.attempt, SourceID: "fixture", EventKey: "message-1", InvocationID: "root", ActualModel: &model, ModelProvenance: "message", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &input, Output: &output}, Coverage: coverage}}
	// Stream replay must retain one measurement in the existing ledger.
	for range 2 {
		if err := in.Usage.Emit(ctx, observation); err != nil {
			return InvocationOutcome{}, err
		}
	}
	outcome, err := r.retainedResultRunner.Run(ctx, in, results)
	outcome.ObservedModel = model
	outcome.Usage = &coverage
	return outcome, err
}

func TestDoctorUsageSharesLedgerWithoutConfiguredModelSubstitution(t *testing.T) {
	service := doctorIOService(t)
	runner := &observedUsageRunner{retainedResultRunner: retainedResultRunner{t: t}}
	service.Runner = runner
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "Explain", SourceID: "usage-source"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts[0].ObservedModel != "observed-model" || result.Attempts[0].ConfiguredModel == "observed-model" {
		t.Fatalf("model attribution: %+v", result.Attempts)
	}
	snapshot, err := service.DB.NewMeterSnapshot(t.Context(), store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snapshot.Close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snapshot.Next(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	measurements := 0
	for _, row := range rows {
		if row.AttemptID != runner.attempt {
			t.Fatal("unexpected attempt")
		}
		if row.Tokens.InputUncached != nil {
			measurements++
			if *row.Tokens.InputUncached != 5 || row.ActualModel == nil || *row.ActualModel != "observed-model" || row.RunID != 0 || row.ExecutionPath != "doctor" || row.CollectionStatus != "partial" {
				t.Fatalf("incorrect shared usage: %+v", row)
			}
		}
	}
	if measurements != 1 {
		t.Fatalf("measurements: %d rows=%+v", measurements, rows)
	}
}
