package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrSuiteBusy means another suite run holds the machine-wide suite lock.
var ErrSuiteBusy = errors.New("store: another suite run is in progress")

// LockSuite lets at most one full suite run at a time per factory home, across
// DB handles and processes. It never blocks: a busy lock is ErrSuiteBusy and
// the caller retries on the next tick. It copies LockStoryProgress: a dedicated
// file that is never unlinked, an explicit unlock before close, and release by
// the kernel if the process dies, so a crash cannot leave the lock stuck.
func (db *DB) LockSuite(ctx context.Context, lockDir string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(db.path)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(lockDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := fmt.Sprintf("%s.suite-%x.lock", filepath.Base(canonical), sha256.Sum256([]byte(canonical)))
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("store: invalid suite lock file"), err, f.Close())
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			err = ErrSuiteBusy
		}
		return nil, errors.Join(err, f.Close())
	}
	return func() error {
		return errors.Join(syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close())
	}, nil
}
