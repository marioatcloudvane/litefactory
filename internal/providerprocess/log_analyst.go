package providerprocess

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	execution "github.com/cloudvane/litefactory/pkg/execution/agent"
)

// RunLogAnalystProcess validates the dedicated contract, then shares only bounded
// process I/O. Provider adapters remain responsible for isolation and catalog checks.
func RunLogAnalystProcess(ctx context.Context, binary string, args, env []string, request agent.LogAnalystRequest, timeout time.Duration, observe func([]byte)) error {
	if err := request.Validate(); err != nil {
		return err
	}
	return execution.RunProcess(ctx, binary, args, env, request.Prompt, request.WorkingDirectory, timeout, observe, nil, nil, false, nil)
}
