package config

import (
	"strings"
	"testing"
)

func canonicalConfig(t *testing.T) *Config {
	t.Helper()
	cfg := Defaults()
	claude := DefaultClaude()
	codex := DefaultCodex()
	cfg.Agents = Agents{Claude: &claude, Codex: &codex}
	cfg.agentOrder = []Kind{KindClaude, KindCodex}
	cfg.ToolProfiles = DefaultToolProfiles()
	return &cfg
}

func TestProfileResolveDefaultsAndOverrides(t *testing.T) {
	var empty ToolProfiles
	for name, want := range map[string]ToolProfile{
		"research":               {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"plan":                   {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"implement":              {Sandbox: SandboxWorkspaceWrite},
		"validate":               {Sandbox: SandboxReadOnly},
		ValidateRepairProfile:    {Sandbox: SandboxWorkspaceWrite},
		"bug-analyst":            {Sandbox: SandboxReadOnly},
		"bug-fixer":              {Sandbox: SandboxWorkspaceWrite},
		SpikeInvestigatorProfile: {Sandbox: SandboxReadOnly},
		SpikeFixerProfile:        {Sandbox: SandboxWorkspaceWrite},
	} {
		got, ok := empty.Resolve(name)
		if !ok || got.Sandbox != want.Sandbox || got.Attachments != want.Attachments {
			t.Errorf("Resolve(%q) = %+v, %t; want %+v", name, got, ok, want)
		}
	}
	if _, ok := empty.Resolve("spike"); ok {
		t.Error("an unknown profile name resolved")
	}

	configured := ToolProfiles{Permissions: map[string]ToolProfile{
		"validate":  {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"implement": {Codex: &ToolProfileCodex{Sandbox: SandboxReadOnly}},
		"plan":      {Sandbox: SandboxReadOnly, Codex: &ToolProfileCodex{Sandbox: SandboxWorkspaceWrite}},
		"research":  {Claude: &ToolProfileClaude{Tools: []string{"Read"}}},
	}}
	for name, want := range map[string]ToolProfile{
		"validate":  {Sandbox: SandboxWorkspaceWrite, Attachments: true},
		"implement": {Sandbox: SandboxReadOnly},
		"plan":      {Sandbox: SandboxReadOnly},
		"research":  {Sandbox: SandboxWorkspaceWrite},
	} {
		got, _ := configured.Resolve(name)
		if got.Sandbox != want.Sandbox || got.Attachments != want.Attachments {
			t.Errorf("configured Resolve(%q) = %+v, want %+v", name, got, want)
		}
	}
}

func TestPermissionProfilesRejectUnknownNamesAndSandboxes(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"custom": func(c *Config) { c.ToolProfiles.Permissions["custom"] = ToolProfile{} },
		"sandbox": func(c *Config) {
			c.ToolProfiles.Permissions["plan"] = ToolProfile{Sandbox: "danger-full-access"}
		},
		"codex.sandbox": func(c *Config) {
			c.ToolProfiles.Permissions["plan"] = ToolProfile{Codex: &ToolProfileCodex{Sandbox: "danger-full-access"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := canonicalConfig(t)
			mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error=%v want %q", err, name)
			}
		})
	}
	cfg := canonicalConfig(t)
	cfg.ToolProfiles = ToolProfiles{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a configuration without tool_profiles is refused: %v", err)
	}
}

// A config.yaml written before the sandbox became the boundary still loads:
// the retired tool lists, ceilings and per-mode Claude stanzas are accepted
// and ignored, and codex.sandbox still selects the sandbox.
func TestRetiredToolKeysStillLoad(t *testing.T) {
	body := `agents:
  claude:
    binary: "/usr/local/bin/claude"
    tools: ["Read", "Bash"]
    allowed_tools: ["Read", "Bash(git status:*)"]
    tool_ceiling: ["Read", "Bash", "WebFetch"]
  codex:
    binary: "/usr/local/bin/codex"
tool_profiles:
  permissions:
    validate:
      attachments: false
      claude:
        tools: ["Read", "Glob", "Grep", "Task", "Bash"]
        allowed_tools: ["Read", "Bash(rg:*)"]
      codex:
        sandbox: "read-only"
    implement:
      attachments: false
      claude:
        tools: ["Read", "Edit"]
        allowed_tools: ["Bash"]
      codex:
        sandbox: "workspace-write"
    bug-analyst:
      claude:
        tools: ["Read"]
      codex:
        sandbox: "read-only"
`
	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, _ := cfg.ToolProfiles.Resolve("validate"); got.Sandbox != SandboxReadOnly {
		t.Fatalf("validate sandbox = %q", got.Sandbox)
	}
}
