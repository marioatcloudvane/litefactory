package daemon_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/daemon"
)

// TestSecondInstanceIsRefused is AC-2.6. Two daemons polling the same
// repositories would both see the same comment, and only the per-task lock
// would stand between them and two runs of it — so the singleton is enforced at
// the process level, before either of them has read anything.
func TestSecondInstanceIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")

	first, err := daemon.AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if first.Path() != path {
		t.Errorf("lock path is %q, want %q", first.Path(), path)
	}

	second, err := daemon.AcquireLock(path)
	if err == nil {
		t.Fatalf("a second instance acquired the lock; closing it: %v", second.Close())
	}
	var alreadyRunning *daemon.AlreadyRunningError
	if !errors.As(err, &alreadyRunning) {
		t.Fatalf("error is %v, want an *AlreadyRunningError", err)
	}
	if alreadyRunning.Path != path {
		t.Errorf("the refusal names %q, want %q", alreadyRunning.Path, path)
	}
	if !strings.Contains(alreadyRunning.Error(), path) {
		t.Errorf("the message does not name the lock file: %s", alreadyRunning)
	}

	// Releasing it lets the next daemon start, which is what makes `daemon
	// stop` followed by `daemon start` work.
	if err := first.Close(); err != nil {
		t.Fatalf("release the lock: %v", err)
	}
	third, err := daemon.AcquireLock(path)
	if err != nil {
		t.Fatalf("acquire the released lock: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Errorf("release the lock: %v", err)
	}
}

// TestLockFileSurvivesRelease is deliberate: the file is never unlinked.
// Removing it would let a second instance create a fresh one and lock that
// instead, and the singleton would quietly stop being one.
func TestLockFileSurvivesRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")

	lock, err := daemon.AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("release the lock: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the lock file was removed on release: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the lock file is mode %#o, want 0600", perm)
	}
}

// TestClosingTwiceIsHarmless: Close sits in a defer on a startup path that can
// fail half way, so it has to tolerate being called on a lock that is already
// released, and on none at all.
func TestClosingTwiceIsHarmless(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")

	lock, err := daemon.AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}

	var absent *daemon.Lock
	if err := absent.Close(); err != nil {
		t.Errorf("closing a nil lock: %v", err)
	}
	if absent.Path() != "" {
		t.Errorf("a nil lock reports the path %q", absent.Path())
	}
}

// TestAcquireLockOnAnUnwritablePathFails keeps the failure legible: a lock that
// cannot be taken because the directory is missing is not the same as one held
// by another daemon, and saying so is the difference between "stop the other
// one" and "your $LF_HOME is gone".
func TestAcquireLockOnAnUnwritablePathFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-directory", "daemon.lock")

	lock, err := daemon.AcquireLock(path)
	if err == nil {
		t.Fatalf("acquired a lock under a missing directory; closing it: %v", lock.Close())
	}
	if errors.As(err, new(*daemon.AlreadyRunningError)) {
		t.Errorf("a missing directory was reported as another daemon: %v", err)
	}
}
