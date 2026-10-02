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
			err := runner.validateRequest(agent.Request{Workflow: agent.WorkflowBug, Permissions: agent.Permissions{Profile: "bug-analyst", Payload: tt.surface}})
			if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "bug-analyst") {
				t.Fatalf("expected analyst permission refusal before other validation, got %v", err)
			}
		})
	}
}

// The Claude fixer works like an Implement engineer: it edits, runs its own
// build and tests and may look things up, but never names a tool outside its
// role surface, such as a forge mutation; the factory owns every remote effect.
func TestBugFixer_ClaudeToolSurface(t *testing.T) {
	t.Parallel()
	refused := []struct {
		name    string
		surface ToolSurface
	}{
		{"missing explicit tools", ToolSurface{}},
		{"unlisted delegate", ToolSurface{Tools: []string{"Read", "Edit", "Agent"}}},
		{"notebook", ToolSurface{Tools: []string{"Read", "NotebookEdit"}}},
		{"forge mutation", ToolSurface{Tools: []string{"Read", "mcp__forge__merge"}}},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := (&Runner{}).validateRequest(agent.Request{Workflow: agent.WorkflowBug, Permissions: agent.Permissions{Profile: "bug-fixer", Payload: tt.surface}})
			if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "bug-fixer") {
				t.Fatalf("expected fixer permission refusal, got %v", err)
			}
		})
	}
	t.Run("implement surface passes the role check", func(t *testing.T) {
		t.Parallel()
		surface := ToolSurface{Tools: []string{"Read", "Glob", "Grep", "Edit", "MultiEdit", "Write", "Bash", "Task", "WebFetch"}, AllowedTools: []string{"Read", "Edit", "Bash(go test:*)"}}
		err := (&Runner{}).validateRequest(agent.Request{Workflow: agent.WorkflowBug, Permissions: agent.Permissions{Profile: "bug-fixer", Payload: surface}})
		if err != nil && strings.Contains(err.Error(), "bug-fixer") {
			t.Fatalf("the fixer implement surface was refused: %v", err)
		}
	})
	t.Run("unknown role", func(t *testing.T) {
		t.Parallel()
		err := (&Runner{}).validateRequest(agent.Request{Workflow: agent.WorkflowBug, Permissions: agent.Permissions{Payload: ToolSurface{Tools: []string{"Read"}}}})
		if !errors.Is(err, ErrInvalidRequest) || !strings.Contains(err.Error(), "unknown Bug role") {
			t.Fatalf("expected unknown role refusal, got %v", err)
		}
	})
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
