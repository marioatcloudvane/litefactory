package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

// BuildMCPLaunchSpec prepares data only after the existing factory launch has
// been bound. #264 owns production adoption; #274 owns host consumption. The
// setting alone never allocates authority, starts a process or modifies policy.
func BuildMCPLaunchSpec(ctx context.Context, enabled bool, executable string, broker *submission.Broker, guard *AttemptGuard) (*agent.MCPLaunchSpec, error) {
	if !enabled || guard == nil {
		return nil, nil
	}
	if broker == nil {
		return nil, errors.New("MCP registration requires its bound broker")
	}
	if err := guard.Check(ctx); err != nil {
		return nil, err
	}
	path, attempt, err := broker.LaunchReference()
	if err != nil {
		return nil, err
	}
	if attempt != guard.Binding().Attempt {
		return nil, errors.New("MCP descriptor and factory attempt do not match")
	}
	return agent.NewMCPLaunchSpec(executable, path, attempt)
}
