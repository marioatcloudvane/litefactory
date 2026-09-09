package agent_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestUsageEventClone_AllCountersOwned(t *testing.T) {
	t.Parallel()
	n := int64(9)
	source := agent.UsageEvent{Observation: &agent.UsageObservation{Tokens: agent.TokenUsage{InputUncached: &n, CacheRead: &n, CacheWrite: &n, CacheWrite5m: &n, CacheWrite1h: &n, Output: &n, Reasoning: &n}}}
	clone := source.Clone()
	counters := []*int64{clone.Observation.Tokens.InputUncached, clone.Observation.Tokens.CacheRead, clone.Observation.Tokens.CacheWrite, clone.Observation.Tokens.CacheWrite5m, clone.Observation.Tokens.CacheWrite1h, clone.Observation.Tokens.Output, clone.Observation.Tokens.Reasoning}
	for _, p := range counters {
		*p = 1
	}
	if n != 9 {
		t.Fatal("cloned counter changed source")
	}
	if !reflect.DeepEqual((agent.UsageEvent{}).Clone(), agent.UsageEvent{}) {
		t.Fatal("clone invented optional payloads")
	}
}

func TestUsageCapture_TransfersInvocationAndCoverageOwnership(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"invocation", "coverage"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			coverage := agent.UsageCoverage{Status: agent.CollectionPartial, Diagnostics: []string{"missing_child"}}
			event := agent.UsageEvent{Coverage: &coverage}
			if kind == "invocation" {
				event = agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: "a", Coverage: coverage}}
			}
			var received agent.UsageEvent
			capture := agent.UsageCapture{AttemptID: "a", Sink: func(_ context.Context, e agent.UsageEvent) error { received = e; return nil }}
			if err := capture.Emit(t.Context(), event); err != nil {
				t.Fatal(err)
			}
			coverage.Diagnostics[0] = "changed"
			got := received.Coverage
			if received.Invocation != nil {
				got = &received.Invocation.Coverage
			}
			if got.Status != agent.CollectionPartial || got.Diagnostics[0] != "missing_child" {
				t.Fatalf("consumer aliased source: %+v", got)
			}
		})
	}
}

func TestUsageCapture_CancellationAndAmbiguity(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"cancelled", "ambiguous"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			event := agent.UsageEvent{Coverage: &agent.UsageCoverage{Status: agent.CollectionPartial}}
			if name == "cancelled" {
				cancel()
			} else {
				event.Invocation = &agent.UsageInvocation{AttemptID: "a"}
			}
			var received *agent.UsageEvent
			capture := agent.UsageCapture{AttemptID: "a", Sink: func(_ context.Context, e agent.UsageEvent) error { received = &e; return nil }}
			err := capture.Emit(ctx, event)
			if err == nil || received != nil {
				t.Fatalf("rejected event reached consumer: %v %+v", err, received)
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}
