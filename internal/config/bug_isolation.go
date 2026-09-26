package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// BugCheckIsolation grants only explicitly declared check runtime resources.
// Project recipes cannot add resources or environment values to this ceiling.
// Checks run under the Codex sandbox: the filesystem is readable except
// DenyRead and $LF_HOME, writes are limited to the worktree, a private
// temporary directory and WritableRoots, and the network is off.
type BugCheckIsolation struct {
	// Binary is the Codex CLI that applies the sandbox.
	Binary        string   `yaml:"binary"`
	WritableRoots []string `yaml:"writable_roots"`
	DenyRead      []string `yaml:"deny_read"`
	PATH          string   `yaml:"path"`
	// Environment names reference operator process values, never inline secrets.
	Environment []string `yaml:"environment"`
	// Network opens the network, loopback included, for projects whose tests
	// start local servers. The sandbox has no loopback-only mode.
	Network bool `yaml:"network"`
}

func validateBugIsolation(p *BugCheckIsolation) []error {
	if p == nil {
		return nil
	}
	var faults []error
	if !filepath.IsAbs(p.Binary) || strings.ContainsRune(p.Binary, 0) {
		faults = append(faults, errors.New("bug_checks.binary must be an absolute path to the Codex CLI"))
	}
	for i, root := range p.WritableRoots {
		if !bugSandboxPath(root) || root == "/" {
			faults = append(faults, fmt.Errorf("bug_checks.writable_roots[%d] must be an absolute directory other than /", i))
		}
	}
	for i, path := range p.DenyRead {
		if !bugSandboxPath(path) || path == "/" {
			faults = append(faults, fmt.Errorf("bug_checks.deny_read[%d] must be an absolute path other than /", i))
		}
	}
	for i, dir := range filepath.SplitList(p.PATH) {
		if !filepath.IsAbs(dir) || dir == "/" || strings.ContainsRune(dir, 0) {
			faults = append(faults, fmt.Errorf("bug_checks.path entry %d must be an absolute runtime directory", i))
		}
	}
	if p.PATH == "" {
		faults = append(faults, errors.New("bug_checks.path must declare runtime executable directories"))
	}
	seen := make(map[string]bool)
	for i, name := range p.Environment {
		if !bugEnvironmentName.MatchString(name) || seen[name] {
			faults = append(faults, fmt.Errorf("bug_checks.environment[%d] must be a distinct environment variable name", i))
		}
		seen[name] = true
	}
	return faults
}

// bugSandboxPath accepts clean absolute paths the sandbox launcher can quote.
func bugSandboxPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, r := range path {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
