package harness

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
)

// PreventionIsolation describes the standalone broker-only launch controls.
func (r *Runner) PreventionIsolation(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "prevention-broker-only-v1: no native tools; strict MCP; private credential home; isolated settings; one bounded print invocation", nil
}

// RunPrevention never falls back to a workflow or repair runner.
func (r *Runner) RunPrevention(ctx context.Context, request agent.PreventionRequest) (agent.DoctorRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.DoctorRunResult{}, err
	}
	request.Invocation.TurnLimit = request.MaxTurns
	return r.runScopedDoctorTransport(ctx, request.Invocation, "prevention")
}
