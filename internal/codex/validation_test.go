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
