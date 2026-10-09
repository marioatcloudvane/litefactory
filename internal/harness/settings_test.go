package harness

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
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

	// The document and argv must agree, so this is pinned to the same constant
	// the command line is built from rather than to a literal.
	if settings.Permissions.DefaultMode != PermissionMode {
		t.Errorf("defaultMode = %q, want %q; the settings document and --permission-mode must not disagree",
			settings.Permissions.DefaultMode, PermissionMode)
	}
	// Whatever the mode is, it is never the one that discards the deny rules
	// below. bypassPermissions is the moral equivalent of the flag
	// config.forbiddenFlag names.
	if settings.Permissions.DefaultMode == "bypassPermissions" {
		t.Error("defaultMode is bypassPermissions; the deny rules are the boundary and this discards them")
	}
	if settings.EnableAllProjectMCPServers {
		t.Error("project MCP servers are enabled; a run must not pick up the branch's .mcp.json")
	}

	deny := strings.Join(settings.Permissions.Deny, "\n")
	for _, want := range []string{".ssh", ".aws", ".config/gh"} {
		if !strings.Contains(deny, want) {
			t.Errorf("%s is not denied:\n%s", want, deny)
		}
	}
	// litefactory makes the commit and the push; no agent does. Under
	// PermissionMode "auto" an absent allow-rule is a question for a classifier
	// rather than a refusal, so this ban only holds if it is written as a deny.
	for _, want := range []string{"git add", "git commit", "git push", "git stash"} {
		if !strings.Contains(deny, want) {
			t.Errorf("%q is not denied, so an agent could make its own commit:\n%s", want, deny)
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

// A Claude Validate reviewer works in a checkout under
// $LF_HOME/validate-scratch and every run reads its skills from
// $LF_HOME/bundles, so neither may be denied. daemon.log (unredacted by
// design; S-7 applies to forge-bound bodies, never to the local copy), the raw
// run logs, the database, config.yaml and the credential-bearing paths must
// stay denied.
func TestDefaultSettingsDeniesHomeStateButNotCheckoutOrBundle(t *testing.T) {
	const home = "/Users/tester/.litefactory"
	data, err := DefaultSettings(home)
	if err != nil {
		t.Fatalf("DefaultSettings: %v", err)
	}
	var settings claudeSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings do not decode: %v", err)
	}
	deny := settings.Permissions.Deny
	joined := strings.Join(deny, "\n")

	for _, want := range []string{
		"Read(//" + home + "/logs/**)",
		"Read(//" + home + "/litefactory.db*)",
		"Read(//" + home + "/config.yaml)",
		"Read(//" + home + "/claude-settings.json)",
		"Read(//" + home + "/claude-home/**)",
		"Read(//" + home + "/secrets/**)",
		"Read(//" + home + "/s/**)",
		"Read(//" + home + "/dc/**)",
	} {
		if !slices.Contains(deny, want) {
			t.Errorf("deny lacks %s:\n%s", want, joined)
		}
	}

	readable := []string{
		home + "/validate-scratch/validate-suite-x/tree/file.go",
		home + "/bundles/github/o/r/1/run-1/plugin/skills/s/SKILL.md",
	}
	for _, rule := range deny {
		if rule == "Read(//"+home+"/**)" {
			t.Errorf("deny still holds the blanket $LF_HOME rule %s", rule)
		}
		if !strings.HasPrefix(rule, "Read(//") || !strings.HasSuffix(rule, ")") {
			continue
		}
		pattern := strings.TrimSuffix(strings.TrimPrefix(rule, "Read(//"), ")")
		for _, file := range readable {
			if denyPatternCovers(pattern, file) {
				t.Errorf("deny rule %s covers %s", rule, file)
			}
		}
	}

	// The patterns must still match what they protect.
	for _, file := range []string{
		home + "/logs/daemon.log",
		home + "/logs/runs/1.jsonl",
		home + "/litefactory.db-wal",
		home + "/config.yaml",
	} {
		covered := false
		for _, rule := range deny {
			if strings.HasPrefix(rule, "Read(//") && denyPatternCovers(strings.TrimSuffix(strings.TrimPrefix(rule, "Read(//"), ")"), file) {
				covered = true
			}
		}
		if !covered {
			t.Errorf("no deny rule covers %s:\n%s", file, joined)
		}
	}
}

// denyPatternCovers reports whether a gitignore-style absolute pattern (only
// a trailing "/**" or a trailing "*" on the last segment) matches file.
func denyPatternCovers(pattern, file string) bool {
	if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
		return file == dir || strings.HasPrefix(file, dir+"/")
	}
	matched, err := path.Match(pattern, file)
	return err == nil && matched
}
