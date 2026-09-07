package team

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// refuseSymlink checks every existing path component from root down to the
// parent directory of path, and path itself if it already exists, for a
// symlink, refusing to write through one. It uses os.Lstat rather than
// os.Stat throughout so a symlink is detected rather than followed.
//
// Owned Path targets are always at most a few directories deep
// (".claude/agents/<key>.md"), so walking every component is cheap and
// leaves no gap between the check and the write.
func refuseSymlink(root, relPath string) error {
	clean := filepath.Clean(relPath)
	parts := splitPath(clean)
	cur := root
	for _, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("lstat %s: %w", cur, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", cur, ErrRefusedSymlink)
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("%s: not a regular file or directory: %w", cur, ErrRefusedSymlink)
		}
	}
	return nil
}

// splitPath splits a cleaned, forward-slash, repo-relative path into its
// components.
func splitPath(p string) []string {
	var parts []string
	for p != "." && p != "/" && p != "" {
		dir, file := filepath.Split(p)
		parts = append([]string{file}, parts...)
		p = filepath.Clean(dir)
	}
	return parts
}

// writeFileAtomic writes data to root/relPath via a temp file in the same
// directory, followed by an atomic rename. relPath is checked against
// isOwnedWriteTarget before anything is touched (T-46): this is the single
// choke point every write this package performs funnels through — standalone
// artifact files and root-file sections (write.go), the provenance lockfile
// (lockfile.go), the per-repo overrides file (overrides.go), and the
// .gitignore entry this package manages (gitignore.go) — so guarding here
// once covers all of them rather than trusting each caller to have checked
// already.
//
// The temp file is created with os.OpenFile using mode directly at
// creation (O_CREATE|O_WRONLY|O_EXCL), rather than os.CreateTemp (which
// always creates at 0600 and cannot take a mode) followed by a Chmod call.
// SEC-10 requires that nothing in litefactory call Chmod, relying instead
// on the umask installed as the first statement of main to narrow
// permissions; passing mode straight to OpenFile satisfies that with the
// same "0666-with-umask" result for a regular file.
func writeFileAtomic(root, relPath string, data []byte, mode os.FileMode) error {
	if !isOwnedWriteTarget(relPath) {
		return &WriteOutsideOwnedPathError{Path: relPath}
	}
	path := filepath.Join(root, relPath)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", path, ErrRefusedSymlink)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: not a regular file: %w", path, ErrRefusedSymlink)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat %s: %w", path, err)
	}

	tmpPath := path + ".tmp-litefactory"
	// Remove any stale temp file left by a previous interrupted run before
	// creating a fresh one with O_EXCL, so this call cannot silently write
	// through a symlink an attacker or leftover run placed at tmpPath.
	if info, err := os.Lstat(tmpPath); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: %w", tmpPath, ErrRefusedSymlink)
		}
		if err := os.Remove(tmpPath); err != nil {
			return fmt.Errorf("remove stale temp file %s: %w", tmpPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("lstat %s: %w", tmpPath, err)
	}

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create temp file %s: %w", tmpPath, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file %s: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("sync temp file %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s to %s: %w", tmpPath, path, err)
	}
	return nil
}
