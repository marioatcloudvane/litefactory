package meter_test

import (
	"errors"
	"math"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
)

func quantity(n int64) *int64 { return &n }

// These cases exercise accounting boundaries independently of provider formats.
func TestTokenAccountingBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		usage   agent.TokenUsage
		want    *int64
		invalid bool
	}{
		{"measured zero/is not unknown", agent.TokenUsage{InputUncached: quantity(0), CacheRead: quantity(0), CacheWrite: quantity(0), Output: quantity(0)}, quantity(0), false},
		{"missing output/total unknown", agent.TokenUsage{InputUncached: quantity(3), CacheRead: quantity(0), CacheWrite: quantity(0)}, nil, false},
		{"known subsets/do not invent missing parents", agent.TokenUsage{InputUncached: quantity(0), CacheRead: quantity(0), CacheWrite5m: quantity(3), Reasoning: quantity(2)}, nil, false},
		{"maximum total/accepted without subset addition", agent.TokenUsage{InputUncached: quantity(0), CacheRead: quantity(0), CacheWrite: quantity(0), Output: quantity(math.MaxInt64), Reasoning: quantity(math.MaxInt64)}, quantity(math.MaxInt64), false},
		{"total overflow/rejected", agent.TokenUsage{InputUncached: quantity(math.MaxInt64), CacheRead: quantity(1), CacheWrite: quantity(0), Output: quantity(0)}, nil, true},
		{"duration subsets overflow/rejected", agent.TokenUsage{CacheWrite: quantity(math.MaxInt64), CacheWrite5m: quantity(math.MaxInt64), CacheWrite1h: quantity(1)}, nil, true},
		{"reasoning exceeds output/rejected", agent.TokenUsage{Output: quantity(4), Reasoning: quantity(5)}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := meter.Total(tt.usage)
			if tt.invalid {
				if !errors.Is(err, meter.ErrInvalidUsage) {
					t.Fatalf("want invalid usage, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("total=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestReconcileRejectsBaselineFromDifferentScope(t *testing.T) {
	t.Parallel()
	model := "model-a"
	original := agent.UsageObservation{AttemptID: "a", SourceID: "source", EventKey: "e", InvocationID: "child", ActualModel: &model, ModelProvenance: "response", Mode: agent.CounterCumulative, Scope: agent.UsageExclusive, CounterKey: "tokens", CounterEpoch: "epoch", Tokens: agent.TokenUsage{Output: quantity(20)}}
	tests := []struct {
		name   string
		change func(*agent.UsageObservation)
	}{
		{"attempt", func(o *agent.UsageObservation) { o.AttemptID = "b" }},
		{"source", func(o *agent.UsageObservation) { o.SourceID = "other" }},
		{"child", func(o *agent.UsageObservation) { o.InvocationID = "sibling" }},
		{"model", func(o *agent.UsageObservation) { m := "model-b"; o.ActualModel = &m }},
		{"unknown model", func(o *agent.UsageObservation) { o.ActualModel = nil }},
		{"counter", func(o *agent.UsageObservation) { o.CounterKey = "other" }},
		{"epoch", func(o *agent.UsageObservation) { o.CounterEpoch = "new" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			baseline := &meter.CounterSnapshot{Identity: meter.CounterIdentity(original), Tokens: agent.TokenUsage{Output: quantity(10)}}
			changed := original.Clone()
			tt.change(&changed)
			got, err := meter.Reconcile(changed, baseline, "")
			if !errors.Is(err, meter.ErrInvalidUsage) || got.Additive || got.Next != nil {
				t.Fatalf("foreign baseline advanced state: %+v, %v", got, err)
			}
		})
	}
}

func TestDeltaSubsetCannotConsumePreviouslyUnclassifiedTokens(t *testing.T) {
	t.Parallel()
	// Both lifetime snapshots are valid, but an increased subset without any
	// corresponding parent increase cannot be billed as fresh consumption.
	current := agent.TokenUsage{Output: quantity(10), Reasoning: quantity(8)}
	baseline := agent.TokenUsage{Output: quantity(10), Reasoning: quantity(2)}
	_, err := meter.Delta(current, baseline)
	if !errors.Is(err, meter.ErrInvalidUsage) {
		t.Fatalf("invalid delta subset accepted: %v", err)
	}
}
