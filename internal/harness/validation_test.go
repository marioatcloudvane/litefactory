package harness

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"strings"
	"testing"
)

func TestValidateRunnerRejectsMutatingAndFreshCheckTools(t *testing.T) {
	r := &Runner{}
	for _, tools := range [][]string{nil, {"Bash"}, {"Write"}, {"Read", "Edit"}} {
		req := agent.Request{Mode: "validate", Permissions: agent.Permissions{Payload: ToolSurface{Tools: tools}}}
		err := r.validateRequest(req)
		if err == nil || !strings.Contains(err.Error(), "Validate") {
			t.Fatal("unsafe surface accepted", tools, err)
		}
	}
}

func TestRetainedValidationRemovesDelegationWithoutMutatingCaller(t *testing.T) {
	tools := []string{"Read", "Task", "Glob", "Grep"}
	allowed := []string{"Read", "Task", "Glob", "Grep"}
	req := agent.Request{Mode: "validate", RetainedValidation: true, Permissions: agent.Permissions{Payload: ToolSurface{Tools: tools, AllowedTools: allowed}}}
	normalized := retainedValidationRequest(req)
	surface, ok := normalized.Permissions.Payload.(ToolSurface)
	if !ok {
		t.Fatal("normalized validation permissions lack a tool surface")
	}
	if strings.Join(surface.Tools, ",") != "Read,Glob,Grep" || strings.Join(surface.AllowedTools, ",") != "Read,Glob,Grep" {
		t.Fatalf("retained review grants delegation: %+v", surface)
	}
	surface.Tools[0], surface.AllowedTools[0] = "changed", "changed"
	if strings.Join(tools, ",") != "Read,Task,Glob,Grep" || strings.Join(allowed, ",") != "Read,Task,Glob,Grep" {
		t.Fatal("normalization aliases caller tool authority")
	}
	if err := (&Runner{}).validateRequest(req); err == nil || !strings.Contains(err.Error(), "Validate") {
		t.Fatalf("direct Task bypass accepted: %v", err)
	}
}
