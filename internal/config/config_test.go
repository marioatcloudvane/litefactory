package config

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func parse(t *testing.T, body string) error {
	t.Helper()
	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		return err
	}
	return cfg.Validate()
}

func TestCanonicalRootsAndLegacyDiagnostics(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"legacy setting", "poll_interval_seconds: 30\n", "settings.poll_interval_seconds"},
		{"legacy provider", "github: {}\n", "providers.github"},
		{"legacy agent", "claude: {}\n", "agents.claude"},
		{"unknown root", "mystery: true\n", `unknown key "mystery"`},
		{"unsupported provider", "providers:\n  gitlab: {}\n", `unknown key "gitlab"`},
		{"literal model", "agents:\n  claude:\n    model: opus\n", "literal per-agent models are no longer supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parse(t, tt.body)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeRejectsAdditionalYAMLDocuments(t *testing.T) {
	_, err := Decode(strings.NewReader("agents:\n  claude: {}\n---\nsettings:\n  log_level: debug\n"))
	if err == nil || !strings.Contains(err.Error(), "document") {
		t.Fatalf("Decode additional document error = %v", err)
	}
}

func TestCanonicalConfigurationRejectsLiteralSecrets(t *testing.T) {
	body := strings.Replace(string(DefaultFileContents()), `default_base_branch: ""`, `default_base_branch: "ghp_123456789012345678901234567890123456"`, 1)
	err := parse(t, body)
	if !errors.Is(err, ErrLiteralSecret) || strings.Contains(err.Error(), "ghp_123456") {
		t.Fatalf("literal-secret error = %v", err)
	}
}

func TestSettingsValidation(t *testing.T) {
	tests := []struct {
		name, key string
		value     int
		ok        bool
	}{
		{"poll lower", "poll_interval_seconds", 10, true}, {"poll upper", "poll_interval_seconds", 60, true},
		{"poll zero", "poll_interval_seconds", 0, false}, {"concurrency lower", "max_concurrent_runs", 1, true},
		{"concurrency upper", "max_concurrent_runs", 16, true}, {"concurrency zero", "max_concurrent_runs", 0, false},
		{"retain positive", "worktree_retain_days", 14, true}, {"retain zero", "worktree_retain_days", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Replace(string(DefaultFileContents()), tt.key+": "+strconv.Itoa(defaultSettingValue(tt.key)), tt.key+": "+strconv.Itoa(tt.value), 1)
			err := parse(t, body)
			if (err == nil) != tt.ok {
				t.Fatalf("error = %v, ok=%v", err, tt.ok)
			}
		})
	}
}

func TestAttachmentCeilingsDefaultForCodexOnlyConfiguration(t *testing.T) {
	cfg := canonicalConfig(t)
	cfg.Agents.Claude = nil
	cfg.agentOrder = []Kind{KindCodex}
	for name, profile := range cfg.ToolProfiles.Permissions {
		profile.Claude = nil
		cfg.ToolProfiles.Permissions[name] = profile
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	want := Defaults().Settings
	if cfg.Settings.AttachmentMaxImageBytes != want.AttachmentMaxImageBytes ||
		cfg.Settings.AttachmentMaxDocumentBytes != want.AttachmentMaxDocumentBytes ||
		cfg.Settings.AttachmentMaxTotalBytes != want.AttachmentMaxTotalBytes ||
		cfg.Settings.AttachmentMaxCount != want.AttachmentMaxCount {
		t.Fatalf("Codex-only attachment ceilings = %+v, want %+v", cfg.Settings, want)
	}
}

func TestAttachmentCeilingOmissionUsesDefaults(t *testing.T) {
	body := string(DefaultFileContents())
	for _, line := range []string{
		"  attachment_max_image_bytes: " + strconv.FormatInt(DefaultAttachmentMaxImageBytes, 10) + "\n",
		"  attachment_max_document_bytes: " + strconv.FormatInt(DefaultAttachmentMaxDocumentBytes, 10) + "\n",
		"  attachment_max_total_bytes: " + strconv.FormatInt(DefaultAttachmentMaxTotalBytes, 10) + "\n",
		"  attachment_max_count: " + strconv.Itoa(DefaultAttachmentMaxCount) + "\n",
	} {
		body = strings.Replace(body, line, "", 1)
	}

	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode omitted ceilings: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate omitted ceilings: %v", err)
	}
	want := Defaults().Settings
	if cfg.Settings.AttachmentMaxImageBytes != want.AttachmentMaxImageBytes ||
		cfg.Settings.AttachmentMaxDocumentBytes != want.AttachmentMaxDocumentBytes ||
		cfg.Settings.AttachmentMaxTotalBytes != want.AttachmentMaxTotalBytes ||
		cfg.Settings.AttachmentMaxCount != want.AttachmentMaxCount {
		t.Fatalf("omitted attachment ceilings = %+v, want %+v", cfg.Settings, want)
	}
}

func TestAttachmentCeilingsAcceptCanonicalBounds(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"image lower/accepted", func(s *Settings) { s.AttachmentMaxImageBytes = MinAttachmentBytes }},
		{"image upper/accepted", func(s *Settings) { s.AttachmentMaxImageBytes = MaxAttachmentBytes }},
		{"document lower/accepted", func(s *Settings) { s.AttachmentMaxDocumentBytes = MinAttachmentBytes }},
		{"document upper/accepted", func(s *Settings) { s.AttachmentMaxDocumentBytes = MaxAttachmentBytes }},
		{"total lower/accepted", func(s *Settings) { s.AttachmentMaxTotalBytes = MinAttachmentBytes }},
		{"total upper/accepted", func(s *Settings) { s.AttachmentMaxTotalBytes = MaxAttachmentBytes }},
		{"count lower/accepted", func(s *Settings) { s.AttachmentMaxCount = MinAttachmentMaxCount }},
		{"count upper/accepted", func(s *Settings) { s.AttachmentMaxCount = MaxAttachmentMaxCount }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := canonicalConfig(t)
			tt.mutate(&cfg.Settings)

			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate canonical bound: %v", err)
			}
		})
	}
}

func TestAttachmentCeilingsReportAllCanonicalErrors(t *testing.T) {
	cfg := canonicalConfig(t)
	cfg.Settings.AttachmentMaxImageBytes = 0
	cfg.Settings.AttachmentMaxDocumentBytes = MinAttachmentBytes - 1
	cfg.Settings.AttachmentMaxTotalBytes = MaxAttachmentBytes + 1
	cfg.Settings.AttachmentMaxCount = 0

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted invalid attachment ceilings")
	}
	for _, path := range []string{
		"settings.attachment_max_image_bytes",
		"settings.attachment_max_document_bytes",
		"settings.attachment_max_total_bytes",
		"settings.attachment_max_count",
	} {
		if !strings.Contains(err.Error(), path) {
			t.Errorf("aggregate error %q omits %q", err, path)
		}
	}
}

func TestAttachmentAndSandboxLegacyLocationsAreRejected(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{"Claude image ceiling/rejected", "agents:\n  claude:\n    attachment_max_image_bytes: 1024\n", "attachment_max_image_bytes"},
		{"Claude document ceiling/rejected", "agents:\n  claude:\n    attachment_max_document_bytes: 1024\n", "attachment_max_document_bytes"},
		{"Claude total ceiling/rejected", "agents:\n  claude:\n    attachment_max_total_bytes: 1024\n", "attachment_max_total_bytes"},
		{"Claude count ceiling/rejected", "agents:\n  claude:\n    attachment_max_count: 1\n", "attachment_max_count"},
		{"Codex default sandbox/rejected", "agents:\n  codex:\n    default_sandbox: read-only\n", "default_sandbox"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tt.body))

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode error = %v, want strict rejection naming %q", err, tt.want)
			}
		})
	}
}

func defaultSettingValue(key string) int {
	switch key {
	case "poll_interval_seconds":
		return DefaultPollIntervalSeconds
	case "max_concurrent_runs":
		return DefaultMaxConcurrentRuns
	default:
		return DefaultWorktreeRetainDays
	}
}

func TestTierValidationAndResolution(t *testing.T) {
	cfg := Defaults()
	claude, codex := DefaultClaude(), DefaultCodex()
	cfg.Agents = Agents{Claude: &claude, Codex: &codex}
	cfg.agentOrder = []Kind{KindClaude, KindCodex}
	cfg.ToolProfiles = DefaultToolProfiles()
	model, ok := cfg.Tiers.ResolveModel(KindClaude, claude.ModelTier)
	if !ok || model != DefaultClaudeModel {
		t.Fatalf("resolved (%q,%v), want (%q,true)", model, ok, DefaultClaudeModel)
	}
	delete(cfg.Tiers.ModelByTier[KindClaude], TierBalanced)
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires a non-empty model") {
		t.Fatalf("error=%v", err)
	}
	var unknown *UnknownTierError
	err = parse(t, "agents:\n  claude:\n    model_tier: quick\n")
	if !errors.As(err, &unknown) {
		t.Fatalf("error %v does not wrap UnknownTierError", err)
	}
}
