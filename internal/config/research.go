package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ResearchExecution reads the operator's repository-local selection. Absence
// selects the gated application default; explicit values are strictly closed.
// This is configuration in the registered clone, never in an agent worktree.
func ResearchExecution(root string) (string, error) {
	return workflowExecution(root, "research")
}

// workflowExecution reads one workflow's execution selection. The label names
// both the file (<label>.yaml) and the wording of every refusal.
func workflowExecution(root, label string) (string, error) {
	dir := filepath.Join(root, ".litefactory")
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New(label + " configuration directory must be a regular directory")
	}
	name := filepath.Join(dir, label+".yaml")
	info, err = os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return "", errors.New(label + " configuration must be a regular file of at most 1024 bytes")
	}
	f, err := os.Open(name) // #nosec G304 -- fixed basename under the trusted clone; Lstat rejects symlinks and SameFile verifies the opened file.
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", errors.New(label + " configuration changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil {
		return "", err
	}
	if len(raw) > 1024 {
		return "", errors.New(label + " configuration exceeds 1024 bytes")
	}
	return decodeExecution(raw, label)
}
