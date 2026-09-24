package harness

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

// Role selection is captured package data, including on a cold fallback; the
// same argv builder must preserve admitted tools for a second-mode role.
func TestWorkflowMainRolePreservesPermissionsAcrossLaunchPaths(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"research", "plan"} {
		for _, path := range []agent.RunPath{PathCold, PathResume, PathColdAfterResumeRefused} {
			t.Run(mode+"/"+string(path), func(t *testing.T) {
				t.Parallel()
				runner := &Runner{model: "dispatcher-model", tools: []string{"Read"}}
				req := agent.Request{Mode: mode, Path: path, Delivery: &agent.Delivery{Root: "/retained", Identity: "captured", Roles: []string{"file-only-role"}, MainRole: "file-only-role", MainModel: "worker-model"}}
				args := runner.args(req)
				for _, pair := range [][2]string{{"--agent", "litefactory-workflow:file-only-role"}, {"--model", "worker-model"}, {"--tools", "Read"}, {"--plugin-dir", "/retained/plugin"}, {"--append-system-prompt-file", "/retained/bootstrap.md"}} {
					i := slices.Index(args, pair[0])
					if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
						t.Errorf("argv %v does not retain %v", args, pair)
					}
				}
				if !slices.Contains(args, "--strict-mcp-config") || slices.Contains(args, "dispatcher-model") {
					t.Fatalf("argv lost retained role isolation: %v", args)
				}
			})
		}
	}
}
