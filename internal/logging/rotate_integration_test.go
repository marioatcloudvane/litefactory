package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func housekeepingWrite(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func housekeepingRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func housekeepingMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func housekeepingRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func TestHousekeepingIntegrationSinkRotateRenamesAndReopensFresh(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	dir := t.TempDir()
	live := filepath.Join(dir, "daemon.log")
	sink := NewFileSink(live)
	defer func() { _ = sink.Close() }()

	if _, err := sink.Write([]byte("before\n")); err != nil {
		t.Fatal(err)
	}
	if err := sink.Rotate("daemon.log.20260930-120000"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(live); !os.IsNotExist(err) {
		t.Fatalf("live file should be gone until next write, err=%v", err)
	}
	if got := housekeepingRead(t, filepath.Join(dir, "daemon.log.20260930-120000")); got != "before\n" {
		t.Fatalf("rotated content = %q", got)
	}
	if _, err := sink.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	if got := housekeepingRead(t, live); got != "after\n" {
		t.Fatalf("fresh live content = %q", got)
	}
	if m := housekeepingMode(t, live); m != 0o600 {
		t.Fatalf("fresh live mode = %o", m)
	}
	if m := housekeepingMode(t, filepath.Join(dir, "daemon.log.20260930-120000")); m != 0o600 {
		t.Fatalf("rotated mode = %o", m)
	}
}

func TestHousekeepingIntegrationSinkRotateNothingToRotateAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "daemon.log")
	sink := NewFileSink(live)
	defer func() { _ = sink.Close() }()

	// Never written: nothing to rotate, not an error, no file created.
	if err := sink.Rotate("daemon.log.20260930-120000"); err != nil {
		t.Fatalf("rotate of absent live file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "daemon.log.20260930-120000")); !os.IsNotExist(err) {
		t.Fatalf("rotate of absent file created a target: %v", err)
	}

	// An existing target is never overwritten, and the live content survives.
	if _, err := sink.Write([]byte("live\n")); err != nil {
		t.Fatal(err)
	}
	housekeepingWrite(t, filepath.Join(dir, "daemon.log.20260930-120000"), "precious\n", 0o600)
	if err := sink.Rotate("daemon.log.20260930-120000"); err == nil {
		t.Fatal("expected error rotating onto an existing target")
	}
	if got := housekeepingRead(t, filepath.Join(dir, "daemon.log.20260930-120000")); got != "precious\n" {
		t.Fatalf("existing target overwritten: %q", got)
	}
	if got := housekeepingRead(t, live); got != "live\n" {
		t.Fatalf("live content lost: %q", got)
	}
	// The sink still works afterwards (reopens the live file).
	if _, err := sink.Write([]byte("more\n")); err != nil {
		t.Fatal(err)
	}
	if got := housekeepingRead(t, live); got != "live\nmore\n" {
		t.Fatalf("live after failed rotate = %q", got)
	}
}

func TestHousekeepingIntegrationSinkRotateRejectsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	sink := NewFileSink(filepath.Join(dir, "daemon.log"))
	defer func() { _ = sink.Close() }()
	if _, err := sink.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", ".", "..", "../escape.log", "sub/daemon.log.1", "/abs/daemon.log.1"} {
		if err := sink.Rotate(name); err == nil {
			t.Errorf("Rotate(%q) accepted", name)
		}
	}
	if got := housekeepingRead(t, filepath.Join(dir, "daemon.log")); got != "x\n" {
		t.Fatalf("live changed by rejected rotations: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "escape.log")); !os.IsNotExist(err) {
		t.Fatal("rotation escaped the logs directory")
	}
}

func TestHousekeepingIntegrationSinkRotateConcurrentWritesLoseNothing(t *testing.T) {
	dir := t.TempDir()
	sink := NewFileSink(filepath.Join(dir, "daemon.log"))
	defer func() { _ = sink.Close() }()

	const writers, perWriter = 4, 200
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				if _, err := fmt.Fprintf(sink, "w%d-%d\n", w, i); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 5; i++ {
		if err := sink.Rotate(fmt.Sprintf("daemon.log.20260930-12000%d", i)); err != nil {
			t.Error(err)
		}
	}
	wg.Wait()

	var total int
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		total += strings.Count(housekeepingRead(t, filepath.Join(dir, e.Name())), "\n")
	}
	if total != writers*perWriter {
		t.Fatalf("lines across live+rotated = %d, want %d", total, writers*perWriter)
	}
}

func TestHousekeepingIntegrationCopyTruncateCopiesThenTruncates(t *testing.T) {
	dir := t.TempDir()
	root := housekeepingRoot(t, dir)
	housekeepingWrite(t, filepath.Join(dir, "launchd.out.log"), "line1\nline2\n", 0o644)

	// A holder opened like launchd does (append); it must keep working.
	holder, err := os.OpenFile(filepath.Join(dir, "launchd.out.log"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()

	n, err := CopyTruncate(root, "launchd.out.log", "launchd.out.log.20260930-120000")
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("line1\nline2\n")) {
		t.Fatalf("copied %d bytes", n)
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.out.log.20260930-120000")); got != "line1\nline2\n" {
		t.Fatalf("copy = %q", got)
	}
	if m := housekeepingMode(t, filepath.Join(dir, "launchd.out.log.20260930-120000")); m != 0o600 {
		t.Fatalf("copy mode = %o, want 0600", m)
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.out.log")); got != "" {
		t.Fatalf("live not truncated: %q", got)
	}
	if _, err := holder.WriteString("line3\n"); err != nil {
		t.Fatal(err)
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.out.log")); got != "line3\n" {
		t.Fatalf("holder write after truncate = %q (no sparse/NUL prefix expected)", got)
	}
}

func TestHousekeepingIntegrationCopyTruncateNoOpCases(t *testing.T) {
	dir := t.TempDir()
	root := housekeepingRoot(t, dir)
	outside := filepath.Join(t.TempDir(), "target.log")
	housekeepingWrite(t, outside, "outside\n", 0o600)
	housekeepingWrite(t, filepath.Join(dir, "empty.log"), "", 0o600)
	if err := os.Symlink(outside, filepath.Join(dir, "link.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "adir.log"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, live := range []string{"missing.log", "empty.log", "link.log", "adir.log"} {
		n, err := CopyTruncate(root, live, live+".20260930-120000")
		if err != nil || n != 0 {
			t.Errorf("%s: n=%d err=%v, want 0,nil", live, n, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, live+".20260930-120000")); !os.IsNotExist(err) {
			t.Errorf("%s: produced a copy", live)
		}
	}
	if got := housekeepingRead(t, outside); got != "outside\n" {
		t.Fatalf("symlink target modified: %q", got)
	}
}

func TestHousekeepingIntegrationCopyTruncateFailureLeavesLiveUntouched(t *testing.T) {
	dir := t.TempDir()
	root := housekeepingRoot(t, dir)
	housekeepingWrite(t, filepath.Join(dir, "launchd.err.log"), "keep me\n", 0o600)
	housekeepingWrite(t, filepath.Join(dir, "launchd.err.log.20260930-120000"), "existing\n", 0o600)

	if _, err := CopyTruncate(root, "launchd.err.log", "launchd.err.log.20260930-120000"); err == nil {
		t.Fatal("expected error when destination exists")
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.err.log")); got != "keep me\n" {
		t.Fatalf("live modified on failed copy: %q", got)
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.err.log.20260930-120000")); got != "existing\n" {
		t.Fatalf("existing dest clobbered: %q", got)
	}

	// Destination escaping the root is refused and live stays intact.
	if _, err := CopyTruncate(root, "launchd.err.log", "../escape.log"); err == nil {
		t.Fatal("expected error for escaping destination")
	}
	if got := housekeepingRead(t, filepath.Join(dir, "launchd.err.log")); got != "keep me\n" {
		t.Fatalf("live modified after escape attempt: %q", got)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "escape.log")); !os.IsNotExist(err) {
		t.Fatal("copy escaped the root")
	}
}

func TestHousekeepingIntegrationCopyTruncateIdempotentWhenEmptied(t *testing.T) {
	dir := t.TempDir()
	root := housekeepingRoot(t, dir)
	housekeepingWrite(t, filepath.Join(dir, "launchd.out.log"), "data\n", 0o600)
	if _, err := CopyTruncate(root, "launchd.out.log", "launchd.out.log.20260930-120000"); err != nil {
		t.Fatal(err)
	}
	n, err := CopyTruncate(root, "launchd.out.log", "launchd.out.log.20260930-120100")
	if err != nil || n != 0 {
		t.Fatalf("second run n=%d err=%v", n, err)
	}
	var buf bytes.Buffer
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		buf.WriteString(e.Name() + "\n")
	}
	if strings.Contains(buf.String(), "120100") {
		t.Fatalf("empty truncated log produced another copy:\n%s", buf.String())
	}
}
