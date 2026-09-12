package config

import (
	"reflect"
	"slices"
	"strconv"
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

func TestValidateProfileIsExactlyReadOnly(t *testing.T) {
	cfg := canonicalConfig(t)
	profile := cfg.ToolProfiles.Permissions["validate"]
	wantTools := []string{"Read", "Glob", "Grep", "Task"}
	wantAllowed := []string{"Read", "Glob", "Grep"}
	if !reflect.DeepEqual(profile.Claude.Tools, wantTools) {
		t.Fatalf("Claude Validate tools = %v, want %v", profile.Claude.Tools, wantTools)
	}
	if !reflect.DeepEqual(profile.Claude.AllowedTools, wantAllowed) {
		t.Fatalf("Claude Validate allowed_tools = %v, want %v", profile.Claude.AllowedTools, wantAllowed)
	}
	if profile.Codex.Sandbox != "read-only" {
		t.Fatalf("Codex Validate sandbox = %q, want read-only", profile.Codex.Sandbox)
	}
}

func TestValidateProfileRejectsMutatingAuthority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ToolProfile)
		want   []string
	}{
		{"Claude Bash surface", func(p *ToolProfile) { p.Claude.Tools = []string{"Read", "Glob", "Grep", "Task", "Bash"} }, []string{`permissions["validate"].claude.tools`, `"Bash"`, "read-only"}},
		{"Claude Edit surface", func(p *ToolProfile) { p.Claude.Tools = []string{"Read", "Glob", "Grep", "Task", "Edit"} }, []string{`permissions["validate"].claude.tools`, `"Edit"`, "read-only"}},
		{"Claude Write surface", func(p *ToolProfile) { p.Claude.Tools = []string{"Read", "Glob", "Grep", "Task", "Write"} }, []string{`permissions["validate"].claude.tools`, `"Write"`, "read-only"}},
		{"Claude shell permission", func(p *ToolProfile) { p.Claude.AllowedTools = []string{"Read", "Bash(git status:*)"} }, []string{`permissions["validate"].claude.allowed_tools`, "Bash(git status:*)", "read-only"}},
		{"Codex workspace write", func(p *ToolProfile) { p.Codex.Sandbox = "workspace-write" }, []string{`permissions["validate"].codex.sandbox`, "read-only"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := canonicalConfig(t)
			profile := cfg.ToolProfiles.Permissions["validate"]
			tt.mutate(&profile)
			cfg.ToolProfiles.Permissions["validate"] = profile
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate accepted mutating authority")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
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
	for _, name := range []string{"research", "plan", "implement", "validate"} {
		p := cfg.ToolProfiles.Permissions[name]
		for _, tool := range []string{"WebSearch", "WebFetch"} {
			want := name == "research"
			if slices.Contains(p.Claude.Tools, tool) != want || slices.Contains(p.Claude.AllowedTools, tool) != want {
				t.Errorf("%s direct web permission for %s, want %t", name, tool, want)
			}
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
	for _, explicit := range []bool{false, true} {
		t.Run("legacy ceiling explicit="+strconv.FormatBool(explicit), func(t *testing.T) {
			body := string(DefaultFileContents())
			ceiling := ""
			if explicit {
				ceiling = yamlList(4, "tool_ceiling", defaultTools())
			}
			body = strings.Replace(body, yamlList(4, "tool_ceiling", DefaultClaude().ToolCeiling), ceiling, 1)
			for _, tool := range []string{"WebSearch", "WebFetch"} {
				body = strings.ReplaceAll(body, "          - "+strconv.Quote(tool)+"\n", "")
			}
			legacy, err := Decode(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if err := legacy.Validate(); err != nil {
				t.Fatalf("persisted non-web profiles rejected: %v", err)
			}
			if !slices.Equal(effectiveToolCeiling(legacy.Agents.Claude), defaultTools()) {
				t.Fatal("legacy ceiling widened")
			}
		})
	}

}

func TestCanonicalAttachmentPermissionsAndOverrides(t *testing.T) {
	tests := []struct {
		name        string
		profile     string
		wantDefault bool
	}{
		{"research/default enabled", "research", true},
		{"plan/default enabled", "plan", true},
		{"implement/default disabled", "implement", false},
		{"validate/default disabled", "validate", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults := DefaultToolProfiles()
			if got := defaults.Permissions[tt.profile].Attachments; got != tt.wantDefault {
				t.Fatalf("%s attachments = %t, want %t", tt.profile, got, tt.wantDefault)
			}

			body := strings.Replace(string(DefaultFileContents()),
				"    "+tt.profile+":\n      attachments: "+strconv.FormatBool(tt.wantDefault),
				"    "+tt.profile+":\n      attachments: "+strconv.FormatBool(!tt.wantDefault), 1)
			cfg, err := Decode(strings.NewReader(body))
			if err != nil {
				t.Fatalf("Decode override: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate override: %v", err)
			}
			if got := cfg.ToolProfiles.Permissions[tt.profile].Attachments; got == tt.wantDefault {
				t.Fatalf("%s explicit attachment override was not retained", tt.profile)
			}
		})
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
			c.Agents.Claude.ToolCeiling = defaultTools()
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
