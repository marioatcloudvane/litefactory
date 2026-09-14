package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMCPAutoStart_StrictLoadedPolicy(t *testing.T) {
	cases := []struct {
		name, value   string
		want, invalid bool
	}{
		{"absent", "", true, false}, {"true", "true", true, false}, {"false", "false", false, false},
		{"string", "\"true\"", false, true}, {"integer", "1", false, true}, {"null", "null", false, true}, {"yes", "yes", false, true}, {"sequence", "[true]", false, true}, {"mapping", "{enabled: true}", false, true}, {"empty", " ", false, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := "agents:\n  claude: {}\n"
			if tt.name != "absent" {
				data += "settings:\n  mcp_auto_start: " + tt.value + "\n"
			}
			cfg, err := Decode(strings.NewReader(data))
			if tt.invalid {
				if err == nil {
					t.Fatal("invalid boolean policy accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Settings.MCPAutoStart != tt.want {
				t.Fatalf("automatic startup=%t want=%t", cfg.Settings.MCPAutoStart, tt.want)
			}
			if cfg.Settings.AutoResumeOnSessionLimit {
				t.Fatal("MCP convenience enabled unrelated automatic recovery")
			}
		})
	}
}

func TestMCPAutoStart_TemplateParity(t *testing.T) {
	t.Parallel()
	body := DefaultFileContents()
	var raw struct {
		Settings struct {
			Auto *bool `yaml:"mcp_auto_start"`
		} `yaml:"settings"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Settings.Auto == nil || !*raw.Settings.Auto {
		t.Fatal("template must explicitly enable automatic registration")
	}
	cfg, err := Decode(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Settings.MCPAutoStart || !Defaults().Settings.MCPAutoStart {
		t.Fatal("template and defaults disagree")
	}
	disabled := strings.Replace(string(body), "mcp_auto_start: true", "mcp_auto_start: false", 1)
	cfg, err = Decode(strings.NewReader(disabled))
	if err != nil || cfg.Settings.MCPAutoStart {
		t.Fatalf("explicit false lost after template decode: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
