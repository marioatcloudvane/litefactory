package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// TestFileSinkHonoursUmask is AC-B.5: with the umask main installs, everything
// the process creates is 0700/0600 and no Chmod call is involved.
//
// The umask is process-wide state, so this test must not run in parallel with
// anything else in this package.
func TestFileSinkHonoursUmask(t *testing.T) {
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })

	path := filepath.Join(t.TempDir(), "home", "logs", "daemon.log")
	sink := NewFileSink(path)
	t.Cleanup(func() {
		if err := sink.Close(); err != nil {
			t.Errorf("close sink: %v", err)
		}
	})

	New(Options{File: sink}).Info("hello")

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("log file mode = %04o, want 0600", got)
	}

	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Errorf("directory %s mode = %04o, want 0700", dir, got)
		}
		// Stop below the temp root, whose permissions the test did not create.
		if filepath.Base(dir) == "home" {
			break
		}
	}
}

func TestFileSinkOpensLazily(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "unused", "daemon.log")
	sink := NewFileSink(path)

	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("constructing a sink created %s (err %v); it must open on first write", filepath.Dir(path), err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close an unopened sink: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("closing an unopened sink created %s", filepath.Dir(path))
	}
}

func TestFileSinkAppends(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")

	first := NewFileSink(path)
	New(Options{File: first}).Info("one")
	if err := first.Close(); err != nil {
		t.Fatalf("close first sink: %v", err)
	}

	second := NewFileSink(path)
	New(Options{File: second}).Info("two")
	if err := second.Close(); err != nil {
		t.Fatalf("close second sink: %v", err)
	}

	content, err := os.ReadFile(path) //nolint:gosec // path is built from t.TempDir.
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if lines := strings.Count(string(content), "\n"); lines != 2 {
		t.Errorf("log has %d lines, want 2 (the second sink truncated it):\n%s", lines, content)
	}
}

func TestFileSinkReportsAnUnopenableFile(t *testing.T) {
	t.Parallel()

	// A path whose parent is a regular file cannot be created.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	sink := NewFileSink(filepath.Join(blocker, "daemon.log"))
	if _, err := sink.Write([]byte("hello")); err == nil {
		t.Fatal("Write succeeded, want an error")
	}
	if _, err := sink.Write([]byte("hello")); err == nil {
		t.Fatal("second Write succeeded, want the remembered error")
	}
	if err := sink.Close(); err != nil {
		t.Errorf("close a never-opened sink: %v", err)
	}
}

func TestFileSinkIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "daemon.log")
	sink := NewFileSink(path)
	logger := New(Options{File: sink})

	const writers, perWriter = 8, 25
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := range writers {
		go func() {
			defer wg.Done()
			for j := range perWriter {
				logger.Info("event", slog.Int("writer", i), slog.Int("seq", j))
			}
		}()
	}
	wg.Wait()

	if err := sink.Close(); err != nil {
		t.Fatalf("close sink: %v", err)
	}
	content, err := os.ReadFile(path) //nolint:gosec // path is built from t.TempDir.
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := strings.Count(string(content), "\n"); got != writers*perWriter {
		t.Errorf("log has %d lines, want %d", got, writers*perWriter)
	}
}
