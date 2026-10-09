package teamsource

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// implementAssets is the one embedded copy of the Implement graph, its story
// worker role and its result skill. The graph file is the sole definition of the
// flow.
//
//go:embed implement
var implementAssets embed.FS

// CaptureImplement captures the built-in Implement graph and its bounded
// role/skill closure against the factory-owned Implement registry. No source
// path, team or owner override is taken.
func CaptureImplement(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(implementAssets, "implement")
	if err != nil {
		return nil, fmt.Errorf("open built-in Implement: %w", err)
	}
	return CaptureWorkflow(ctx, source, "implement.dot", "go-development", agent.ModeImplement, registry)
}
