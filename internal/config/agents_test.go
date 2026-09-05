package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestAgentOrderControlsDefault(t *testing.T) {
	tests := []struct {
		name, yaml string
		want       []Kind
	}{
		{"claude first", "agents:\n  claude: {}\n  codex: {}\n", []Kind{KindClaude, KindCodex}},
		{"codex first", "agents:\n  codex: {}\n  claude: {}\n", []Kind{KindCodex, KindClaude}},
		{"one agent", "agents:\n  codex: {}\n", []Kind{KindCodex}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader(tt.yaml))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.AgentOrder(), tt.want) {
				t.Fatalf("order=%v want %v", cfg.AgentOrder(), tt.want)
			}
			got, ok := cfg.DefaultAgent()
			if !ok || got != tt.want[0] {
				t.Fatalf("default=(%q,%v)", got, ok)
			}
		})
	}
}

func TestAgentsStrictness(t *testing.T) {
	tests := []struct{ name, yaml, want string }{
		{"none", "settings: {}\n", "at least one"},
		{"unknown", "agents:\n  other: {}\n", "unsupported coding agent"},
		{"duplicate", "agents:\n  claude: {}\n  claude: {}\n", "already defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parse(t, tt.yaml)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v want %q", err, tt.want)
			}
		})
	}
}

func TestAgentDefaultsAreTierOnly(t *testing.T) {
	cfg, err := Decode(strings.NewReader("agents:\n  claude: {}\n  codex: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Claude.ModelTier != TierBalanced || cfg.Agents.Codex.ModelTier != TierBalanced {
		t.Fatalf("tiers=(%q,%q)", cfg.Agents.Claude.ModelTier, cfg.Agents.Codex.ModelTier)
	}
}
