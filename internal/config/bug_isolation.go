package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// BugCheckIsolation grants only explicitly declared check runtime resources.
// Project recipes cannot add resources or environment values to this ceiling.
// The concrete executor validates host support and paths before each launch.
type BugCheckIsolation struct {
	Binary        string   `yaml:"binary"`
	ReadOnlyRoots []string `yaml:"read_only_roots"`
	PATH          string   `yaml:"path"`
	// Environment names reference operator process values, never inline secrets.
	Environment []string `yaml:"environment"`
}

func validateBugIsolation(p *BugCheckIsolation) []error {
	if p == nil {
		return nil
	}
	var faults []error
	if !filepath.IsAbs(p.Binary) || strings.ContainsRune(p.Binary, 0) {
		faults = append(faults, errors.New("bug_checks.binary must be an absolute executable path"))
	}
	if len(p.ReadOnlyRoots) == 0 {
		faults = append(faults, errors.New("bug_checks.read_only_roots must declare runtime system trees"))
	}
	for i, root := range p.ReadOnlyRoots {
		switch root {
		case "/usr", "/bin", "/sbin", "/lib", "/lib64":
		default:
			faults = append(faults, fmt.Errorf("bug_checks.read_only_roots[%d] is not a supported runtime tree", i))
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
