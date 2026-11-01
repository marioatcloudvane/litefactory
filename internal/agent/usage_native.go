package agent

import (
	"context"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// NativeCapture binds provider observations to the existing factory attempt.
// Identity validation and persistence remain in UsageCapture.Emit.
func (c *UsageCapture) NativeCapture() *execution.UsageCapture {
	if c == nil {
		return nil
	}
	capture := *c
	return &execution.UsageCapture{EvidenceContext: EvidenceContext, Sink: func(ctx context.Context, e execution.UsageEvent) error {
		out := UsageEvent{Coverage: e.Coverage}
		if n := e.Invocation; n != nil {
			out.Invocation = &UsageInvocation{AttemptID: capture.AttemptID, ID: n.ID, ParentID: n.ParentID, SourceID: n.SourceID, SourceInvocationID: n.SourceInvocationID, Coverage: n.Coverage}
		}
		if n := e.Observation; n != nil {
			out.Observation = &UsageObservation{AttemptID: capture.AttemptID, SourceID: n.SourceID, EventKey: n.EventKey, InvocationID: n.InvocationID, ActualModel: n.ActualModel, ModelProvenance: n.ModelProvenance, Mode: n.Mode, Scope: n.Scope, CounterKey: n.CounterKey, CounterEpoch: n.CounterEpoch, Tokens: n.Tokens, Coverage: n.Coverage}
		}
		return capture.Emit(ctx, out)
	}}
}
