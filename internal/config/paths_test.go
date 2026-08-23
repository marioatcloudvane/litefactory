package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateUsesLFHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)

	paths, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}

	// S-1, in one place: if any of these move, everything that reads them
	// moves with them.
	want := map[string]string{
		"home":     home,
		"config":   filepath.Join(home, "config.yaml"),
		"database": filepath.Join(home, "litefactory.db"),
		"logs":     filepath.Join(home, "logs"),
		"daemon":   filepath.Join(home, "logs", "daemon.log"),
		"runs":     filepath.Join(home, "logs", "runs"),
		"lock":     filepath.Join(home, "daemon.lock"),
		"settings": filepath.Join(home, "claude-settings.json"),
	}
	got := map[string]string{
		"home":     paths.Home(),
		"config":   paths.ConfigPath(),
		"database": paths.DBPath(),
		"logs":     paths.LogsDir(),
		"daemon":   paths.DaemonLogPath(),
		"runs":     paths.RunsDir(),
		"lock":     paths.LockPath(),
		"settings": paths.SettingsPath(),
	}
	for key, wantPath := range want {
		if got[key] != wantPath {
			t.Errorf("%s path = %q, want %q", key, got[key], wantPath)
		}
	}
}

func TestLocateFallsBackToUserHome(t *testing.T) {
	t.Setenv(EnvHome, "")

	paths, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	if want := filepath.Join(dir, ".litefactory"); paths.Home() != want {
		t.Errorf("Home() = %q, want %q", paths.Home(), want)
	}
}

// TestLocateMakesLFHomeAbsolute matters for the daemon: a relative LF_HOME
// would otherwise make its idea of its own state depend on the directory it
// happened to be started from.
func TestLocateMakesLFHomeAbsolute(t *testing.T) {
	t.Setenv(EnvHome, "relative-home")

	paths, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !filepath.IsAbs(paths.Home()) {
		t.Errorf("Home() = %q, want an absolute path", paths.Home())
	}
}

// TestLocateIgnoresBlankLFHome covers `LF_HOME= litefactory ...`, where the
// variable is set but empty: that must mean "unset", not "the root directory".
func TestLocateIgnoresBlankLFHome(t *testing.T) {
	t.Setenv(EnvHome, "   ")

	paths, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	if want := filepath.Join(dir, ".litefactory"); paths.Home() != want {
		t.Errorf("Home() = %q, want %q", paths.Home(), want)
	}
}

func TestEnsureHomeIsIdempotent(t *testing.T) {
	paths := NewPaths(filepath.Join(t.TempDir(), "nested", "home"))

	for i := range 2 {
		if err := paths.EnsureHome(); err != nil {
			t.Fatalf("EnsureHome (call %d): %v", i+1, err)
		}
	}
	info, err := os.Stat(paths.Home())
	if err != nil {
		t.Fatalf("stat home: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", paths.Home())
	}
}
