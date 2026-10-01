package logging

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// RotationStampLayout is the UTC time layout appended to a rotated log file's
// name: daemon.log.20260930-141500.
const RotationStampLayout = "20060102-150405"

// Rotate renames the live file to name, a base name inside the same directory,
// under the sink lock. The file is left closed, so the next Write reopens a
// fresh file at the original path. A live file that does not exist is not an
// error: there is nothing to rotate. An existing target is never overwritten.
func (s *FileSink) Rotate(name string) error {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return fmt.Errorf("rotate log file %s: invalid rotated name %q", s.path, name)
	}
	target := filepath.Join(filepath.Dir(s.path), name)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		if err != nil {
			return fmt.Errorf("close log file %s: %w", s.path, err)
		}
	}
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("rotate log file %s: %s already exists", s.path, target)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("rotate log file %s: %w", s.path, err)
	}
	if err := os.Rename(s.path, target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("rotate log file %s: %w", s.path, err)
	}
	s.failed = nil
	return nil
}

// CopyTruncate preserves a file another process holds open for appending, as
// launchd does with its redirected output: it copies live to dest (mode 0600),
// syncs the copy, then truncates live to zero. Both names are relative to root.
// An empty live file, or one that does not exist, is left alone and reports 0
// bytes. A failed copy removes the partial copy and leaves live untouched; a
// failed truncate keeps the complete copy. It returns the bytes copied.
func CopyTruncate(root *os.Root, live, dest string) (copied int64, err error) {
	info, err := root.Lstat(live)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat %s: %w", live, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, nil
	}
	src, err := root.OpenFile(live, os.O_RDWR, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", live, err)
	}
	defer func() { err = errors.Join(err, closeQuiet(src)) }()

	out, err := root.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", dest, err)
	}
	n, copyErr := io.Copy(out, src)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		if rmErr := root.Remove(dest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			copyErr = errors.Join(copyErr, rmErr)
		}
		return 0, fmt.Errorf("copy %s to %s: %w", live, dest, copyErr)
	}
	if err := src.Truncate(0); err != nil {
		return n, fmt.Errorf("truncate %s after copy to %s: %w", live, dest, err)
	}
	return n, nil
}

func closeQuiet(f *os.File) error { return f.Close() }
