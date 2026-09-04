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
