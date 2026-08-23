package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestDaemonLogPathUsesLFHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LF_HOME", home)

	paths, err := config.Locate()
	if err != nil {
		t.Fatalf("config.Locate: %v", err)
	}
	if want := filepath.Join(home, "logs", "daemon.log"); paths.DaemonLogPath() != want {
		t.Errorf("DaemonLogPath = %q, want %q", paths.DaemonLogPath(), want)
	}
}

func TestDaemonLogPathFallsBackToHomeDir(t *testing.T) {
	t.Setenv("LF_HOME", "")

	paths, err := config.Locate()
	if err != nil {
		t.Fatalf("config.Locate: %v", err)
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	if want := filepath.Join(dir, ".litefactory", "logs", "daemon.log"); paths.DaemonLogPath() != want {
		t.Errorf("DaemonLogPath = %q, want %q", paths.DaemonLogPath(), want)
	}
}

// TestConfiguredLevelFallsBackWithoutConfig covers the deliberate silence in
// configuredLevel: with no $LF_HOME on disk at all, the logger still gets a
// level rather than the process refusing to start.
func TestConfiguredLevelFallsBackToInfo(t *testing.T) {
	t.Setenv("LF_HOME", t.TempDir())

	if got := configuredLevel(); got != slog.LevelInfo {
		t.Errorf("configuredLevel() = %v, want %v", got, slog.LevelInfo)
	}
}

// TestConfiguredLevelReadsConfig checks the seam is actually wired: a
// log_level in config.yaml reaches the logger.
func TestConfiguredLevelReadsConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LF_HOME", home)

	paths := config.NewPaths(home)
	if _, err := config.WriteDefault(paths); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	contents, err := os.ReadFile(paths.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	edited := strings.Replace(string(contents), `log_level: "info"`, `log_level: "debug"`, 1)
	if err := os.WriteFile(paths.ConfigPath(), []byte(edited), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if got := configuredLevel(); got != slog.LevelDebug {
		t.Errorf("configuredLevel() = %v, want %v", got, slog.LevelDebug)
	}
}

// TestRootContextCancelsOnSignal checks the one cancellation source in the
// program actually fires. The signal is delivered to this test process and
// caught by the context, so it does not kill the test binary.
func TestRootContextCancelsOnSignal(t *testing.T) {
	ctx, stop := rootContext(context.Background(), io.Discard)
	defer stop()

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal self: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("root context was not cancelled by SIGTERM")
	}
}

// TestRootContextStopIsIdempotent covers the deferred cancel running more than
// once, which must not panic on the closed watcher channel.
func TestRootContextStopIsIdempotent(t *testing.T) {
	_, stop := rootContext(context.Background(), io.Discard)
	stop()
	stop()
}
