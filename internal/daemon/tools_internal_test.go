package daemon

import (
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
		{"spike borrows implement", agent.ModeSpike, agent.Kind(config.KindCodex), "implement", "workspace-write"},
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
