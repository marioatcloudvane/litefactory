package codex

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestBugAnalyst_ReadOnlyArgv(t *testing.T) {
	t.Parallel()
	for _, p := range []agent.RunPath{agent.PathCold, agent.PathResume} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			runner := &Runner{}
			args, err := runner.args(agent.Request{Workflow: agent.WorkflowBug, Path: p}, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"--ignore-user-config", "--ignore-rules"} {
				if !slices.Contains(args, want) {
					t.Errorf("missing analyst isolation argument %q: %v", want, args)
				}
			}
			// The shell is Codex's only file reader; the read-only sandbox,
			// not a disabled shell, keeps the analyst from writing.
			if slices.Contains(args, "features.shell_tool=false") {
				t.Errorf("analyst shell disabled: %v", args)
			}
			index := slices.Index(args, "--sandbox")
			if index < 0 || index+1 >= len(args) || args[index+1] != "read-only" {
				t.Fatalf("missing read-only sandbox: %v", args)
			}
		})
	}
}
