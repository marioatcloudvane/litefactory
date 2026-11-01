package agent

import (
	"context"
	"errors"
	"testing"
)

func TestUsageCaptureOwnsEventsAndPreservesUnknownCounters(t *testing.T) {
	t.Parallel()
	zero := int64(0)
	model := "observed"
	source := UsageObservation{ActualModel: &model, Tokens: TokenUsage{Output: &zero}, Coverage: UsageCoverage{Diagnostics: []string{"partial"}}}
	capture := UsageCapture{Sink: func(_ context.Context, e UsageEvent) error {
		if e.Observation.Tokens.Output == nil || *e.Observation.Tokens.Output != 0 || e.Observation.Tokens.InputUncached != nil {
			t.Fatal("unknown and measured zero collapsed")
		}
		*e.Observation.ActualModel = "changed"
		*e.Observation.Tokens.Output = 9
		e.Observation.Coverage.Diagnostics[0] = "changed"
		return nil
	}}
	if err := capture.Emit(t.Context(), UsageEvent{Observation: &source}); err != nil {
		t.Fatal(err)
	}
	if model != "observed" || zero != 0 || source.Coverage.Diagnostics[0] != "partial" {
		t.Fatal("sink mutated producer evidence")
	}
}

func TestUsageCaptureValidatesPayloadAndRetainsSinkFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("sink failed")
	calls := 0
	capture := UsageCapture{Sink: func(context.Context, UsageEvent) error { calls++; return failure }}
	for _, event := range []UsageEvent{{}, {Invocation: &UsageInvocation{}, Coverage: &UsageCoverage{}}} {
		if err := capture.Emit(t.Context(), event); err == nil {
			t.Fatal("ambiguous payload accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid evidence reached sink")
	}
	if err := capture.Emit(t.Context(), UsageEvent{Coverage: &UsageCoverage{}}); !errors.Is(err, failure) {
		t.Fatalf("lost sink error %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := capture.Emit(ctx, UsageEvent{Coverage: &UsageCoverage{}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation %v", err)
	}
}
