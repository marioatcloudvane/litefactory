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

func TestCanonicalPermissionMatrix(t *testing.T) {
	cfg := canonicalConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	for _, name := range []string{"research", "plan", "implement", "validate"} {
		p, ok := cfg.ToolProfiles.Permissions[name]
		if !ok || p.Claude == nil || p.Codex == nil {
			t.Errorf("profile %q incomplete", name)
		}
	}
	validate := cfg.ToolProfiles.Permissions["validate"]
	for _, tool := range validate.Claude.Tools {
		if tool == "Bash" || tool == "Write" || tool == "Edit" {
			t.Errorf("validate exposes mutating Claude tool %q", tool)
		}
	}
	if validate.Codex.Sandbox != "read-only" {
		t.Errorf("validate sandbox=%q", validate.Codex.Sandbox)
	}
}

func TestPermissionMatrixRejectsMissingExtraAndAgentMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing mode", func(c *Config) { delete(c.ToolProfiles.Permissions, "plan") }, "plan"},
		{"extra mode", func(c *Config) { c.ToolProfiles.Permissions["custom"] = c.ToolProfiles.Permissions["plan"] }, "custom"},
		{"missing agent stanza", func(c *Config) {
			p := c.ToolProfiles.Permissions["research"]
			p.Codex = nil
			c.ToolProfiles.Permissions["research"] = p
		}, "codex"},
		{"claude widens ceiling", func(c *Config) {
			p := c.ToolProfiles.Permissions["validate"]
			p.Claude.Tools = []string{"Read", "WebFetch"}
			c.ToolProfiles.Permissions["validate"] = p
		}, "tool_ceiling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := canonicalConfig(t)
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want %q", err, tt.want)
			}
		})
	}
}

func TestProfileStanzasDoNotCrossDerive(t *testing.T) {
	p := ToolProfile{Claude: &ToolProfileClaude{Tools: []string{"Read"}}}
	if !p.HasStanzaFor("claude") || p.HasStanzaFor("codex") || p.HasStanzaFor("other") {
		t.Fatalf("unexpected stanza routing")
	}
}
