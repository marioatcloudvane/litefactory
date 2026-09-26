// Package buganalysis defines the pure bounded Bug analysis proposal contract.
package buganalysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// BugAnalysis is a proposal, never an authorization or execution-record patch.
// The factory keeps every revision and checks stable criterion meanings.
type BugAnalysis struct {
	Expected         string               `json:"expected"`
	Actual           string               `json:"actual"`
	Reproduction     BugReproduction      `json:"reproduction"`
	Cause            string               `json:"cause"`
	Evidence         []string             `json:"evidence"`
	Boundaries       *BugRepairBoundaries `json:"boundaries,omitempty"`
	Remedy           string               `json:"remedy"`
	Exclusions       []string             `json:"exclusions"`
	Criteria         []BugCriterion       `json:"criteria"`
	Decisions        []string             `json:"decisions,omitempty"`
	Assumptions      []string             `json:"assumptions,omitempty"`
	Questions        []BugQuestion        `json:"questions,omitempty"`
	AnswerReferences []string             `json:"answer_references,omitempty"`
}

// BugRepairBoundaries is a typed detail of the same owner-reviewed remedy.
// Exact repository-relative files include intended new regression files; this
// is neither a new approval gate nor a substitute for semantic role duties.
type BugRepairBoundaries struct {
	AllowedPaths  []string `json:"allowed_paths"`
	ExcludedPaths []string `json:"excluded_paths,omitempty"`
}

// Validate checks that approved boundaries name concrete repository paths.
func (b BugRepairBoundaries) Validate() error {
	if len(b.AllowedPaths) == 0 || len(b.AllowedPaths) > 256 || len(b.ExcludedPaths) > 256 {
		return errors.New("bug analysis: bounded remedy needs 1–256 explicit allowed paths")
	}
	seen := map[string]bool{}
	for _, paths := range [][]string{b.AllowedPaths, b.ExcludedPaths} {
		for _, p := range paths {
			if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, "\\\x00\r\n*?[") || seen[p] {
				return errors.New("bug analysis: invalid, duplicate or conflicting remedy path")
			}
			seen[p] = true
		}
	}
	return nil
}

// BugReproduction records the steps, environment and observation status.
type BugReproduction struct {
	Steps       []string `json:"steps"`
	Environment string   `json:"environment"`
	Observation string   `json:"observation"`
}

// BugCriterion binds a stable requirement ID to a measurable success condition.
type BugCriterion struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
	Measurement string `json:"measurement"`
}

// BugQuestion identifies an owner question and whether it blocks review.
type BugQuestion struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Material bool   `json:"material"`
}

var bugStableID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,127}$`)

// Validate checks a bounded partial snapshot or a complete review proposal.
// Answer references are checked against retained authenticated observations by
// the store; an authored reference alone cannot resolve a material question.
func (a BugAnalysis) Validate(review bool) error {
	var faults []error
	if a.Boundaries != nil {
		if err := a.Boundaries.Validate(); err != nil {
			faults = append(faults, err)
		}
	}
	required := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			faults = append(faults, fmt.Errorf("bug analysis: %s is blank", name))
		}
	}
	if review {
		required("expected behavior", a.Expected)
		required("actual behavior", a.Actual)
		required("environment", a.Reproduction.Environment)
		required("cause", a.Cause)
		required("remedy", a.Remedy)
		if len(a.Reproduction.Steps) == 0 || len(a.Evidence) == 0 || len(a.Exclusions) == 0 || len(a.Criteria) == 0 {
			faults = append(faults, errors.New("bug analysis: review needs reproduction steps, cause evidence, exclusions and measurable criteria"))
		}
	}
	switch a.Reproduction.Observation {
	case "observed", "owner_reported", "not_observed":
	case "":
		if review {
			faults = append(faults, errors.New("bug analysis: observation status is required"))
		}
	default:
		faults = append(faults, errors.New("bug analysis: invalid observation status"))
	}
	for _, list := range [][]string{a.Reproduction.Steps, a.Evidence, a.Exclusions, a.Decisions, a.Assumptions, a.AnswerReferences} {
		for _, value := range list {
			required("list entry", value)
		}
	}
	seen := make(map[string]bool)
	for _, c := range a.Criteria {
		if !bugStableID.MatchString(c.ID) || seen[c.ID] {
			faults = append(faults, fmt.Errorf("bug analysis: invalid or duplicate criterion %q", c.ID))
		}
		seen[c.ID] = true
		required("criterion requirement", c.Requirement)
		required("criterion measurement", c.Measurement)
	}
	seen = make(map[string]bool)
	for _, q := range a.Questions {
		if !bugStableID.MatchString(q.ID) || seen[q.ID] {
			faults = append(faults, fmt.Errorf("bug analysis: invalid or duplicate question %q", q.ID))
		}
		seen[q.ID] = true
		required("question", q.Text)
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		faults = append(faults, err)
	} else if len(encoded) > 1<<20 {
		faults = append(faults, errors.New("bug analysis exceeds 1 MiB"))
	}
	return errors.Join(faults...)
}

// Digest binds the complete canonical structured proposal, including references.
func (a BugAnalysis) Digest() (string, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
