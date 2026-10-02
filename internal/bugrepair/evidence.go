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

// Notice is a policy-class observation Validate makes instead of refusing: the
// caller records it as a fact and continues (REFUSAL_REVIEW.md, T-003).
// Reference is stable across replays; Text is short and plain-language.
type Notice struct {
	Reference string
	Text      string
}

// Validate refuses omitted/unknown criteria and escaping paths (integrity: it
// would otherwise corrupt the owner-approved binding or let a fabricated
// criterion pass as covered). Semantic scope needs a separate factory
// observation; allowed paths are not semantic proof.
//
// A reshape/blocked criterion left without a test reference or an explicit
// gap (the !done branch) is policy, not integrity: the owner still reads the
// coverage table at delivery review and decides, so it is returned as a
// Notice rather than a hard error (REFUSAL_REVIEW.md: bugrepair.coverage_gap).
func (e Evidence) Validate(criteria []string, done bool) ([]Notice, error) {
	if e.Reservation == "" || e.AnalysisDigest == "" || e.SourceCommit == "" || e.SourceTree == "" || strings.TrimSpace(e.Summary) == "" {
		return nil, errors.New("bug repair: incomplete evidence binding")
	}
	expected := make(map[string]bool, len(criteria))
	for _, id := range criteria {
		if id == "" || expected[id] {
			return nil, errors.New("bug repair: invalid factory criteria")
		}
		expected[id] = true
	}
	var notices []Notice
	seen := make(map[string]bool)
	for _, c := range e.Coverage {
		if !expected[c.Criterion] || seen[c.Criterion] {
			return notices, fmt.Errorf("bug repair: unknown or duplicate criterion %q", c.Criterion)
		}
		seen[c.Criterion] = true
		for _, paths := range [][]string{c.Changes, c.Tests} {
			for _, path := range paths {
				if !fs.ValidPath(path) || path == "." || strings.ContainsAny(path, "\\\x00\r\n") {
					return notices, errors.New("bug repair: escaping evidence path")
				}
			}
		}
		if done && (strings.TrimSpace(c.Gap) != "" || len(c.Changes) == 0 || len(c.Tests) == 0) {
			return notices, errors.New("bug repair: unmet implementation requirement must be blocked")
		}
		if !done && len(c.Changes) == 0 && len(c.Tests) == 0 && strings.TrimSpace(c.Gap) == "" {
			notices = append(notices, Notice{
				Reference: "bugrepair.coverage_gap:" + c.Criterion,
				Text:      fmt.Sprintf("criterion %s has no changed file, test reference or explicit gap recorded", c.Criterion),
			})
		}
	}
	if done && len(seen) != len(expected) {
		return notices, errors.New("bug repair: omitted approved criteria")
	}
	return notices, nil
}
