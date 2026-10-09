package config

import (
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
		if cfg.Settings.TeamSource != DefaultTeamSource {
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
	for _, tt := range []struct{ input, want string }{{"", DefaultTeamSource}, {"HTTPS://Example.TEST:443/teams/teams.json", "https://example.test/teams/teams.json"}, {"https://[::1]:8443/teams.json", "https://[::1]:8443/teams.json"}, {"https://example.test/%74eams.json", "https://example.test/teams.json"}, {"/repo/teams/teams.json", "file:///repo/teams/teams.json"}, {"file:///repo/teams/teams.json", "file:///repo/teams/teams.json"}} {
		got, err := NormalizeTeamSource(tt.input)
		if err != nil || got != tt.want {
			t.Errorf("normalize %q = %q, %v", tt.input, got, err)
		}
	}
	for _, input := range []string{"http://example.test/teams.json", "//example.test/teams.json", "https://example.test", "https://example.test/", "https://user:private@example.test/teams.json", "https://example.test/teams.json?token=private", "https://example.test/teams.json#private", "https://example.test/a/../teams.json", "https://example.test/%2e%2e/teams.json", "https://example.test/a%2fb/teams.json", "https://example.test/%252e%252e/teams.json", "https://example.test/a\\teams.json", " https://example.test/teams.json", "https://example.test/a//teams.json", "file://host/teams.json", "file:///repo/../teams.json", "file:///repo/teams.json?x=1", "/repo/", "relative/teams.json"} {
		if _, err := NormalizeTeamSource(input); err == nil {
			t.Errorf("accepted %q", input)
		} else if strings.Contains(err.Error(), "private") {
			t.Error("credential disclosed")
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
