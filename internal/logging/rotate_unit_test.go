package logging_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/logging"
)

func TestFileSink_Rotate(t *testing.T) {
	t.Parallel()

	t.Run("renames live file and next write reopens fresh file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "daemon.log")
		s := logging.NewFileSink(path)
		if _, err := s.Write([]byte("old\n")); err != nil {
			t.Fatal(err)
		}

		if err := s.Rotate("daemon.log.20260930-141500"); err != nil {
			t.Fatalf("Rotate: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("live file should be gone after rotate, stat err = %v", err)
		}
		if _, err := s.Write([]byte("new\n")); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()

		old, _ := os.ReadFile(filepath.Join(dir, "daemon.log.20260930-141500"))
		cur, _ := os.ReadFile(path)
		if string(old) != "old\n" || string(cur) != "new\n" {
			t.Errorf("old=%q cur=%q", old, cur)
		}
	})

	t.Run("missing live file/is not an error", func(t *testing.T) {
		t.Parallel()
		s := logging.NewFileSink(filepath.Join(t.TempDir(), "daemon.log"))
		if err := s.Rotate("daemon.log.20260930-141500"); err != nil {
			t.Fatalf("Rotate: %v", err)
		}
	})

	t.Run("existing target/errors and is not overwritten", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "daemon.log")
		target := filepath.Join(dir, "daemon.log.20260930-141500")
		if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		s := logging.NewFileSink(path)
		_, _ = s.Write([]byte("live"))

		err := s.Rotate("daemon.log.20260930-141500")

		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err = %v", err)
		}
		got, _ := os.ReadFile(target)
		if string(got) != "keep" {
			t.Errorf("target overwritten: %q", got)
		}
		_ = s.Close()
	})

	for _, name := range []string{"", ".", "..", "sub/x.log", "../x.log"} {
		t.Run(fmt.Sprintf("invalid name %q/rejected", name), func(t *testing.T) {
			t.Parallel()
			s := logging.NewFileSink(filepath.Join(t.TempDir(), "daemon.log"))
			if err := s.Rotate(name); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	t.Run("concurrent writers/no lost or failed writes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "daemon.log")
		s := logging.NewFileSink(path)
		const writers, perWriter = 8, 200
		var wg sync.WaitGroup
		errCh := make(chan error, writers*perWriter+10)
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perWriter; i++ {
					if _, err := s.Write([]byte("line\n")); err != nil {
						errCh <- err
					}
				}
			}()
		}
		for r := 0; r < 5; r++ {
			if err := s.Rotate(fmt.Sprintf("daemon.log.2026093%d-000000", r)); err != nil {
				errCh <- err
			}
		}
		wg.Wait()
		_ = s.Close()
		close(errCh)
		for err := range errCh {
			t.Errorf("unexpected error: %v", err)
		}

		entries, _ := os.ReadDir(dir)
		lines := 0
		for _, e := range entries {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			lines += strings.Count(string(b), "line\n")
		}
		if lines != writers*perWriter {
			t.Errorf("lines = %d, want %d", lines, writers*perWriter)
		}
	})
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestCopyTruncate(t *testing.T) {
	t.Parallel()

	t.Run("success/copies bytes and truncates live", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.log"), []byte("hello world"), 0o600); err != nil {
			t.Fatal(err)
		}

		n, err := logging.CopyTruncate(openRoot(t, dir), "a.log", "a.log.1")

		if err != nil || n != 11 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		cp, _ := os.ReadFile(filepath.Join(dir, "a.log.1"))
		live, _ := os.ReadFile(filepath.Join(dir, "a.log"))
		if string(cp) != "hello world" || len(live) != 0 {
			t.Errorf("copy=%q live=%q", cp, live)
		}
		info, _ := os.Stat(filepath.Join(dir, "a.log.1"))
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", info.Mode().Perm())
		}
	})

	t.Run("empty live file/left alone reports zero", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "a.log"), nil, 0o600)

		n, err := logging.CopyTruncate(openRoot(t, dir), "a.log", "a.log.1")

		if err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "a.log.1")); !os.IsNotExist(err) {
			t.Errorf("dest should not exist: %v", err)
		}
	})

	t.Run("missing live file/no error", func(t *testing.T) {
		t.Parallel()
		n, err := logging.CopyTruncate(openRoot(t, t.TempDir()), "a.log", "a.log.1")
		if err != nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})

	t.Run("failed copy/live untouched and no partial dest", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "a.log"), []byte("data"), 0o600)
		// Dest already exists: O_EXCL create fails before anything is copied.
		_ = os.WriteFile(filepath.Join(dir, "a.log.1"), []byte("prior"), 0o600)

		n, err := logging.CopyTruncate(openRoot(t, dir), "a.log", "a.log.1")

		if err == nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		live, _ := os.ReadFile(filepath.Join(dir, "a.log"))
		prior, _ := os.ReadFile(filepath.Join(dir, "a.log.1"))
		if string(live) != "data" || string(prior) != "prior" {
			t.Errorf("live=%q prior=%q", live, prior)
		}
	})

	t.Run("unwritable dest path/errors and live untouched", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// A directory named "a.log" is not regular; use a file whose dest cannot
		// be created in a missing subdirectory to force the create failure.
		_ = os.WriteFile(filepath.Join(dir, "a.log"), []byte("data"), 0o600)

		n, err := logging.CopyTruncate(openRoot(t, dir), "a.log", "missing/a.log.1")

		if err == nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		live, _ := os.ReadFile(filepath.Join(dir, "a.log"))
		if string(live) != "data" {
			t.Errorf("live=%q", live)
		}
	})

	t.Run("unopenable live file/errors and live untouched, true truncate failure not portably injectable", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		dir := t.TempDir()
		live := filepath.Join(dir, "a.log")
		_ = os.WriteFile(live, []byte("data"), 0o600)
		// Make the live file read-only: open O_RDWR fails before copy, so this
		// instead exercises the open failure path; a true truncate failure
		// cannot be provoked portably without injection.
		_ = os.Chmod(live, 0o400)
		t.Cleanup(func() { _ = os.Chmod(live, 0o600) })

		n, err := logging.CopyTruncate(openRoot(t, dir), "a.log", "a.log.1")

		if err == nil || n != 0 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		got, _ := os.ReadFile(live)
		if string(got) != "data" {
			t.Errorf("live=%q", got)
		}
	})
}
