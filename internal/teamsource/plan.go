package teamsource

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

// planAssets holds the built-in Plan role, skill and graph definitions.
// Runtime never traverses repository source paths.
//
//go:embed plan
var planAssets embed.FS

// CapturePlan captures the built-in Plan graph and its bounded role/skill closure.
// The caller supplies the factory-owned Plan registry, including lf_plan_story.
// NativeWorkflow renders the captured nodes in memory for either harness.
func CapturePlan(ctx context.Context, registry graphexec.Registry) (*WorkflowDefinition, error) {
	source, err := fs.Sub(planAssets, "plan")
	if err != nil {
		return nil, fmt.Errorf("open built-in Plan: %w", err)
	}
	return CaptureWorkflow(ctx, source, "plan.dot", "builtin", agent.ModePlan, registry)
}
