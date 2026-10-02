package teamsource

import (
	"context"
	"embed"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/graphexec"
)

// bugAssets contains the Bug-owned role closure. The parent graph is supplied
// by its lifecycle increment; role capture alone never admits graph execution.
//
//go:embed bug
var bugAssets embed.FS

// CaptureBugAnalyst resolves the shipped read-only analyst and its result
// resources using the same bounded parser as other factory-owned graph roles.
// NativeWorkflow renders this closure for isolated installation by either
// adapter, without modifying an existing installed instruction tree.
func CaptureBugAnalyst(ctx context.Context) (WorkflowNode, error) {
	source, err := fs.Sub(bugAssets, "bug")
	if err != nil {
		return WorkflowNode{}, err
	}
	return captureResearchNode(ctx, source, graphexec.Node{
		Name: "analyst", Limit: 8,
		Attrs: map[string]string{"lf_agent_markdown": "bug-analyst", "lf_skills": "bug-analysis-result"},
	}, nil)
}
