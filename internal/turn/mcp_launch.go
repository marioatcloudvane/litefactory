package turn

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/submission"
)

// BuildMCPLaunchSpec prepares data only after the existing factory launch has
// been bound. The
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
	reply, err := (submission.Client{DescriptorPath: path}).List(ctx)
	if err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New("MCP installed catalog unavailable")
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(reply.Result, &catalog); err != nil {
		return nil, err
	}
	if len(catalog.Tools) == 0 {
		return nil, errors.New("MCP installed catalog is empty")
	}
	names := make([]string, len(catalog.Tools))
	for i, tool := range catalog.Tools {
		names[i] = tool.Name
	}
	if err := guard.Check(ctx); err != nil {
		return nil, err
	}
	return agent.NewMCPLaunchSpec(executable, path, attempt, names...)
}
