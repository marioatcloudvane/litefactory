package codex

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
	"testing"
)

func TestValidateDisablesShellOnAllConversationPaths(t *testing.T) {
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		req := request(t, path)
		req.Mode = "validate"
		r := &Runner{}
		argv, err := r.args(req, "read-only")
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(argv, " ")
		for _, want := range []string{"--sandbox read-only", "features.shell_tool=false", "features.multi_agent=false"} {
			if !strings.Contains(args, want) {
				t.Fatalf("%s missing %s", args, want)
			}
		}
	}
}

func TestInspectionBrokerKeepsColdAndResumedExecutionDisabled(t *testing.T) {
	t.Parallel()
	for _, workflow := range []string{"validate", "bug"} {
		for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
			req := request(t, path)
			req.Mode = workflow
			if workflow == "bug" {
				req.Mode = ""
				req.Workflow = agent.WorkflowBug
			}
			req.MCPLaunch = mustSpec(t, "/factory/litefactory", "/private/descriptor.json", "inspection_list", "inspection_read", "inspection_search")
			argv, err := (&Runner{}).args(req, "read-only")
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(argv, " ")
			for _, want := range []string{"--sandbox read-only", "features.shell_tool=false", "features.multi_agent=false", "inspection_list", "inspection_read", "inspection_search"} {
				if !strings.Contains(joined, want) {
					t.Fatalf("%s %s missing %q: %s", workflow, path, want, joined)
				}
			}
		}
	}
}
