package teamsource

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// spikeAssets is the one embedded copy of the Spike graph, its investigator role
// and its result skill. The graph file is the sole definition of the flow.
//
//go:embed spike
var spikeAssets embed.FS

// CaptureSpike captures the built-in Spike graph and its bounded role/skill
// closure against the factory-owned Spike registry. No source path, team or
// owner override is taken.
func CaptureSpike(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(spikeAssets, "spike")
	if err != nil {
		return nil, fmt.Errorf("open built-in Spike: %w", err)
	}
	return CaptureWorkflow(ctx, source, "spike.dot", "go-development", agent.ModeSpike, registry)
}
