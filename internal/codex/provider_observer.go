package codex

import (
	"context"
	"github.com/cloudvane/litefactory/internal/agent"
)

// SetProviderObserver installs a terminal observation sink without changing retries.
func (r *Runner) SetProviderObserver(observer func(context.Context, agent.Request, *agent.RunResult, error)) {
	r.providerObserver.Set(observer)
}
