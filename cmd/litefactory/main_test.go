package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDaemonLogPathUsesLFHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LF_HOME", home)

	got, err := daemonLogPath()
	if err != nil {
		t.Fatalf("daemonLogPath: %v", err)
	}
	if want := filepath.Join(home, "daemon.log"); got != want {
		t.Errorf("daemonLogPath = %q, want %q", got, want)
	}
}

func TestDaemonLogPathFallsBackToHomeDir(t *testing.T) {
	t.Setenv("LF_HOME", "")

	got, err := daemonLogPath()
	if err != nil {
		t.Fatalf("daemonLogPath: %v", err)
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory on this machine: %v", err)
	}
	if want := filepath.Join(dir, ".litefactory", "daemon.log"); got != want {
		t.Errorf("daemonLogPath = %q, want %q", got, want)
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
