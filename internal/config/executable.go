package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Executable resolution.
//
// The daemon must not use exec.LookPath. launchd hands a LaunchAgent
// PATH=/usr/bin:/bin:/usr/sbin:/sbin, which does not contain /opt/homebrew/bin,
// so a claude installed by Homebrew is simply not on the daemon's PATH (SD-7).
// The fix has two halves: `daemon start` materialises the installing shell's
// PATH into the plist, and `daemon run` resolves the binary at startup and
// fails loudly, naming every directory it searched (AC-3.18) — which
// exec.LookPath's error cannot do.
//
// The honest configuration is an absolute claude.binary. The PATH search
// exists so the default value of "claude" works from an interactive shell, and
// [Executable.FromPATH] lets a caller warn that it is relying on it.

// Sentinel causes, for errors.Is.
var (
	// ErrExecutableNotFound means no candidate path existed.
	ErrExecutableNotFound = errors.New("executable not found")
	// ErrNotExecutable means a file was found but cannot be executed.
	ErrNotExecutable = errors.New("not executable")
)

// Executable is a resolved command.
type Executable struct {
	// Name is the configured value, as written.
	Name string
	// Path is the absolute path to the executable file.
	Path string
	// FromPATH reports that Name was a bare command name found by searching
	// PATH. A daemon should log this: the same configuration will fail under
	// launchd, whose PATH is much smaller than an interactive shell's.
	FromPATH bool
}

// ExecutableError reports that a configured command could not be resolved. Its
// message names the configured value and, for a PATH search, every directory
// that was looked in.
type ExecutableError struct {
	// Key is the configuration key the value came from, for example
	// "claude.binary".
	Key string
	// Name is the configured value.
	Name string
	// Searched holds the directories a PATH search covered. Empty when Name
	// was an explicit path.
	Searched []string
	// Err is the sentinel cause: [ErrExecutableNotFound] or
	// [ErrNotExecutable].
	Err error
}

func (e *ExecutableError) Error() string {
	if len(e.Searched) == 0 {
		return fmt.Sprintf("%s: %q %v", e.Key, e.Name, e.Err)
	}
	return fmt.Sprintf("%s: %q %v; searched PATH (%s); set %s to an absolute path",
		e.Key, e.Name, e.Err, strings.Join(e.Searched, string(filepath.ListSeparator)), e.Key)
}

func (e *ExecutableError) Unwrap() error { return e.Err }

// ResolveExecutable locates name, searching pathEnv (a PATH-shaped, list
// separator-joined string) only when name is a bare command name. key names
// the configuration setting for the error message.
//
// Callers pass PATH explicitly rather than having this read the environment,
// so that a test can assert the exact failure a launchd PATH produces.
func ResolveExecutable(key, name, pathEnv string) (Executable, error) {
	if name == "" {
		return Executable{}, &ExecutableError{Key: key, Name: name, Err: ErrExecutableNotFound}
	}

	if strings.ContainsRune(name, filepath.Separator) {
		path, err := filepath.Abs(name)
		if err != nil {
			return Executable{}, &ExecutableError{Key: key, Name: name, Err: ErrExecutableNotFound}
		}
		if err := checkExecutable(path); err != nil {
			return Executable{}, &ExecutableError{Key: key, Name: name, Err: err}
		}
		return Executable{Name: name, Path: path}, nil
	}

	searched := splitPath(pathEnv)
	for _, dir := range searched {
		candidate := filepath.Join(dir, name)
		if err := checkExecutable(candidate); err != nil {
			continue
		}
		return Executable{Name: name, Path: candidate, FromPATH: true}, nil
	}
	return Executable{}, &ExecutableError{
		Key:      key,
		Name:     name,
		Searched: searched,
		Err:      ErrExecutableNotFound,
	}
}

// ResolveBinary locates the configured claude executable.
func (c Claude) ResolveBinary(pathEnv string) (Executable, error) {
	return ResolveExecutable("claude.binary", c.Binary, pathEnv)
}

// checkExecutable reports whether path is a regular file with an execute bit.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return ErrExecutableNotFound
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return ErrNotExecutable
	}
	if info.Mode().Perm()&0o111 == 0 {
		return ErrNotExecutable
	}
	return nil
}

// splitPath splits a PATH value, dropping empty entries. POSIX reads an empty
// entry as the working directory; resolving a daemon's binary relative to
// wherever it happens to have been started is never what was meant.
func splitPath(pathEnv string) []string {
	var dirs []string
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}
