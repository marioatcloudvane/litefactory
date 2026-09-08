package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// WithCatalogLock serializes catalog mutations and snapshot capture using one
// stable lock inode. Contention fails explicitly instead of reading a mixture
// of catalog generations.
func WithCatalogLock(ctx context.Context, root string, fn func() error) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	if info, e := r.Lstat(".litefactory"); e == nil && !info.IsDir() {
		return fmt.Errorf("catalog metadata must be a real directory")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	if err := r.MkdirAll(".litefactory", 0o700); err != nil {
		return fmt.Errorf("create catalog metadata: %w", err)
	}
	f, err := r.OpenFile(".litefactory/.team.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open catalog lock: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("catalog lock is not regular")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("catalog is busy: %w", err)
	}
	defer func() { err = errors.Join(err, syscall.Flock(int(f.Fd()), syscall.LOCK_UN)) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	// A process may have stopped between individual catalog renames. Recovery
	// completes under the same stable lock before any reader sees a generation.
	if err := recoverCatalogTransaction(root); err != nil {
		return fmt.Errorf("recover catalog: %w", err)
	}
	return fn()
}
