package agent_test

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"testing"
)

func TestBugWorkflow_ModeCompatibility(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"research", "plan", "implement", "validate", "spike"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mode, ok := agent.ParseMode(name)
			if !ok {
				t.Fatal("existing mode rejected")
			}
			workflow, ok := agent.WorkflowForMode(mode)
			if !ok || string(workflow) != name {
				t.Fatalf("mapping = %q %v", workflow, ok)
			}
			got, ok := workflow.Mode()
			if !ok || got != mode {
				t.Fatalf("reverse mapping = %q %v", got, ok)
			}
			for _, explicit := range []agent.Workflow{"", workflow} {
				if got, err := agent.ResolveWorkflow(explicit, mode); err != nil || got != workflow {
					t.Fatalf("resolve = %q %v", got, err)
				}
			}
		})
	}
	for _, value := range []string{"bug", "unknown", "", "Bug", " bug"} {
		t.Run("standalone_or_invalid/"+value, func(t *testing.T) {
			t.Parallel()
			workflow, ok := agent.ParseWorkflow(value)
			if ok != (value == "bug") {
				t.Fatalf("workflow parse = %q %v", workflow, ok)
			}
			if mode, ok := workflow.Mode(); ok || mode != "" {
				t.Fatalf("unexpected compatibility mode %q", mode)
			}
			if _, ok := agent.ParseMode(value); ok {
				t.Fatal("standalone or invalid identity accepted as mode")
			}
			if _, ok := agent.WorkflowForMode(agent.Mode(value)); ok {
				t.Fatal("invalid mode mapped")
			}
		})
	}
	if _, ok := agent.ParseModeLabel("Mode: Bug"); ok {
		t.Fatal("sixth mode label accepted")
	}
}

func TestBugWorkflow_ResolveBindings(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		workflow agent.Workflow
		mode     agent.Mode
		valid    bool
	}{
		{"standalone", agent.WorkflowBug, "", true},
		{"surrogate research", agent.WorkflowBug, agent.ModeResearch, false},
		{"cast bug mode", "", agent.Mode("bug"), false},
		{"unknown", agent.Workflow("other"), "", false},
		{"conflicting modes", agent.WorkflowPlan, agent.ModeResearch, false},
		{"empty", "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agent.ResolveWorkflow(tt.workflow, tt.mode)
			if (err == nil) != tt.valid {
				t.Fatalf("resolve = %q %v", got, err)
			}
			if tt.valid && got != agent.WorkflowBug {
				t.Fatalf("lost bug binding: %q", got)
			}
		})
	}
}
