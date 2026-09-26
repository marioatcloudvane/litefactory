// Package bugrepair defines authored repair evidence, never check or delivery authority.
package bugrepair

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Coverage identifies every approved success criterion and its authored coverage.
type Coverage struct {
	Criterion string   `json:"criterion"`
	Changes   []string `json:"changes"`
	Tests     []string `json:"tests"`
	Gap       string   `json:"gap,omitempty"`
}

// Evidence binds authored files to the exact reservation and approved analysis.
type Evidence struct {
	Reservation    string     `json:"reservation"`
	AnalysisDigest string     `json:"analysis_digest"`
	SourceCommit   string     `json:"source_commit"`
	SourceTree     string     `json:"source_tree"`
	Summary        string     `json:"summary"`
	Coverage       []Coverage `json:"coverage"`
}

// Validate refuses omitted/unknown criteria and escaping paths. Semantic scope
// needs a separate factory observation; allowed paths are not semantic proof.
func (e Evidence) Validate(criteria []string, done bool) error {
	if e.Reservation == "" || e.AnalysisDigest == "" || e.SourceCommit == "" || e.SourceTree == "" || strings.TrimSpace(e.Summary) == "" {
		return errors.New("bug repair: incomplete evidence binding")
	}
	expected := make(map[string]bool, len(criteria))
	for _, id := range criteria {
		if id == "" || expected[id] {
			return errors.New("bug repair: invalid factory criteria")
		}
		expected[id] = true
	}
	seen := make(map[string]bool)
	for _, c := range e.Coverage {
		if !expected[c.Criterion] || seen[c.Criterion] {
			return fmt.Errorf("bug repair: unknown or duplicate criterion %q", c.Criterion)
		}
		seen[c.Criterion] = true
		for _, paths := range [][]string{c.Changes, c.Tests} {
			for _, path := range paths {
				if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00\r\n") {
					return errors.New("bug repair: escaping evidence path")
				}
			}
		}
		if done && (strings.TrimSpace(c.Gap) != "" || len(c.Changes) == 0 || len(c.Tests) == 0) {
			return errors.New("bug repair: unmet implementation requirement must be blocked")
		}
		if !done && len(c.Changes) == 0 && len(c.Tests) == 0 && strings.TrimSpace(c.Gap) == "" {
			return errors.New("bug repair: coverage needs references or explicit gap")
		}
	}
	if done && len(seen) != len(expected) {
		return errors.New("bug repair: omitted approved criteria")
	}
	return nil
}
