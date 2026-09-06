package team

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// repoLockName is the fixed name, within litefactoryDir, of the file this
// package flocks around the whole of an Add call. It is distinct from the
// lockfile itself (team.lock.json), which is rewritten atomically on every
// successful call: this file is created once, kept forever, and never
// replaced, so a lock held by one process is never invalidated by another
// process reading or rewriting the lockfile out from under it.
const repoLockName = ".team.lock"

// repoLock holds a per-repo advisory lock for the duration of one Add call.
//
// Unlike internal/daemon's Lock, which uses LOCK_EX|LOCK_NB and fails fast
// when the lock is already held, this lock blocks: two concurrent Add
// invocations against the same repo are expected to serialize, not race
// each other to fail. internal/team cannot import internal/daemon (see
// doc.go's Invariants), so this is a small, independent implementation
// rather than a shared one.
type repoLock struct {
	f *os.File
}

// acquireRepoLock blocks until it holds an exclusive lock on root's
// .litefactory/.team.lock file, creating the directory and file as needed.
func acquireRepoLock(root string) (*repoLock, error) {
	dir := filepath.Join(root, litefactoryDir)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return nil, fmt.Errorf("create %s: %w", litefactoryDir, err)
	}
	path := filepath.Join(dir, repoLockName)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: %w", path, ErrRefusedSymlink)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("lstat %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &repoLock{f: f}, nil
}

// release unlocks and closes the lock file. The file itself is left in
// place for the next Add call to reuse.
func (l *repoLock) release() error {
	if err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.f.Close()
		return fmt.Errorf("unlock %s: %w", l.f.Name(), err)
	}
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", l.f.Name(), err)
	}
	return nil
}
