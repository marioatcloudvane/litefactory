package agent

import (
	"context"
	"errors"
	"testing"
)

func TestUsageOwnership(t *testing.T) {
	t.Parallel()
	n := int64(7)
	model := "actual"
	original := UsageObservation{AttemptID: "attempt", ActualModel: &model, Tokens: TokenUsage{InputUncached: &n}, Coverage: UsageCoverage{Diagnostics: []string{"partial"}}}
	capture := UsageCapture{AttemptID: "attempt", Sink: func(_ context.Context, e UsageEvent) error {
		*e.Observation.ActualModel = "changed"
		*e.Observation.Tokens.InputUncached = 99
		e.Observation.Coverage.Diagnostics[0] = "changed"
		return nil
	}}
	if err := capture.Emit(t.Context(), UsageEvent{Observation: &original}); err != nil {
		t.Fatal(err)
	}
	if model != "actual" || n != 7 || original.Coverage.Diagnostics[0] != "partial" {
		t.Fatal("sink mutated producer")
	}
	r := &RunResult{Usage: &original.Coverage}
	clone := r.Clone()
	clone.Usage.Diagnostics[0] = "report"
	if r.Usage.Diagnostics[0] != "partial" {
		t.Fatal("report clone aliases diagnostics")
	}
}

func TestUsageCaptureRejectsWrongAttemptAndPropagatesFailure(t *testing.T) {
	t.Parallel()
	sinkErr := errors.New("persistence failed")
	calls := 0
	capture := UsageCapture{AttemptID: "a", Sink: func(_ context.Context, _ UsageEvent) error { calls++; return sinkErr }}
	if err := capture.Emit(t.Context(), UsageEvent{Invocation: &UsageInvocation{AttemptID: "b"}}); err == nil {
		t.Fatal("accepted foreign attempt")
	}
	if err := capture.Emit(t.Context(), UsageEvent{}); err == nil {
		t.Fatal("accepted empty payload")
	}
	if calls != 0 {
		t.Fatal("invalid payload reached sink")
	}
	if err := capture.Emit(t.Context(), UsageEvent{Coverage: &UsageCoverage{}}); !errors.Is(err, sinkErr) {
		t.Fatalf("lost error: %v", err)
	}
}
