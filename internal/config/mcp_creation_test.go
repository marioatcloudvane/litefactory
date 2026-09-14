package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMCPCreationSetting_StrictFalseDefault(t *testing.T) {
	cases := []struct {
		name, value   string
		want, invalid bool
	}{{"absent", "", false, false}, {"false", "false", false, false}, {"true", "true", true, false}, {"quoted", "\"true\"", false, true}, {"integer", "1", false, true}, {"null", "null", false, true}, {"yes", "yes", false, true}, {"empty", " ", false, true}, {"sequence", "[true]", false, true}, {"mapping", "{enabled: true}", false, true}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "agents:\n  claude: {}\n"
			if tt.name != "absent" {
				body += "settings:\n  allow_mcp_story_creation: " + tt.value + "\n"
			}
			cfg, err := Decode(strings.NewReader(body))
			if tt.invalid {
				if err == nil {
					t.Fatal("invalid gate accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Settings.AllowMCPStoryCreation != tt.want {
				t.Fatalf("gate=%t want=%t", cfg.Settings.AllowMCPStoryCreation, tt.want)
			}
			if !cfg.Settings.MCPAutoStart || cfg.Settings.AutoResumeOnSessionLimit {
				t.Fatal("creation gate changed unrelated loaded policies")
			}
		})
	}
}
func TestMCPCreationSetting_TemplateParity(t *testing.T) {
	t.Parallel()
	body := DefaultFileContents()
	var raw struct {
		Settings struct {
			Allow *bool `yaml:"allow_mcp_story_creation"`
		} `yaml:"settings"`
	}
	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Settings.Allow == nil || *raw.Settings.Allow {
		t.Fatal("template must explicitly disable creation")
	}
	cfg, err := Decode(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.AllowMCPStoryCreation || Defaults().Settings.AllowMCPStoryCreation {
		t.Fatal("creation default not false")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
