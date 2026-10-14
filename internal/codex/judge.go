package codex

import (
	"context"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

// RunJudge refuses before launch: the supported Codex transport cannot disable
// every native execution tool. Read-only filesystem access does not establish
// an evidence-only ceiling, and repair certification does not grant this scope.
func (r *Runner) RunJudge(ctx context.Context, request agent.JudgeRequest) (agent.JudgeRunResult, error) {
	if err := request.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err := config.ValidateThinking(config.KindCodex, request.Invocation.Model, request.Thinking); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	return agent.JudgeRunResult{}, agent.ErrJudgeIsolationUnverified
}
