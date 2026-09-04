package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

// TestDefaultFileMatchesDefaults is the anti-drift check. The shipped
// config.yaml is what a user reads to learn what litefactory does; if it
// disagrees with the values the code actually uses, it is worse than no file
// at all.
func TestDefaultFileMatchesDefaults(t *testing.T) {
	cfg, err := Decode(bytes.NewReader(DefaultFileContents()))
	if err != nil {
		t.Fatalf("the default config file does not decode: %v", err)
	}
	want := Defaults()
	claude, codex := DefaultClaude(), DefaultCodex()
	want.Agents = Agents{Claude: &claude, Codex: &codex}
	want.Agents.Codex.WritableRoots = []string{}
	want.Providers.GitHub.TokenKeychain = Keychain{Service: "litefactory", Account: "github-bot"}
	want.ToolProfiles = DefaultToolProfiles()
	want.agentOrder = []Kind{KindClaude, KindCodex}
	if diff := cmp.Diff(want, *cfg, cmp.AllowUnexported(Config{})); diff != "" {
		t.Errorf("the default config file has drifted from Defaults() (-want +got):\n%s", diff)
	}
}

// TestDefaultFileValidates catches the shipped file failing its own rules —
// the worst possible first-run experience.
func TestDefaultFileValidates(t *testing.T) {
	cfg, err := Decode(bytes.NewReader(DefaultFileContents()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the default config file does not validate: %v", err)
	}
}

// TestDefaultFileHasNoCredential guards the file we ourselves write against
// the rule we impose on everyone else.
func TestDefaultFileHasNoCredential(t *testing.T) {
	if class, found := ClassifySecret(string(DefaultFileContents())); found {
		t.Errorf("the default config file contains something shaped like a credential (%s)", class)
	}
}

// TestDefaultFileIsStructurallyComplete checks key presence independently of
// Decode's merge-on-default behavior. Without this check, a supported defaulted
// key can disappear from the operator-facing reference while the anti-drift
// comparison still passes.
func TestDefaultFileIsStructurallyComplete(t *testing.T) {
	type rawSettings struct {
		AttachmentMaxImageBytes    *int64 `yaml:"attachment_max_image_bytes"`
		AttachmentMaxDocumentBytes *int64 `yaml:"attachment_max_document_bytes"`
		AttachmentMaxTotalBytes    *int64 `yaml:"attachment_max_total_bytes"`
		AttachmentMaxCount         *int   `yaml:"attachment_max_count"`
	}
	type rawProfile struct {
		Attachments *bool              `yaml:"attachments"`
		Claude      *ToolProfileClaude `yaml:"claude"`
		Codex       *ToolProfileCodex  `yaml:"codex"`
	}
	var raw struct {
		Settings rawSettings `yaml:"settings"`
		Agents   struct {
			Claude map[string]yaml.Node `yaml:"claude"`
			Codex  map[string]yaml.Node `yaml:"codex"`
		} `yaml:"agents"`
		ToolProfiles struct {
			Permissions map[string]rawProfile `yaml:"permissions"`
		} `yaml:"tool_profiles"`
	}
	if err := yaml.Unmarshal(DefaultFileContents(), &raw); err != nil {
		t.Fatalf("decode generated YAML structure: %v", err)
	}

	defaults := Defaults().Settings
	ceilings := []struct {
		name string
		got  *int64
		want int64
	}{
		{"attachment_max_image_bytes", raw.Settings.AttachmentMaxImageBytes, defaults.AttachmentMaxImageBytes},
		{"attachment_max_document_bytes", raw.Settings.AttachmentMaxDocumentBytes, defaults.AttachmentMaxDocumentBytes},
		{"attachment_max_total_bytes", raw.Settings.AttachmentMaxTotalBytes, defaults.AttachmentMaxTotalBytes},
	}
	for _, ceiling := range ceilings {
		if ceiling.got == nil {
			t.Errorf("generated settings omit %s", ceiling.name)
		} else if *ceiling.got != ceiling.want {
			t.Errorf("generated %s = %d, want %d", ceiling.name, *ceiling.got, ceiling.want)
		}
	}
	if raw.Settings.AttachmentMaxCount == nil {
		t.Error("generated settings omit attachment_max_count")
	} else if *raw.Settings.AttachmentMaxCount != defaults.AttachmentMaxCount {
		t.Errorf("generated attachment_max_count = %d, want %d", *raw.Settings.AttachmentMaxCount, defaults.AttachmentMaxCount)
	}

	for _, name := range canonicalPermissionProfiles {
		profile, ok := raw.ToolProfiles.Permissions[name]
		if !ok {
			t.Errorf("generated permissions omit %s profile", name)
			continue
		}
		if profile.Attachments == nil {
			t.Errorf("generated %s profile omits attachments", name)
		}
		if profile.Claude == nil || profile.Codex == nil {
			t.Errorf("generated %s profile must include both agent authorities", name)
		}
	}
	if _, found := raw.Agents.Codex["default_sandbox"]; found {
		t.Error("generated agents.codex contains removed default_sandbox key")
	}
	for _, key := range []string{
		"attachment_max_image_bytes",
		"attachment_max_document_bytes",
		"attachment_max_total_bytes",
		"attachment_max_count",
	} {
		if _, found := raw.Agents.Claude[key]; found {
			t.Errorf("generated agents.claude contains moved %s key", key)
		}
	}
}

// TestDefaultFileDocumentsTheTwoToolKeys pins the comments that stop a future
// reader deleting claude.tools or claude.allowed_tools as a duplicate of the
// other. The keys look redundant and are not; the file is where that gets
// explained.
func TestDefaultFileDocumentsTheTwoToolKeys(t *testing.T) {
	contents := string(DefaultFileContents())
	for _, want := range []string{"--tools", "--allowedTools", "--settings", "NOT the boundary"} {
		if !strings.Contains(contents, want) {
			t.Errorf("the default config file no longer mentions %q", want)
		}
	}
}

// TestDefaultFileDocumentsTheConcurrencyCeiling is AC-PAR-6.2. The shipped
// file is where an operator learns what max_concurrent_runs costs them, so it
// has to carry the range, and it has to say that the default is a placeholder
// rather than a number anyone measured — otherwise the first reader to quote
// it will quote it as evidence.
//
// The range and the value are rendered from the consts through a positional
// fmt.Sprintf, where inserting an argument mid-list shifts every later one, so
// they are asserted here as well as through TestDefaultFileMatchesDefaults:
// two arguments of the same type can swap without the decode noticing.
func TestDefaultFileDocumentsTheConcurrencyCeiling(t *testing.T) {
	contents := string(DefaultFileContents())

	wantRange := fmt.Sprintf("valid range: %d..%d", MinMaxConcurrentRuns, MaxMaxConcurrentRuns)
	wantValue := fmt.Sprintf("max_concurrent_runs: %d", DefaultMaxConcurrentRuns)
	for _, want := range []string{wantValue, wantRange} {
		if !strings.Contains(contents, want) {
			t.Errorf("the default config file does not contain %q", want)
		}
	}
}

func TestWriteDefaultCreatesConfig(t *testing.T) {
	paths := NewPaths(t.TempDir())

	created, err := WriteDefault(paths)
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if !created {
		t.Fatal("WriteDefault reported no file was created")
	}
	if _, err := os.Stat(paths.ConfigPath()); err != nil {
		t.Fatalf("stat config: %v", err)
	}
}

// TestWriteDefaultNeverOverwrites is AC-0.7: re-running init must not throw
// away a config the user has edited.
func TestWriteDefaultNeverOverwrites(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}

	const edited = "poll_interval_seconds: 45\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(edited), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	created, err := WriteDefault(paths)
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if created {
		t.Error("WriteDefault reported it created a file that already existed")
	}

	got, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != edited {
		t.Errorf("config.yaml was overwritten:\n%s", got)
	}
}

// TestWriteDefaultCreatesHome covers init on a genuinely clean machine, where
// $LF_HOME does not exist yet.
func TestWriteDefaultCreatesHome(t *testing.T) {
	paths := NewPaths(t.TempDir() + "/fresh")

	if _, err := WriteDefault(paths); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := os.Stat(paths.Home()); err != nil {
		t.Fatalf("stat home: %v", err)
	}
}

func TestLoadReadsTheWrittenFile(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if _, err := WriteDefault(paths); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	cfg, err := Load(paths)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Settings.PollIntervalSeconds != DefaultPollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want %d", cfg.Settings.PollIntervalSeconds, DefaultPollIntervalSeconds)
	}
	// An empty settings_path is filled in from $LF_HOME, so the harness never
	// has to decide what "empty" means.
	if cfg.Agents.Claude.SettingsPath != paths.SettingsPath() {
		t.Errorf("Claude.SettingsPath = %q, want %q", cfg.Agents.Claude.SettingsPath, paths.SettingsPath())
	}
}

// TestLoadWithoutConfigSaysToRunInit turns "open ...: no such file or
// directory" into the one instruction that fixes it.
func TestLoadWithoutConfigSaysToRunInit(t *testing.T) {
	_, err := Load(NewPaths(t.TempDir()))
	if err == nil {
		t.Fatal("Load succeeded with no config file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(err, fs.ErrNotExist) = false, want true (err = %v)", err)
	}
	var notInit *NotInitializedError
	if !errors.As(err, &notInit) {
		t.Fatalf("errors.As(err, *NotInitializedError) = false, want true (err = %v)", err)
	}
	if !strings.Contains(err.Error(), "litefactory init") {
		t.Errorf("error = %q, want it to name the command that fixes it", err)
	}
}

// TestLoadNamesTheFile: a validation failure has to say which file to edit,
// because $LF_HOME is not where most people would look first.
func TestLoadNamesTheFile(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	if err := os.WriteFile(paths.ConfigPath(), []byte("poll_interval_seconds: 5\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := Load(paths)
	if err == nil {
		t.Fatal("Load accepted an out-of-range interval")
	}
	if !strings.Contains(err.Error(), paths.ConfigPath()) {
		t.Errorf("error = %q, want it to name %q", err, paths.ConfigPath())
	}
}

// TestLoadPreservesTheSecretSentinel checks the wrapping in Load does not
// break the exit-3 decision the command layer makes with errors.Is.
func TestLoadPreservesTheSecretSentinel(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.EnsureHome(); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	body := "providers:\n  github:\n    token_env: ghp_0123456789abcdefghijklmnopqrstuvwx\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := Load(paths)
	if !errors.Is(err, ErrLiteralSecret) {
		t.Errorf("errors.Is(err, ErrLiteralSecret) = false, want true (err = %v)", err)
	}
}
