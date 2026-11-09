package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamSourceDefaultsAndDecode(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "settings: {}\n", string(DefaultFileContents())} {
		cfg, err := Decode(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Settings.TeamSource != "" {
			t.Fatalf("source = %q", cfg.Settings.TeamSource)
		}
	}
	cfg, err := Decode(strings.NewReader("settings:\n  team_source: https://example.test/teams/teams.json\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.TeamSource != "https://example.test/teams/teams.json" {
		t.Fatal("configured source lost")
	}
	cfg.Settings.TeamSource = "https://user:private@example.test/teams.json"
	cfg.Settings.PollIntervalMinutes = 0
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "settings.team_source") || !strings.Contains(err.Error(), "poll_interval_minutes") || strings.Contains(err.Error(), "private") {
		t.Fatalf("validation = %v", err)
	}
}
func TestNormalizeTeamSource(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ input, want string }{{"/repo/teams/source", "file:///repo/teams/source"}, {"HTTPS://Example.TEST:443/teams/teams.json", "https://example.test/teams/teams.json"}, {"https://[::1]:8443/teams.json", "https://[::1]:8443/teams.json"}, {"https://example.test/%74eams.json", "https://example.test/teams.json"}, {"/repo/teams/teams.json", "file:///repo/teams/teams.json"}, {"file:///repo/teams/teams.json", "file:///repo/teams/teams.json"}} {
		got, err := NormalizeTeamSource(tt.input)
		if err != nil || got != tt.want {
			t.Errorf("normalize %q = %q, %v", tt.input, got, err)
		}
	}
	for _, input := range []string{"", "http://example.test/teams.json", "//example.test/teams.json", "https://example.test", "https://example.test/", "https://user:private@example.test/teams.json", "https://example.test/teams.json?token=private", "https://example.test/teams.json#private", "https://example.test/a/../teams.json", "https://example.test/%2e%2e/teams.json", "https://example.test/a%2fb/teams.json", "https://example.test/%252e%252e/teams.json", "https://example.test/a\\teams.json", " https://example.test/teams.json", "https://example.test/a//teams.json", "file://host/teams.json", "file:///repo/../teams.json", "file:///repo/teams.json?x=1", "/repo/", "relative/teams.json"} {
		if _, err := NormalizeTeamSource(input); err == nil {
			t.Errorf("accepted %q", input)
		} else if strings.Contains(err.Error(), "private") {
			t.Error("credential disclosed")
		}
	}
}
func TestTeamSourceFolderAcceptsOnlyAFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "teams.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, got, err := TeamSourceFolder(dir + "/")
	if err != nil || got != dir || identity != "file://"+filepath.ToSlash(dir) {
		t.Fatalf("folder = %q %q %v", identity, got, err)
	}
	if again, _, err := TeamSourceFolder("file://" + filepath.ToSlash(dir)); err != nil || again != identity {
		t.Fatalf("file URL folder = %q %v", again, err)
	}
	for _, input := range []string{"", "https://example.test/teams/teams.json", "http://example.test/teams.json", file, "file://" + filepath.ToSlash(file), "relative/teams/source"} {
		if _, _, err := TeamSourceFolder(input); !errors.Is(err, ErrTeamSourceFolder) || !strings.Contains(err.Error(), "--source /absolute/path/to/teams/source") {
			t.Errorf("TeamSourceFolder(%q) = %v", input, err)
		}
	}
}
func TestTeamStoragePaths(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	p := NewPaths(home)
	for _, tt := range []struct{ got, relative string }{{p.TeamsDir(), "teams"}, {p.TeamsInstalledDir(), "teams/installed"}, {p.TeamsStagingDir(), "teams/staging"}, {p.TeamsTrashDir(), "teams/trash"}, {p.TeamsLockPath(), "teams/catalogue.lock"}} {
		if want := filepath.Join(home, filepath.FromSlash(tt.relative)); tt.got != want {
			t.Errorf("path=%q want %q", tt.got, want)
		}
	}
}
