package agent

import (
	"context"
	"errors"
	"testing"

	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

func TestNativeCaptureRetainsFactoryAttributionAndOwnership(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("persist failed")
	var got UsageEvent
	capture := UsageCapture{AttemptID: "reserved-attempt", Sink: func(_ context.Context, e UsageEvent) error { got = e; return sentinel }}
	native := capture.NativeCapture()
	capture.AttemptID = "changed"
	capture.Sink = nil
	zero := int64(0)
	observation := execution.UsageObservation{SourceID: "session", EventKey: "response", InvocationID: "root", Tokens: execution.TokenUsage{Output: &zero}, Coverage: execution.UsageCoverage{Diagnostics: []string{"partial"}}}
	if err := native.Emit(t.Context(), execution.UsageEvent{Observation: &observation}); !errors.Is(err, sentinel) {
		t.Fatalf("sink failure lost: %v", err)
	}
	if got.Observation == nil || got.Observation.AttemptID != "reserved-attempt" || got.Observation.Tokens.Output == nil || *got.Observation.Tokens.Output != 0 || got.Observation.Tokens.InputUncached != nil {
		t.Fatalf("factory translation %+v", got)
	}
	*got.Observation.Tokens.Output = 9
	got.Observation.Coverage.Diagnostics[0] = "changed"
	if zero != 0 || observation.Coverage.Diagnostics[0] != "partial" {
		t.Fatal("factory sink aliases neutral producer")
	}
}
