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
		{"legacy setting", "poll_interval_seconds: 30\n", "settings.poll_interval_minutes"},
		{"legacy nested setting", "settings:\n  poll_interval_seconds: 30\n", "settings.poll_interval_minutes"},
		{"legacy not converted", "settings:\n  poll_interval_seconds: 30\n", "not converted"},
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
		{"poll lower", "poll_interval_minutes", 1, true}, {"poll upper", "poll_interval_minutes", 180, true},
		{"poll zero", "poll_interval_minutes", 0, false}, {"poll negative", "poll_interval_minutes", -5, false},
		{"poll over", "poll_interval_minutes", 181, false}, {"concurrency lower", "max_concurrent_runs", 1, true},
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

// TestAutoSyncSettingsDefaultWhenOmitted is AC-8.1: an unconfigured
// repository gets AutoSyncBase=true, AutoSyncMaxBehind=100 and
// BlockRunOnConflict=true with no config file changes required.
func TestAutoSyncSettingsDefaultWhenOmitted(t *testing.T) {
	body := string(DefaultFileContents())
	for _, line := range []string{
		"  auto_sync_base: true\n",
		"  auto_sync_max_behind: " + strconv.Itoa(DefaultAutoSyncMaxBehind) + "\n",
		"  block_run_on_conflict: true\n",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("default file does not contain %q", line)
		}
		body = strings.Replace(body, line, "", 1)
	}

	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode with auto-sync settings omitted: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with auto-sync settings omitted: %v", err)
	}
	if !cfg.Settings.AutoSyncBase {
		t.Error("AutoSyncBase default = false, want true")
	}
	if cfg.Settings.AutoSyncMaxBehind != DefaultAutoSyncMaxBehind {
		t.Errorf("AutoSyncMaxBehind default = %d, want %d", cfg.Settings.AutoSyncMaxBehind, DefaultAutoSyncMaxBehind)
	}
	if !cfg.Settings.BlockRunOnConflict {
		t.Error("BlockRunOnConflict default = false, want true")
	}
}

// TestAutoSyncMaxBehindZeroMeansNoCap is half of AC-8.2: zero loads
// successfully and is a real, distinct value from the default rather than a
// disguised "use the default" signal.
func TestAutoSyncMaxBehindZeroMeansNoCap(t *testing.T) {
	cfg := canonicalConfig(t)
	cfg.Settings.AutoSyncMaxBehind = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with auto_sync_max_behind=0: %v", err)
	}
}

// TestAutoSyncMaxBehindNegativeIsConfigError is the other half of AC-8.2: a
// negative value fails validation and is never clamped to 0 or to the
// default (CFG-6).
func TestAutoSyncMaxBehindNegativeIsConfigError(t *testing.T) {
	cfg := canonicalConfig(t)
	cfg.Settings.AutoSyncMaxBehind = -1

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted a negative auto_sync_max_behind")
	}
	if !strings.Contains(err.Error(), "settings.auto_sync_max_behind") {
		t.Errorf("error = %v, want it to name settings.auto_sync_max_behind", err)
	}
	if cfg.Settings.AutoSyncMaxBehind != -1 {
		t.Errorf("AutoSyncMaxBehind was mutated to %d; a rejected value must never be clamped", cfg.Settings.AutoSyncMaxBehind)
	}
}

// TestAutoSyncMaxBehindNegativeFailsConfigLoad exercises the same rule
// end-to-end through Decode+Validate, the path a real config.yaml takes.
func TestAutoSyncMaxBehindNegativeFailsConfigLoad(t *testing.T) {
	err := parse(t, "settings:\n  auto_sync_max_behind: -5\n")
	if err == nil || !strings.Contains(err.Error(), "settings.auto_sync_max_behind") {
		t.Fatalf("error = %v, want it to name settings.auto_sync_max_behind", err)
	}
}

// TestAutoSyncBooleansOverrideExplicitly checks that an explicit true or
// false in YAML lands on the field rather than being swallowed by the
// default decoding merges onto (CFG-2).
func TestAutoSyncBooleansOverrideExplicitly(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		get  func(Settings) bool
		want bool
	}{
		{"auto_sync_base explicit false", "auto_sync_base: false\n", func(s Settings) bool { return s.AutoSyncBase }, false},
		{"auto_sync_base explicit true", "auto_sync_base: true\n", func(s Settings) bool { return s.AutoSyncBase }, true},
		{"block_run_on_conflict explicit false", "block_run_on_conflict: false\n", func(s Settings) bool { return s.BlockRunOnConflict }, false},
		{"block_run_on_conflict explicit true", "block_run_on_conflict: true\n", func(s Settings) bool { return s.BlockRunOnConflict }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Decode(strings.NewReader("settings:\n  " + tt.yaml))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got := tt.get(cfg.Settings); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAutoSyncMaxBehindExplicitZeroDecodesDistinctFromDefault closes a gap in
// AC-8.2 the existing zero-means-no-cap test does not reach: it goes through
// [Decode] itself, rather than assigning the struct field directly, to check
// that an explicitly written auto_sync_max_behind: 0 lands on the field as a
// real zero instead of being merged back onto the 100 default the decoder
// pre-populates the struct with (CFG-2, the same distinguishing behavior
// poll_interval_seconds relies on elsewhere in this package).
func TestAutoSyncMaxBehindExplicitZeroDecodesDistinctFromDefault(t *testing.T) {
	body := strings.Replace(string(DefaultFileContents()),
		"  auto_sync_max_behind: "+strconv.Itoa(DefaultAutoSyncMaxBehind)+"\n",
		"  auto_sync_max_behind: 0\n", 1)

	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode with explicit auto_sync_max_behind=0: %v", err)
	}
	if cfg.Settings.AutoSyncMaxBehind != 0 {
		t.Fatalf("AutoSyncMaxBehind = %d, want 0 (an explicit zero must not be merged back onto the %d default)",
			cfg.Settings.AutoSyncMaxBehind, DefaultAutoSyncMaxBehind)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with explicit auto_sync_max_behind=0: %v", err)
	}
}

// TestAutoSyncSettingsOverrideIsPerField checks that writing an explicit
// value for one of the three auto-sync keys does not disturb its two
// siblings within the same decode: each of AutoSyncBase, AutoSyncMaxBehind
// and BlockRunOnConflict must be independently overridable rather than the
// three being decoded, or defaulted, as a group.
func TestAutoSyncSettingsOverrideIsPerField(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		wantBase      bool
		wantMaxBehind int
		wantBlock     bool
	}{
		{
			name:          "only auto_sync_base overridden/siblings keep defaults",
			yaml:          "settings:\n  auto_sync_base: false\n",
			wantBase:      false,
			wantMaxBehind: DefaultAutoSyncMaxBehind,
			wantBlock:     true,
		},
		{
			name:          "only auto_sync_max_behind overridden/siblings keep defaults",
			yaml:          "settings:\n  auto_sync_max_behind: 5\n",
			wantBase:      true,
			wantMaxBehind: 5,
			wantBlock:     true,
		},
		{
			name:          "only block_run_on_conflict overridden/siblings keep defaults",
			yaml:          "settings:\n  block_run_on_conflict: false\n",
			wantBase:      true,
			wantMaxBehind: DefaultAutoSyncMaxBehind,
			wantBlock:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Decode(strings.NewReader(tt.yaml))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if cfg.Settings.AutoSyncBase != tt.wantBase {
				t.Errorf("AutoSyncBase = %v, want %v", cfg.Settings.AutoSyncBase, tt.wantBase)
			}
			if cfg.Settings.AutoSyncMaxBehind != tt.wantMaxBehind {
				t.Errorf("AutoSyncMaxBehind = %d, want %d", cfg.Settings.AutoSyncMaxBehind, tt.wantMaxBehind)
			}
			if cfg.Settings.BlockRunOnConflict != tt.wantBlock {
				t.Errorf("BlockRunOnConflict = %v, want %v", cfg.Settings.BlockRunOnConflict, tt.wantBlock)
			}
		})
	}
}

func defaultSettingValue(key string) int {
	switch key {
	case "poll_interval_minutes":
		return DefaultPollIntervalMinutes
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

func TestWebhookListen(t *testing.T) {
	cases := []struct {
		name, listen string
		ok           bool
	}{
		{"default", "127.0.0.1:8787", true},
		{"explicit", "127.0.0.1:9000", true},
		{"hostname", "localhost:9000", true},
		{"ipv6", "[::1]:9000", true},
		{"off", "", true},
		{"no port", "127.0.0.1", false},
		{"bad port", "127.0.0.1:http", false},
		{"port zero", "127.0.0.1:0", false},
		{"port too big", "127.0.0.1:70000", false},
		{"no host", ":8787", false},
		{"garbage", "not an address", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(string(DefaultFileContents()), `listen: "127.0.0.1:8787"`, "listen: "+strconv.Quote(tc.listen), 1)
			err := parse(t, body)
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "webhook.listen") {
					t.Fatalf("err = %v, want one naming webhook.listen", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			cfg, _ := Decode(strings.NewReader(body))
			if cfg.Webhook.Listen != tc.listen {
				t.Errorf("Listen = %q, want %q", cfg.Webhook.Listen, tc.listen)
			}
		})
	}
}

func TestWebhookListenDefaultsWhenTheKeyIsAbsent(t *testing.T) {
	body := strings.Replace(string(DefaultFileContents()), "webhook:\n", "# webhook:\n", 1)
	body = strings.Replace(body, `  listen: "127.0.0.1:8787"`, "", 1)
	body = strings.Replace(body, `  public_url: ""`, "", 1)
	cfg, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Webhook.Listen != DefaultWebhookListen {
		t.Errorf("Listen = %q, want the default %q", cfg.Webhook.Listen, DefaultWebhookListen)
	}
}

func TestDefaultFileContentsDocumentsTheWebhookListener(t *testing.T) {
	out := string(DefaultFileContents())
	if !strings.Contains(out, "listen: \"127.0.0.1:8787\"") || !strings.Contains(out, "/litefactory/webhook") {
		t.Errorf("the template does not document webhook.listen:\n%s", out)
	}
}

func TestWebhookPublicURL(t *testing.T) {
	cases := []struct {
		name, url string
		ok        bool
	}{
		{"empty", "", true},
		{"https", "https://hooks.example.com/litefactory/webhook", true},
		{"http", "http://192.168.1.5:8787/litefactory/webhook", true},
		{"relative", "/litefactory/webhook", false},
		{"no scheme", "hooks.example.com/x", false},
		{"ftp", "ftp://hooks.example.com/x", false},
		{"no host", "https:///x", false},
		{"garbage", "ht!tp://%zz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.Replace(string(DefaultFileContents()), `public_url: ""`, "public_url: "+strconv.Quote(tc.url), 1)
			err := parse(t, body)
			if !tc.ok {
				if err == nil || !strings.Contains(err.Error(), "webhook.public_url") {
					t.Fatalf("err = %v, want one naming webhook.public_url", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			cfg, _ := Decode(strings.NewReader(body))
			if cfg.Webhook.PublicURL != tc.url {
				t.Errorf("PublicURL = %q, want %q", cfg.Webhook.PublicURL, tc.url)
			}
		})
	}
}

func TestDefaultFileContentsDocumentsThePublicURL(t *testing.T) {
	if out := string(DefaultFileContents()); !strings.Contains(out, `public_url: ""`) {
		t.Errorf("the template does not document webhook.public_url:\n%s", out)
	}
}
