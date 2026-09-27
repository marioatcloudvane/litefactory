package harness

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
	"testing"
)

func TestBugWorkflow_PromptIdentity(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		resume   bool
		mode     string
		workflow agent.Workflow
		valid    bool
	}{
		{"cold bug", false, "", agent.WorkflowBug, true}, {"resumed bug", true, "", agent.WorkflowBug, true},
		{"cold conflict", false, "research", agent.WorkflowBug, false}, {"resumed conflict", true, "implement", agent.WorkflowBug, false},
		{"unknown", false, "", agent.Workflow("unknown"), false},
		{"existing mode", false, "spike", agent.WorkflowSpike, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := testPromptInput()
			in.Mode = tt.mode
			in.Workflow = tt.workflow
			in.Resume = tt.resume
			got, err := BuildPrompt(in)
			if (err == nil) != tt.valid {
				t.Fatalf("BuildPrompt=%v", err)
			}
			if !tt.valid {
				return
			}
			if tt.workflow == agent.WorkflowBug {
				if !strings.Contains(got, "- Workflow: bug") || strings.Contains(got, "- Mode:") {
					t.Fatalf("standalone header missing or mode substituted: %s", got)
				}
			} else if !strings.Contains(got, "- Mode: spike") {
				t.Fatal("existing mode header lost")
			}
		})
	}
}
