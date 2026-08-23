package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileSink is the io.Writer behind daemon.log. It opens the file — and creates
// its parent directory — on the first write rather than at construction, so a
// command that logs nothing (litefactory --version, litefactory --help) leaves
// no trace on disk.
//
// Permissions are deliberately not set here: the process umask of 0o077,
// installed as the first statement of main, turns the 0o777/0o666 request into
// a 0700 directory and a 0600 file. Nothing in litefactory calls Chmod.
//
// A FileSink is safe for concurrent use; slog handlers may be called from any
// goroutine.
type FileSink struct {
	path string

	mu     sync.Mutex
	file   *os.File
	failed error
}

// NewFileSink returns a sink that appends to path.
func NewFileSink(path string) *FileSink {
	return &FileSink{path: path}
}

// Write appends p to the file, opening it if this is the first write. Once
// opening has failed the error is remembered and returned for every
// subsequent write, so a broken log destination is reported rather than
// retried on every record.
func (s *FileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed != nil {
		return 0, s.failed
	}
	if s.file == nil {
		if err := s.open(); err != nil {
			s.failed = err
			return 0, err
		}
	}
	return s.file.Write(p)
}

// open creates the parent directory and opens the log file for appending. The
// caller holds s.mu.
func (s *FileSink) open() error {
	dir := filepath.Dir(s.path)
	//nolint:gosec // 0700 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("create log directory %s: %w", dir, err)
	}
	//nolint:gosec // 0600 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", s.path, err)
	}
	s.file = f
	return nil
}

// Close closes the underlying file if it was ever opened.
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	if err != nil {
		return fmt.Errorf("close log file %s: %w", s.path, err)
	}
	return nil
}
