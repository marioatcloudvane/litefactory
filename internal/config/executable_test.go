package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// launchdPATH is the environment a LaunchAgent actually gets. Homebrew's
// /opt/homebrew/bin is not in it, which is the whole reason claude.binary
// wants an absolute path (SD-7).
const launchdPATH = "/usr/bin:/bin:/usr/sbin:/sbin"

// writeExecutable creates a runnable file and returns its path.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestResolveExecutableAbsolutePath(t *testing.T) {
	path := writeExecutable(t, t.TempDir(), "claude")

	claude := Claude{Binary: path}
	got, err := claude.ResolveBinary(launchdPATH)
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got.Path != path {
		t.Errorf("Path = %q, want %q", got.Path, path)
	}
	if got.FromPATH {
		t.Error("FromPATH = true for an absolute path")
	}
}

func TestResolveExecutableSearchesPATH(t *testing.T) {
	dir := t.TempDir()
	path := writeExecutable(t, dir, "claude")

	claude := Claude{Binary: "claude"}
	got, err := claude.ResolveBinary(strings.Join([]string{"/nonexistent", dir}, string(filepath.ListSeparator)))
	if err != nil {
		t.Fatalf("ResolveBinary: %v", err)
	}
	if got.Path != path {
		t.Errorf("Path = %q, want %q", got.Path, path)
	}
	if !got.FromPATH {
		t.Error("FromPATH = false for a bare command name found on PATH")
	}
}

// TestResolveExecutableNamesTheSearchedPATH is AC-3.18. A daemon that fails
// with "claude: not found" costs an evening; one that prints the four
// directories launchd gave it explains itself.
func TestResolveExecutableNamesTheSearchedPATH(t *testing.T) {
	claude := Claude{Binary: "claude"}

	_, err := claude.ResolveBinary(launchdPATH)
	if err == nil {
		t.Skip("this machine has claude on the launchd PATH, so there is nothing to fail")
	}
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Errorf("errors.Is(err, ErrExecutableNotFound) = false, want true (err = %v)", err)
	}

	var execErr *ExecutableError
	if !errors.As(err, &execErr) {
		t.Fatalf("errors.As(err, *ExecutableError) = false (err = %v)", err)
	}
	if execErr.Key != "claude.binary" {
		t.Errorf("Key = %q, want %q", execErr.Key, "claude.binary")
	}
	for _, dir := range strings.Split(launchdPATH, ":") {
		if !strings.Contains(err.Error(), dir) {
			t.Errorf("error does not name the searched directory %q:\n%v", dir, err)
		}
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Errorf("error does not suggest an absolute path:\n%v", err)
	}
}

func TestResolveExecutableMissingAbsolutePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "claude")

	_, err := Claude{Binary: missing}.ResolveBinary(launchdPATH)
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("err = %v, want ErrExecutableNotFound", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error does not name the configured binary %q:\n%v", missing, err)
	}
	// An explicit path was given, so there is no PATH search to report.
	if strings.Contains(err.Error(), "searched PATH") {
		t.Errorf("error mentions a PATH search that did not happen:\n%v", err)
	}
}

func TestResolveExecutableNotExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("not a program"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	_, err := Claude{Binary: path}.ResolveBinary(launchdPATH)
	if !errors.Is(err, ErrNotExecutable) {
		t.Fatalf("err = %v, want ErrNotExecutable", err)
	}
}

func TestResolveExecutableRejectsADirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := Claude{Binary: dir}.ResolveBinary(launchdPATH)
	if !errors.Is(err, ErrNotExecutable) {
		t.Fatalf("err = %v, want ErrNotExecutable", err)
	}
}

// TestResolveExecutableIgnoresEmptyPATHEntries: POSIX reads an empty PATH
// entry as the working directory, which for a daemon means "wherever launchd
// happened to start me".
func TestResolveExecutableIgnoresEmptyPATHEntries(t *testing.T) {
	// Resolved under the key the daemon uses for git, since the same helper
	// resolves both executables at startup.
	_, err := ResolveExecutable("git", "git", "::")
	if !errors.Is(err, ErrExecutableNotFound) {
		t.Fatalf("err = %v, want ErrExecutableNotFound", err)
	}
}
