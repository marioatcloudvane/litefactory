package codex

import (
	"context"

	"github.com/cloudvane/litefactory/internal/agent"
)

// LogAnalystIsolation remains closed because native tool isolation is unverified.
func (r *Runner) LogAnalystIsolation(context.Context) (string, error) {
	return "", agent.ErrLogAnalystIsolation
}

// RunLogAnalyst refuses unsupported isolation without a broader runner fallback.
func (r *Runner) RunLogAnalyst(_ context.Context, request agent.LogAnalystRequest) (agent.LogAnalystRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.LogAnalystRunResult{}, err
	}
	return agent.LogAnalystRunResult{}, agent.ErrLogAnalystIsolation
}
