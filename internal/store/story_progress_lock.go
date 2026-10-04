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

// ErrStoryProgressBusy leaves durable work for the next daemon tick.
var ErrStoryProgressBusy = errors.New("store: story progress parent is busy")

// LockStoryProgress serializes ingestion and remote delivery for a parent across
// DB handles and processes. This subordinate lock never acquires the session
// worker lock or holds a SQL transaction. Its dedicated file is never unlinked.
// The returned release function unlocks explicitly before closing: flock
// belongs to the open file description, which a forked child may still share
// between fork and exec, so closing only this descriptor could leave the lock
// held. Process exit also releases ownership.
func (db *DB) LockStoryProgress(ctx context.Context, parent, lockDir string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if parent == "" {
		return nil, fmt.Errorf("store: empty story progress parent")
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
	name := fmt.Sprintf("%s.story-progress-%x.lock", filepath.Base(canonical), sha256.Sum256([]byte(canonical+"\x00"+parent)))
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("store: invalid progress lock file"), err, f.Close())
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			err = ErrStoryProgressBusy
		}
		return nil, errors.Join(err, f.Close())
	}
	return func() error {
		return errors.Join(syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close())
	}, nil
}
