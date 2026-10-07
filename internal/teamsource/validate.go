package teamsource

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// validateAssets is the one embedded copy of the Validate graph, its four roles
// (one write-access repair agent and three read-only reviewers) and its two
// result skills. The graph file is the sole definition of the flow.
//
//go:embed validate
var validateAssets embed.FS

// CaptureValidate captures the built-in Validate graph and its bounded
// role/skill closure against the factory-owned Validate registry. No source
// path, team or owner override is taken.
func CaptureValidate(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(validateAssets, "validate")
	if err != nil {
		return nil, fmt.Errorf("open built-in Validate: %w", err)
	}
	return CaptureWorkflow(ctx, source, "validate.dot", "builtin", agent.ModeValidate, registry)
}
