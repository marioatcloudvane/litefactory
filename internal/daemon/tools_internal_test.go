package daemon

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func modeProfiles() config.ToolProfiles { return config.DefaultToolProfiles() }

func TestResolveToolsByModeAndAgent(t *testing.T) {
	tests := []struct {
		name             string
		mode             agent.Mode
		kind             agent.Kind
		profile, sandbox string
	}{
		{"research claude", agent.ModeResearch, agent.Kind(config.KindClaude), "research", "workspace-write"},
		{"plan codex", agent.ModePlan, agent.Kind(config.KindCodex), "plan", "workspace-write"},
		{"implement claude", agent.ModeImplement, agent.Kind(config.KindClaude), "implement", "workspace-write"},
		{"validate claude", agent.ModeValidate, agent.Kind(config.KindClaude), "validate", "read-only"},
		{"validate codex", agent.ModeValidate, agent.Kind(config.KindCodex), "validate", "read-only"},
		{"spike runs under its fixer sandbox", agent.ModeSpike, agent.Kind(config.KindCodex), "spike", "workspace-write"},
	}
	for _, d := range []*Daemon{{toolProfiles: modeProfiles()}, {}} {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := d.resolveTools(tt.mode, tt.kind)
				if !ok {
					t.Fatal("not resolved")
				}
				if got.Profile != tt.profile || got.Sandbox != tt.sandbox {
					t.Fatalf("decision=%+v", got)
				}
				if tt.kind == agent.Kind(config.KindClaude) && !slices.Equal(got.Tools, config.AgentTools()) {
					t.Fatalf("Claude tools = %v, want the full surface", got.Tools)
				}
			})
		}
	}
}

func TestResolveToolsHonoursTheConfiguredSandbox(t *testing.T) {
	d := &Daemon{toolProfiles: config.ToolProfiles{Permissions: map[string]config.ToolProfile{
		"validate":               {Sandbox: config.SandboxWorkspaceWrite},
		config.SpikeFixerProfile: {Codex: &config.ToolProfileCodex{Sandbox: config.SandboxReadOnly}},
	}}}
	if got, _ := d.resolveTools(agent.ModeValidate, agent.Kind(config.KindClaude)); got.Sandbox != config.SandboxWorkspaceWrite {
		t.Fatalf("validate sandbox = %q", got.Sandbox)
	}
	if got, _ := d.resolveTools(agent.ModeSpike, agent.Kind(config.KindCodex)); got.Sandbox != config.SandboxReadOnly {
		t.Fatalf("spike sandbox = %q", got.Sandbox)
	}
}

func TestResolveToolsHasNoFallback(t *testing.T) {
	tests := []struct {
		name string
		mode agent.Mode
		kind agent.Kind
	}{{"invalid mode", "bogus", agent.Kind(config.KindClaude)}, {"unknown agent", agent.ModePlan, "other"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Daemon{toolProfiles: modeProfiles()}
			if _, ok := d.resolveTools(tt.mode, tt.kind); ok {
				t.Fatal("unexpected fallback")
			}
		})
	}
}

func TestLegacyToolsLabel(t *testing.T) {
	for _, labels := range [][]string{{"lf:tools=research"}, {"LF:TOOLS=hostile"}} {
		if !legacyToolsLabel(labels) {
			t.Errorf("not detected: %v", labels)
		}
	}
	if legacyToolsLabel([]string{"bug", "lf:tool=research"}) {
		t.Fatal("unrelated label detected")
	}
}

func TestBugRolePermissionsResolveSandboxPerRole(t *testing.T) {
	resolve := bugRolePermissions(config.ToolProfiles{Permissions: map[string]config.ToolProfile{
		"bug-analyst": {Codex: &config.ToolProfileCodex{Sandbox: config.SandboxWorkspaceWrite}},
	}})
	for role, want := range map[string]string{"bug-analyst": config.SandboxWorkspaceWrite, "bug-fixer": config.SandboxWorkspaceWrite, config.ValidateRepairProfile: config.SandboxWorkspaceWrite} {
		for _, kind := range []agent.Kind{agent.Kind(config.KindClaude), agent.Kind(config.KindCodex)} {
			got, err := resolve(kind, role)
			if err != nil || got.Sandbox != want {
				t.Fatalf("%s/%s = %+v %v", role, kind, got, err)
			}
			if wantTools := kind == agent.Kind(config.KindClaude); wantTools != slices.Equal(got.Tools, config.AgentTools()) {
				t.Fatalf("%s/%s tools = %v", role, kind, got.Tools)
			}
		}
	}
	if got, _ := bugRolePermissions(config.ToolProfiles{})(agent.Kind(config.KindClaude), "bug-analyst"); got.Sandbox != config.SandboxReadOnly {
		t.Fatalf("default analyst sandbox = %q", got.Sandbox)
	}
	if _, err := resolve(agent.Kind(config.KindClaude), "implement"); err == nil {
		t.Fatal("a mode resolved as a Bug role")
	}
}
