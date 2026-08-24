package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSettingsIsValidJSONAndDeniesTheObviousTargets(t *testing.T) {
	const home = "/Users/tester/.litefactory"
	data, err := DefaultSettings(home)
	if err != nil {
		t.Fatalf("DefaultSettings: %v", err)
	}

	var settings claudeSettings
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		t.Fatalf("settings do not round-trip: %v", err)
	}

	if settings.Permissions.DefaultMode != "default" {
		t.Errorf("defaultMode = %q; a headless run has nobody to prompt, but bypass is not the answer",
			settings.Permissions.DefaultMode)
	}
	if settings.EnableAllProjectMCPServers {
		t.Error("project MCP servers are enabled; a run must not pick up the branch's .mcp.json")
	}

	deny := strings.Join(settings.Permissions.Deny, "\n")
	// $LF_HOME holds daemon.log, which is unredacted by design (S-7 applies to
	// forge-bound bodies, never to the local copy).
	if !strings.Contains(deny, home) {
		t.Errorf("$LF_HOME is not denied:\n%s", deny)
	}
	for _, want := range []string{".ssh", ".aws", ".config/gh"} {
		if !strings.Contains(deny, want) {
			t.Errorf("%s is not denied:\n%s", want, deny)
		}
	}
}

func TestDefaultSettingsRejectsARelativeHome(t *testing.T) {
	if _, err := DefaultSettings("relative"); err == nil {
		t.Fatal("DefaultSettings accepted a relative $LF_HOME")
	} else if !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("error %v does not unwrap to ErrInvalidOptions", err)
	}
}

func TestWriteSettingsReplacesAndEnsureSettingsDoesNot(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "claude-settings.json")

	// EnsureSettings creates.
	written, err := EnsureSettings(path, home)
	if err != nil {
		t.Fatalf("EnsureSettings: %v", err)
	}
	if !written {
		t.Fatal("EnsureSettings reported no write for a missing file")
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// EnsureSettings leaves an existing file alone — an operator may have
	// pointed claude.settings_path at something they maintain.
	const custom = `{"permissions":{"deny":[]}}`
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	written, err = EnsureSettings(path, home)
	if err != nil {
		t.Fatalf("EnsureSettings: %v", err)
	}
	if written {
		t.Fatal("EnsureSettings overwrote an existing file")
	}
	if got, _ := os.ReadFile(path); string(got) != custom {
		t.Fatalf("EnsureSettings rewrote the file: %s", got)
	}

	// WriteSettings is litefactory reclaiming the file, which is what `init`
	// and a startup refresh want.
	if err := WriteSettings(path, home); err != nil {
		t.Fatalf("WriteSettings: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(first) {
		t.Fatal("WriteSettings did not restore litefactory's own document")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings mode = %o, want 600", perm)
	}
}

func TestSettingsPathMustBeAbsolute(t *testing.T) {
	if err := WriteSettings("claude-settings.json", "/home"); err == nil {
		t.Error("WriteSettings accepted a relative path")
	}
	if _, err := EnsureSettings("claude-settings.json", "/home"); err == nil {
		t.Error("EnsureSettings accepted a relative path")
	}
}

func TestWriteFileAtomicCreatesMissingDirectories(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "c.json")
	if err := writeFileAtomic(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "{}\n" {
		t.Fatalf("read back %q, %v", got, err)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o, want 700", perm)
	}
}
