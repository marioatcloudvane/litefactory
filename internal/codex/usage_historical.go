package codex

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	native "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
	"strings"
)

// HistoricalWindow retains the original finalized attempt's time interval.
type HistoricalWindow = native.HistoricalWindow

// HistoricalUsage attaches existing factory attribution to neutral source evidence.
type HistoricalUsage struct {
	Invocations  []agent.UsageInvocation
	Observations []agent.UsageObservation
	Diagnostics  []string
	source       native.HistoricalUsage
}

// CollectHistoricalUsage preserves attempt identity while delegating native reads.
func CollectHistoricalUsage(ctx context.Context, home, attempt, rootID string, window HistoricalWindow) (HistoricalUsage, error) {
	if attempt == "" || len(attempt) > 256 || strings.ContainsAny(attempt, "\x00\n\r\t") {
		return HistoricalUsage{}, errors.New("invalid historical usage scope")
	}
	source, err := native.CollectHistoricalUsage(ctx, home, rootID, window)
	result := HistoricalUsage{Diagnostics: source.Diagnostics, source: source}
	for _, n := range source.Invocations {
		result.Invocations = append(result.Invocations, agent.UsageInvocation{AttemptID: attempt, ID: n.ID, ParentID: n.ParentID, SourceID: n.SourceID, SourceInvocationID: n.SourceInvocationID, Coverage: n.Coverage})
	}
	for _, n := range source.Observations {
		result.Observations = append(result.Observations, agent.UsageObservation{AttemptID: attempt, SourceID: n.SourceID, EventKey: n.EventKey, InvocationID: n.InvocationID, ActualModel: n.ActualModel, ModelProvenance: n.ModelProvenance, Mode: n.Mode, Scope: n.Scope, CounterKey: n.CounterKey, CounterEpoch: n.CounterEpoch, Tokens: n.Tokens, Coverage: n.Coverage})
	}
	return result, err
}

// Revalidate checks that the neutral source files still match the collected evidence.
func (h HistoricalUsage) Revalidate(ctx context.Context) error { return h.source.Revalidate(ctx) }
