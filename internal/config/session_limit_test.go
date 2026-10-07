package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDecodeAutoResumeOnSessionLimit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		setting string
		want    bool
		wantErr bool
	}{
		{name: "absent/disabled"},
		{name: "false/disabled", setting: "auto_resume_on_session_limit: false"},
		{name: "true/enabled", setting: "auto_resume_on_session_limit: true", want: true},
		{name: "invalid scalar/rejected", setting: "auto_resume_on_session_limit: sometimes", wantErr: true},
		{name: "integer/rejected", setting: "auto_resume_on_session_limit: 1", wantErr: true},
		{name: "sequence/rejected", setting: "auto_resume_on_session_limit: [true]", wantErr: true},
		{name: "mapping/rejected", setting: "auto_resume_on_session_limit: {enabled: true}", wantErr: true},
		{name: "unknown setting/rejected", setting: "auto_resume_on_session_limits: true", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "agents:\n  claude: {}\n"
			if tt.setting != "" {
				body += "settings:\n  " + tt.setting + "\n"
			}

			cfg, err := Decode(strings.NewReader(body))

			if tt.wantErr {
				if err == nil {
					t.Fatal("Decode accepted malformed or unknown setting")
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if cfg.Settings.AutoResumeOnSessionLimit != tt.want {
				t.Errorf("auto resume = %t, want %t", cfg.Settings.AutoResumeOnSessionLimit, tt.want)
			}
		})
	}
}

func TestDefaultFileAutoResumeOnSessionLimit(t *testing.T) {
	t.Parallel()
	var raw struct {
		Settings struct {
			AutoResume *bool `yaml:"auto_resume_on_session_limit"`
		} `yaml:"settings"`
	}
	body := DefaultFileContents()

	if err := yaml.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode template structure: %v", err)
	}
	if raw.Settings.AutoResume == nil || *raw.Settings.AutoResume {
		t.Fatal("template must explicitly include auto_resume_on_session_limit: false")
	}
	cfg, err := Decode(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("Decode template: %v", err)
	}
	if cfg.Settings.AutoResumeOnSessionLimit || Defaults().Settings.AutoResumeOnSessionLimit {
		t.Fatal("template and defaults must disable automatic recovery")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate template: %v", err)
	}
}

func TestDecodeValidateAutoStart(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		section string
		want    bool
		wantErr bool
	}{
		{name: "absent/off"},
		{name: "false/off", section: "auto_start: false"},
		{name: "true/on", section: "auto_start: true", want: true},
		{name: "string/rejected", section: `auto_start: "true"`, wantErr: true},
		{name: "yes/rejected", section: "auto_start: yes", wantErr: true},
		{name: "null/rejected", section: "auto_start: null", wantErr: true},
		{name: "integer/rejected", section: "auto_start: 1", wantErr: true},
		{name: "unknown/rejected", section: "auto_starts: true", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := "agents:\n  claude: {}\n"
			if tt.section != "" {
				body += "validate:\n  " + tt.section + "\n"
			}
			cfg, err := Decode(strings.NewReader(body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("Decode accepted a malformed validate.auto_start")
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if cfg.ValidateMode.AutoStart != tt.want {
				t.Errorf("auto start = %t, want %t", cfg.ValidateMode.AutoStart, tt.want)
			}
		})
	}
}

func TestDefaultFileValidateAutoStart(t *testing.T) {
	t.Parallel()
	var raw struct {
		Validate struct {
			AutoStart *bool `yaml:"auto_start"`
		} `yaml:"validate"`
	}
	if err := yaml.Unmarshal([]byte(DefaultFileContents()), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Validate.AutoStart == nil || *raw.Validate.AutoStart || Defaults().ValidateMode.AutoStart {
		t.Fatal("the template must include validate.auto_start: false, the default")
	}
}
