package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// lockFileMode is 0600. The file holds nothing — its existence and its lock
// state are the whole content — but it lives in $LF_HOME and everything there
// is the owner's alone (S-1).
const lockFileMode fs.FileMode = 0o600

// Lock is the exclusive advisory lock that makes the daemon a singleton
// (AC-2.6).
//
// It is taken on a dedicated file, $LF_HOME/daemon.lock, and deliberately not
// on litefactory.db. Locking the database file would fight SQLite's own WAL
// locking, and the two mechanisms disagreeing is the kind of bug that shows up
// as a corrupted read months later.
//
// The lock is held for the daemon's lifetime and released by [Lock.Close]. The
// file is never removed: unlinking it would let a second instance create a
// fresh one and lock that instead, and the whole guarantee would evaporate. An
// abandoned lock costs nothing — the kernel drops the flock when the process
// dies, however it dies, which is exactly the property a pid file does not
// have.
type Lock struct {
	path string
	file *os.File
}

// AcquireLock takes the exclusive lock on path, creating the file if needed.
//
// A lock held by another process returns an [*AlreadyRunningError] rather than
// blocking: a second `daemon run` must fail immediately and visibly, not wait
// in silence for the first one to stop.
func AcquireLock(path string) (*Lock, error) {
	// #nosec G304 -- path is $LF_HOME/daemon.lock, built by config.Paths from
	// the resolved home directory. It is never user input.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFileMode)
	if err != nil {
		return nil, fmt.Errorf("open the daemon lock file %s: %w", path, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := file.Close()
		// EWOULDBLOCK is EAGAIN on every platform litefactory runs on; both are
		// checked so a future port does not turn "someone else has it" into an
		// unexplained startup failure.
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errors.Join(&AlreadyRunningError{Path: path}, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("lock %s: %w", path, err), closeErr)
	}
	return &Lock{path: path, file: file}, nil
}

// Path returns the lock file's path, for the startup line that says which one
// this daemon holds.
func (l *Lock) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close releases the lock. Closing the descriptor is what drops the flock;
// the unlock is explicit anyway, so that a reader does not have to know that.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return errors.Join(fmt.Errorf("unlock %s: %w", l.path, unlockErr), closeErr)
	}
	return closeErr
}
