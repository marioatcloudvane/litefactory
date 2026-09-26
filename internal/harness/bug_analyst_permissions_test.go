package harness

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestBugAnalyst_RefusesUnsafeToolSurface(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		surface ToolSurface
	}{
		{"missing explicit tools", ToolSurface{}},
		{"shell", ToolSurface{Tools: []string{"Read", "Bash"}}},
		{"edit", ToolSurface{Tools: []string{"Read", "Edit"}}},
		{"write", ToolSurface{Tools: []string{"Read", "Write"}}},
		{"delegate", ToolSurface{Tools: []string{"Read", "Agent"}}},
		{"forge mutation", ToolSurface{Tools: []string{"Read", "mcp__forge__merge"}}},
		{"allowed shell rule", ToolSurface{Tools: []string{"Read"}, AllowedTools: []string{"Bash(go test:*)"}}},
		{"allowed forge rule", ToolSurface{Tools: []string{"Read"}, AllowedTools: []string{"mcp__forge__close"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &Runner{}
			err := runner.validateRequest(agent.Request{Workflow: agent.WorkflowBug, Permissions: agent.Permissions{Payload: tt.surface}})
			if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "Bug analyst") {
				t.Fatalf("expected analyst permission refusal before other validation, got %v", err)
			}
		})
	}
}

func TestBugAnalyst_ReadSurfaceOnColdAndResumeArgv(t *testing.T) {
	t.Parallel()
	for _, p := range []agent.RunPath{agent.PathCold, agent.PathResume} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			runner := &Runner{}
			req := agent.Request{Workflow: agent.WorkflowBug, Path: p, Permissions: agent.Permissions{Payload: ToolSurface{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}}}
			args := runner.args(req)
			value := func(flag string) string {
				for i, s := range args {
					if s == flag && i+1 < len(args) {
						return args[i+1]
					}
				}
				t.Fatalf("missing flag %s: %v", flag, args)
				return ""
			}
			if got := value("--tools"); got != "Read,Glob,Grep" {
				t.Fatalf("tool surface=%q", got)
			}
			if got := value("--allowedTools"); got != "Read,Glob,Grep" {
				t.Fatalf("allowed surface=%q", got)
			}
		})
	}
}

func TestBugAnalyst_ResultCatalogAugmentsReadSurface(t *testing.T) {
	t.Parallel()
	for _, p := range []agent.RunPath{agent.PathCold, agent.PathResume} {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			launch, err := agent.NewMCPLaunchSpec("/synthetic/litefactory", "/synthetic/descriptor.json", "analyst-attempt", "communication_preflight", "communication_submit", "communication_receipt")
			if err != nil {
				t.Fatal(err)
			}
			req := agent.Request{Workflow: agent.WorkflowBug, Path: p, MCPLaunch: launch, Permissions: agent.Permissions{Payload: ToolSurface{Tools: []string{"Read", "Glob", "Grep"}, AllowedTools: []string{"Read", "Glob", "Grep"}}}}
			args := (&Runner{}).args(req)
			values := map[string]string{}
			for i := 0; i+1 < len(args); i++ {
				if args[i] == "--tools" || args[i] == "--allowedTools" {
					values[args[i]] = args[i+1]
				}
			}
			if values["--tools"] != "Read,Glob,Grep" {
				t.Fatalf("MCP catalog broadened native tool surface: %q", values["--tools"])
			}
			want := "Read,Glob,Grep,mcp__" + mcpServerName + "__communication_preflight,mcp__" + mcpServerName + "__communication_submit,mcp__" + mcpServerName + "__communication_receipt"
			if values["--allowedTools"] != want {
				t.Fatalf("allowed tools=%q, want %q", values["--allowedTools"], want)
			}
		})
	}
}
