package codex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func (r *Runner) acquireDenyLease(ctx context.Context, req agent.Request) (func(), error) {
	if req.Invocation != agent.InvocationGraphNode {
		return func() {}, nil
	}
	if req.Session.Ref == "" {
		return nil, errors.New("codex graph session lease requires a factory session identity")
	}
	keys := []string{"factory\x00" + req.Ref.String() + "\x00" + req.Session.Ref}
	if req.Session.Resume != "" {
		keys = append(keys, "native\x00"+req.Session.Resume)
	}
	return r.acquireDenyLeaseKeys(ctx, keys)
}

// acquireNativeDenyLease binds a newly observed native identity before exposing it
// to another launch. A bounded context lets the caller refuse conflicting adoption.
func (r *Runner) acquireNativeDenyLease(ctx context.Context, nativeID string) (func(), error) {
	if nativeID == "" {
		return nil, errors.New("codex native session lease requires an identity")
	}
	return r.acquireDenyLeaseKeys(ctx, []string{"native\x00" + nativeID})
}

func (r *Runner) acquireDenyLeaseKeys(ctx context.Context, keys []string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := r.denyLeaseDirectory()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = fmt.Sprintf("%x.lock", sha256.Sum256([]byte(key)))
	}
	slices.Sort(names)
	var files []*os.File
	release := sync.OnceFunc(func() {
		for i := len(files) - 1; i >= 0; i-- {
			_ = syscall.Flock(int(files[i].Fd()), syscall.LOCK_UN)
			_ = files[i].Close()
		}
	})
	for _, name := range names {
		file, err := acquireDenyLockFile(ctx, dir, name)
		if err != nil {
			release()
			return nil, fmt.Errorf("codex session lease: %w", err)
		}
		files = append(files, file)
	}
	return release, nil
}

func (r *Runner) denyLeaseDirectory() (string, error) {
	// The configured factory home can use a conventional platform alias. Beneath
	// its canonical location, the policy and lock directories must be real and owned.
	if err := r.paths.EnsureHome(); err != nil {
		return "", err
	}
	home, err := filepath.EvalSymlinks(r.paths.Home())
	if err != nil {
		return "", err
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return "", err
	}
	policy := filepath.Join(home, filepath.Base(r.paths.CodexPolicyDir()))
	for _, dir := range []string{policy, filepath.Join(policy, "leases")} {
		if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !denyLeaseOwned(info) {
			return "", fmt.Errorf("codex policy directory must be a private owned real directory")
		}
	}
	return filepath.Join(policy, "leases"), nil
}

func denyLeaseOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(os.Geteuid())
}

func acquireDenyLockFile(ctx context.Context, dir, name string) (*os.File, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	before, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	f, err := openDenyLockFile(ctx, root, dir, name, before)
	if err != nil {
		return nil, err
	}
	verify := func() error {
		current, err := os.Lstat(dir)
		if err != nil || !current.IsDir() || !os.SameFile(before, current) {
			return errors.New("session lease directory was replaced")
		}
		canonical, err := filepath.EvalSymlinks(dir)
		if err != nil || canonical != dir {
			return errors.New("session lease directory contains a symlink")
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		named, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !denyLeaseOwned(info) || !os.SameFile(info, named) {
			return errors.New("session lease file is unsafe or was replaced")
		}
		return nil
	}
	fail := func(err error) (*os.File, error) {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	if err := verify(); err != nil {
		return fail(err)
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return fail(err)
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-ticker.C:
		}
	}
	if err := verify(); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return f, nil
}

// Exclusive creation avoids a Darwin first-creation race in openat with O_CREAT.
// The existing-file path never creates or truncates anything. Every retry stays
// under the same open root and proves that its directory has not been replaced.
func openDenyLockFile(ctx context.Context, root *os.Root, dir, name string, directory os.FileInfo) (*os.File, error) {
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current, err := os.Lstat(dir)
		canonical, resolveErr := filepath.EvalSymlinks(dir)
		if err != nil || resolveErr != nil || canonical != dir || !os.SameFile(directory, current) {
			return nil, errors.New("session lease directory was replaced")
		}
		file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, os.ErrExist) {
			before, statErr := root.Lstat(name)
			switch {
			case statErr != nil:
				err = statErr
			case !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || !denyLeaseOwned(before):
				return nil, errors.New("session lease file is unsafe")
			default:
				file, err = root.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW, 0600)
				if err == nil {
					opened, statErr := file.Stat()
					if statErr != nil || !os.SameFile(before, opened) {
						_ = file.Close()
						return nil, errors.New("session lease file was replaced while opening")
					}
					return file, nil
				}
			}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		last = err
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, last
}
