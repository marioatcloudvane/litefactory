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
//
// Once the lock is held, it also ensures root's .gitignore carries an entry
// for litefactoryDir (T-45, gitignore.go): this is the one place every write
// path in this package (Add, SetOverride, UnsetOverride) passes through
// before touching .litefactory, so it is the natural, single choke point for
// keeping the ignore rule present in every worktree a team command ever
// runs against, no matter which of those three entry points got there
// first. Doing this after the flock rather than before it means two
// concurrent callers against the same root can never race a read of
// .gitignore against each other's write.
func acquireRepoLock(root string) (*repoLock, error) {
	dir := filepath.Join(root, litefactoryDir)
	// #nosec G301 -- SEC-10 requires main to install umask 077 before these repository writes.
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
	if err := refuseSymlink(root, filepath.Join(litefactoryDir, repoLockName)); err != nil {
		return nil, err
	}
	repo, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open repository root: %w", err)
	}
	defer repo.Close()
	metadata, err := repo.OpenRoot(litefactoryDir)
	if err != nil {
		return nil, fmt.Errorf("open repository metadata: %w", err)
	}
	defer metadata.Close()
	// Create the stable inode exclusively, then open the winner's inode on
	// contention. Concurrent nonexclusive O_CREATE opens can fail with ENOENT
	// on macOS; no caller may remove or replace this coordination file.
	f, err := metadata.OpenFile(repoLockName, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0o666)
	if os.IsExist(err) {
		f, err = metadata.OpenFile(repoLockName, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if err := ensureGitignoreEntry(root); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
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
