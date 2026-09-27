package teamsource

import (
	"context"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"io/fs"
)

// CaptureBugFixer captures the complete dedicated role for isolated native
// installation. Capturing instructions grants no runner or effect permissions.
func CaptureBugFixer(ctx context.Context) (WorkflowNode, error) {
	source, err := fs.Sub(bugAssets, "bug")
	if err != nil {
		return WorkflowNode{}, err
	}
	return captureResearchNode(ctx, source, graphexec.Node{Name: "fixer", Limit: 3, Attrs: map[string]string{"lf_agent_markdown": "bug-fixer", "lf_skills": "bug-repair-result"}}, nil)
}
