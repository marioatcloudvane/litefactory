package config

import (
	"strings"
	"testing"
)

func TestBugAgentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		value, agents string
		valid         bool
	}{
		{"", "  claude: {}\n", true},
		{"codex", "  claude: {}\n  codex: {}\n", true},
		{"claude", "  claude: {}\n  codex: {}\n", true},
		{"codex", "  claude: {}\n", false},
		{"unknown", "  claude: {}\n", false},
		{"Codex", "  codex: {}\n", false},
	} {
		t.Run(tc.value+tc.agents, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader("bug_agent: " + tc.value + "\nagents:\n" + tc.agents))
			if err == nil {
				cfg.ToolProfiles = DefaultToolProfiles()
				for name, profile := range cfg.ToolProfiles.Permissions {
					if cfg.Agents.Claude == nil {
						profile.Claude = nil
					}
					if cfg.Agents.Codex == nil {
						profile.Codex = nil
					}
					cfg.ToolProfiles.Permissions[name] = profile
				}
				if cfg.Agents.Claude != nil {
					cfg.Agents.Claude.ToolCeiling = DefaultClaude().ToolCeiling
				}
				err = cfg.Validate()
			}
			if tc.valid {
				if err != nil || string(cfg.BugAgent) != tc.value {
					t.Fatalf("config=%+v err=%v", cfg, err)
				}
				if first, _ := cfg.DefaultAgent(); string(first) != strings.Fields(tc.agents)[0][:len(strings.Fields(tc.agents)[0])-1] {
					t.Fatal("Bug preference reordered agents")
				}
			} else if err == nil || !strings.Contains(err.Error(), "bug_agent") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
