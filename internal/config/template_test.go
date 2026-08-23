package config

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	if diff := cmp.Diff(Defaults(), *cfg); diff != "" {
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
	if cfg.PollIntervalSeconds != DefaultPollIntervalSeconds {
		t.Errorf("PollIntervalSeconds = %d, want %d", cfg.PollIntervalSeconds, DefaultPollIntervalSeconds)
	}
	// An empty settings_path is filled in from $LF_HOME, so the harness never
	// has to decide what "empty" means.
	if cfg.Claude.SettingsPath != paths.SettingsPath() {
		t.Errorf("Claude.SettingsPath = %q, want %q", cfg.Claude.SettingsPath, paths.SettingsPath())
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
	body := "github:\n  token_env: ghp_0123456789abcdefghijklmnopqrstuvwx\n"
	if err := os.WriteFile(paths.ConfigPath(), []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	_, err := Load(paths)
	if !errors.Is(err, ErrLiteralSecret) {
		t.Errorf("errors.Is(err, ErrLiteralSecret) = false, want true (err = %v)", err)
	}
}
