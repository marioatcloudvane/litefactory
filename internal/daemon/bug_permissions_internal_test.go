package daemon

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestBugRuntimePermissionsNoModeSurrogate(t *testing.T) {
	profiles := map[string]config.ToolProfile{
		"research":  {Codex: &config.ToolProfileCodex{Sandbox: "read-only"}},
		"implement": {Codex: &config.ToolProfileCodex{Sandbox: "workspace-write"}},
	}
	resolve := bugRolePermissions(profiles, []string{"Read", "Glob", "Grep", "Edit"})
	for _, role := range []string{"bug-analyst", "bug-fixer", "research", "unknown"} {
		if _, err := resolve(agent.Kind(config.KindCodex), role); err == nil {
			t.Fatalf("missing independent role %s borrowed mode authority", role)
		}
	}
}

func TestBugRuntimePermissionsExplicitProfilesAndImmutableCeiling(t *testing.T) {
	profiles := map[string]config.ToolProfile{
		"bug-analyst": {Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Grep"}, AllowedTools: []string{"Read"}}, Codex: &config.ToolProfileCodex{Sandbox: "read-only"}},
		"bug-fixer":   {Codex: &config.ToolProfileCodex{Sandbox: "workspace-write"}},
	}
	ceiling := []string{"Read", "Glob", "Grep"}
	resolve := bugRolePermissions(profiles, ceiling)
	profiles["bug-analyst"].Claude.Tools[0] = "Bash"
	profiles["bug-analyst"].Codex.Sandbox = "workspace-write"
	ceiling[0] = "Bash"
	for _, tc := range []struct {
		kind          agent.Kind
		role, sandbox string
	}{
		{agent.Kind(config.KindCodex), "bug-analyst", "read-only"},
		{agent.Kind(config.KindCodex), "bug-fixer", "workspace-write"},
		{agent.Kind(config.KindClaude), "bug-analyst", ""},
	} {
		p, err := resolve(tc.kind, tc.role)
		if err != nil || p.Sandbox != tc.sandbox {
			t.Fatalf("%s/%s: %+v %v", tc.kind, tc.role, p, err)
		}
		if len(p.Tools) > 0 {
			p.Tools[0] = "Bash"
			fresh, err := resolve(tc.kind, tc.role)
			if err != nil || fresh.Tools[0] != "Read" {
				t.Fatalf("caller mutated later role permission: %+v %v", fresh, err)
			}
		}
	}
	if _, err := resolve(agent.Kind(config.KindClaude), "bug-fixer"); err == nil {
		t.Fatal("a Claude fixer without an explicit profile received edit authority")
	}
}

// A Claude fixer works like an Implement engineer, shell included, within its
// role surface and the operator ceiling; Bash rules count as Bash.
func TestBugRuntimePermissionsClaudeFixer(t *testing.T) {
	ceiling := []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}
	fixer := func(tools ...string) map[string]config.ToolProfile {
		return map[string]config.ToolProfile{"bug-fixer": {Claude: &config.ToolProfileClaude{Tools: tools, AllowedTools: []string{"Read", "Bash(go test:*)"}}}}
	}
	p, err := bugRolePermissions(fixer("Read", "Grep", "Edit", "Write", "Bash"), ceiling)(agent.Kind(config.KindClaude), "bug-fixer")
	if err != nil || len(p.Tools) != 5 {
		t.Fatalf("Claude fixer shell surface refused: %+v %v", p, err)
	}
	for _, tools := range [][]string{{"Read", "Edit", "Task"}, {"Read", "Edit", "MultiEdit"}, {"Read", "Edit", "NotebookEdit"}} {
		if _, err := bugRolePermissions(fixer(tools...), ceiling)(agent.Kind(config.KindClaude), "bug-fixer"); err == nil {
			t.Fatalf("Claude fixer tools %v outside the role surface or ceiling admitted", tools)
		}
	}
}

func TestBugRuntimePermissionsRejectWidening(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile config.ToolProfile
		kind    agent.Kind
		role    string
	}{
		{"analyst shell", config.ToolProfile{Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Bash"}}}, agent.Kind(config.KindClaude), "bug-analyst"},
		{"analyst extra MCP", config.ToolProfile{Claude: &config.ToolProfileClaude{Tools: []string{"Read"}, AllowedTools: []string{"mcp__forge__merge"}}}, agent.Kind(config.KindClaude), "bug-analyst"},
		{"analyst write sandbox", config.ToolProfile{Codex: &config.ToolProfileCodex{Sandbox: "workspace-write"}}, agent.Kind(config.KindCodex), "bug-analyst"},
		{"fixer broad sandbox", config.ToolProfile{Codex: &config.ToolProfileCodex{Sandbox: "unrestricted"}}, agent.Kind(config.KindCodex), "bug-fixer"},
		{"attachment grant", config.ToolProfile{Codex: &config.ToolProfileCodex{Sandbox: "read-only"}, Attachments: true}, agent.Kind(config.KindCodex), "bug-analyst"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := bugRolePermissions(map[string]config.ToolProfile{tc.role: tc.profile}, []string{"Read", "Bash"})
			if _, err := resolve(tc.kind, tc.role); err == nil {
				t.Fatal("unsafe role grant admitted")
			}
		})
	}
}

// The Validate repair agent's grant is resolved like the Bug fixer's: explicit,
// contained workspace-write on Codex, the fixer surface on Claude, and never
// borrowed from the read-only validate mode profile.
func TestValidateRepairPermissionsExplicitAndBounded(t *testing.T) {
	ceiling := []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}
	modeOnly := map[string]config.ToolProfile{"validate": {Codex: &config.ToolProfileCodex{Sandbox: "read-only"}, Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Glob", "Grep", "Task"}}}}
	for _, kind := range []agent.Kind{agent.Kind(config.KindCodex), agent.Kind(config.KindClaude)} {
		if _, err := bugRolePermissions(modeOnly, ceiling)(kind, config.ValidateRepairProfile); err == nil || !strings.Contains(err.Error(), "missing explicit validate-repair") {
			t.Fatalf("%s: missing validate-repair borrowed mode authority: %v", kind, err)
		}
	}
	profiles := map[string]config.ToolProfile{config.ValidateRepairProfile: {
		Codex:  &config.ToolProfileCodex{Sandbox: "workspace-write"},
		Claude: &config.ToolProfileClaude{Tools: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}, AllowedTools: []string{"Read", "Bash(go test:*)"}},
	}}
	resolve := bugRolePermissions(profiles, ceiling)
	if p, err := resolve(agent.Kind(config.KindCodex), config.ValidateRepairProfile); err != nil || p.Sandbox != "workspace-write" {
		t.Fatalf("Codex validate-repair: %+v %v", p, err)
	}
	if p, err := resolve(agent.Kind(config.KindClaude), config.ValidateRepairProfile); err != nil || strings.Join(p.Tools, ",") != "Read,Glob,Grep,Edit,Write,Bash" {
		t.Fatalf("Claude validate-repair: %+v %v", p, err)
	}
	for _, profile := range []config.ToolProfile{
		{Codex: &config.ToolProfileCodex{Sandbox: "read-only"}},
		{Codex: &config.ToolProfileCodex{Sandbox: "danger-full-access"}},
		{Codex: &config.ToolProfileCodex{Sandbox: "workspace-write"}, Attachments: true},
	} {
		if _, err := bugRolePermissions(map[string]config.ToolProfile{config.ValidateRepairProfile: profile}, ceiling)(agent.Kind(config.KindCodex), config.ValidateRepairProfile); err == nil {
			t.Fatalf("unsafe Codex validate-repair grant admitted: %+v", profile.Codex)
		}
	}
	for _, tools := range [][]string{{"Read", "NotebookEdit"}, {"Read", "Edit", "Task"}} {
		p := map[string]config.ToolProfile{config.ValidateRepairProfile: {Claude: &config.ToolProfileClaude{Tools: tools}}}
		if _, err := bugRolePermissions(p, ceiling)(agent.Kind(config.KindClaude), config.ValidateRepairProfile); err == nil {
			t.Fatalf("Claude validate-repair tools %v outside the role surface or ceiling admitted", tools)
		}
	}
}
