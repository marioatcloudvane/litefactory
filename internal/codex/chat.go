package codex

import (
	"context"

	"github.com/cloudvane/litefactory/internal/agent"
)

// RunChat preserves the existing closed doctor gate. No workflow execution can
// substitute for an unverified response-only provider path.
func (r *Runner) RunChat(ctx context.Context, request agent.ChatRequest) (agent.ChatResult, error) {
	if err := request.Validate(); err != nil {
		return agent.ChatResult{}, err
	}
	if _, err := r.DoctorIsolation(ctx); err != nil {
		return agent.ChatResult{}, err
	}
	return agent.ChatResult{}, agent.ErrDoctorIsolationUnverified
}
