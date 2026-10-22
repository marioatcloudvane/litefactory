package daemon

import (
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func modeProfiles() map[string]config.ToolProfile { return config.DefaultToolProfiles().Permissions }

func TestResolveToolsByModeAndAgent(t *testing.T) {
	tests := []struct {
		name             string
		mode             agent.Mode
		kind             agent.Kind
		profile, sandbox string
	}{
		{"research claude", agent.ModeResearch, agent.Kind(config.KindClaude), "research", ""},
		{"plan codex", agent.ModePlan, agent.Kind(config.KindCodex), "plan", "workspace-write"},
		{"implement claude", agent.ModeImplement, agent.Kind(config.KindClaude), "implement", ""},
		{"validate codex", agent.ModeValidate, agent.Kind(config.KindCodex), "validate", "read-only"},
		{"spike own ceiling codex", agent.ModeSpike, agent.Kind(config.KindCodex), "spike", "workspace-write"},
	}
	d := &Daemon{toolProfiles: modeProfiles()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := d.resolveTools(tt.mode, tt.kind)
			if !ok {
				t.Fatal("not resolved")
			}
			if got.Profile != tt.profile || got.Sandbox != tt.sandbox {
				t.Fatalf("decision=%+v", got)
			}
			if tt.kind == agent.Kind(config.KindClaude) && len(got.Tools) == 0 {
				t.Fatal("Claude tools empty")
			}
		})
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

func TestResolveToolsCopiesSlices(t *testing.T) {
	d := &Daemon{toolProfiles: modeProfiles()}
	first, ok := d.resolveTools(agent.ModeResearch, agent.Kind(config.KindClaude))
	if !ok {
		t.Fatal("not resolved")
	}
	first.Tools[0] = "changed"
	again, _ := d.resolveTools(agent.ModeResearch, agent.Kind(config.KindClaude))
	if again.Tools[0] == "changed" {
		t.Fatal("configuration slice was aliased")
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

func TestResolveSpikeToolsUsesOnlyExplicitCappedRoleBindings(t *testing.T) {
	claude, codex := agent.Kind(config.KindClaude), agent.Kind(config.KindCodex)
	profiles := func(mutate func(map[string]config.ToolProfile)) *Daemon {
		p := config.DefaultToolProfiles().Permissions
		mutate(p)
		return &Daemon{toolProfiles: p, toolCeiling: []string{"Read", "Glob", "Grep", "Edit", "Write", "Bash"}}
	}
	t.Run("ceiling is the fixer role, never implement", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {})
		got, ok := d.resolveTools(agent.ModeSpike, claude)
		if !ok || got.Profile != "spike" || !slices.Contains(got.Tools, "Bash") || slices.Contains(got.Tools, "Task") {
			t.Fatalf("decision=%+v ok=%t", got, ok)
		}
		got.Tools[0] = "changed"
		if again, _ := d.resolveTools(agent.ModeSpike, claude); again.Tools[0] == "changed" {
			t.Fatal("configuration slice was aliased")
		}
	})
	t.Run("implement profile changes nothing", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {
			implement := p["implement"]
			implement.Claude.Tools = append(implement.Claude.Tools, "WebFetch")
			p["implement"] = implement
		})
		got, ok := d.resolveTools(agent.ModeSpike, claude)
		if !ok || slices.Contains(got.Tools, "WebFetch") {
			t.Fatalf("implement leaked into spike: %+v", got)
		}
	})
	for _, name := range []string{config.SpikeInvestigatorProfile, config.SpikeFixerProfile} {
		t.Run("missing "+name+" fails closed", func(t *testing.T) {
			d := profiles(func(p map[string]config.ToolProfile) { delete(p, name) })
			for _, kind := range []agent.Kind{claude, codex} {
				if got, ok := d.resolveTools(agent.ModeSpike, kind); ok {
					t.Fatalf("%s resolved without %s: %+v", kind, name, got)
				}
			}
		})
	}
	t.Run("missing stanza for the selected agent fails closed", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {
			fixer := p[config.SpikeFixerProfile]
			fixer.Codex = nil
			p[config.SpikeFixerProfile] = fixer
		})
		if _, ok := d.resolveTools(agent.ModeSpike, codex); ok {
			t.Fatal("codex resolved without a fixer stanza")
		}
		if _, ok := d.resolveTools(agent.ModeSpike, claude); !ok {
			t.Fatal("claude lost its binding")
		}
	})
	t.Run("grant outside the operator ceiling fails closed", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {})
		d.toolCeiling = []string{"Read", "Glob", "Grep"}
		if _, ok := d.resolveTools(agent.ModeSpike, claude); ok {
			t.Fatal("fixer edits granted beyond the ceiling")
		}
	})
	t.Run("investigator wider than fixer fails closed", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {
			inv := p[config.SpikeInvestigatorProfile]
			inv.Claude.Tools = append(slices.Clone(inv.Claude.Tools), "Edit", "Task")
			p[config.SpikeInvestigatorProfile] = inv
		})
		if _, ok := d.resolveTools(agent.ModeSpike, claude); ok {
			t.Fatal("investigator exceeded the mode ceiling")
		}
	})
	t.Run("wrong sandboxes and attachments fail closed", func(t *testing.T) {
		d := profiles(func(p map[string]config.ToolProfile) {
			inv := p[config.SpikeInvestigatorProfile]
			inv.Codex = &config.ToolProfileCodex{Sandbox: "workspace-write"}
			p[config.SpikeInvestigatorProfile] = inv
		})
		if _, ok := d.resolveTools(agent.ModeSpike, codex); ok {
			t.Fatal("a writable investigator was accepted")
		}
		d = profiles(func(p map[string]config.ToolProfile) {
			fixer := p[config.SpikeFixerProfile]
			fixer.Attachments = true
			p[config.SpikeFixerProfile] = fixer
		})
		if _, ok := d.resolveTools(agent.ModeSpike, claude); ok {
			t.Fatal("attachment ingestion was granted to Spike")
		}
	})
}
