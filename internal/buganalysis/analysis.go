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
	Prevention       *BugPrevention       `json:"prevention,omitempty"`
}

// BugPrevention is the analyst's note on whether the book of standards would
// have prevented the bug. It is optional so earlier analyses stay valid; the
// judgement is the analyst's, the factory only checks the shape.
type BugPrevention struct {
	Kind       string `json:"kind"`
	StandardID string `json:"standard_id,omitempty"`
	Note       string `json:"note"`
}

// Prevention kinds the analyst can record.
const (
	PreventionMissingStandard = "missing_standard"
	PreventionIgnoredStandard = "ignored_standard"
	PreventionOneOff          = "one_off"
)

// Validate checks the note is well-formed: a known kind, a standard ID exactly
// when an existing standard was ignored, and a nonblank note.
func (p BugPrevention) Validate() error {
	var faults []error
	switch p.Kind {
	case PreventionMissingStandard, PreventionIgnoredStandard, PreventionOneOff:
	default:
		faults = append(faults, fmt.Errorf("bug analysis: prevention kind %q is not missing_standard, ignored_standard or one_off", p.Kind))
	}
	id := strings.TrimSpace(p.StandardID)
	if p.Kind == PreventionIgnoredStandard && id == "" {
		faults = append(faults, errors.New("bug analysis: an ignored_standard prevention note names the standard_id"))
	}
	if p.Kind != PreventionIgnoredStandard && p.StandardID != "" {
		faults = append(faults, errors.New("bug analysis: standard_id belongs only to an ignored_standard prevention note"))
	}
	if strings.TrimSpace(p.Note) == "" {
		faults = append(faults, errors.New("bug analysis: prevention note is blank"))
	}
	return errors.Join(faults...)
}

// BugRepairBoundaries is a typed detail of the same owner-reviewed remedy.
// Exact repository-relative files include intended new regression files; this
// is neither a new approval gate nor a substitute for semantic role duties.
type BugRepairBoundaries struct {
	AllowedPaths  []string `json:"allowed_paths"`
	ExcludedPaths []string `json:"excluded_paths,omitempty"`
}

// Notice is a policy-class observation Validate makes instead of refusing:
// the caller records it as a fact and continues (REFUSAL_REVIEW.md, T-003).
// Reference is stable across replays; Text is short and plain-language.
type Notice struct {
	Reference string
	Text      string
}

// Validate checks that approved boundaries name concrete repository paths. An
// escaping or malformed path stays a hard error (integrity: it could name
// something outside the project). The 1-256 count bound and a repeated path
// are policy, not integrity — nothing unapproved can merge, close or spend
// budget over a remedy that merely lists more paths than usual or repeats
// one; the owner still reads the approved remedy at analysis review
// (REFUSAL_REVIEW.md: buganalysis.boundaries_count, buganalysis.boundaries_duplicate).
func (b BugRepairBoundaries) Validate() ([]Notice, error) {
	if len(b.AllowedPaths) == 0 {
		return nil, errors.New("bug analysis: bounded remedy needs at least one explicit allowed path")
	}
	var notices []Notice
	if len(b.AllowedPaths) > 256 || len(b.ExcludedPaths) > 256 {
		notices = append(notices, Notice{
			Reference: "buganalysis.boundaries_count",
			Text:      fmt.Sprintf("remedy boundaries list %d allowed and %d excluded paths, beyond the usual 256-path guidance", len(b.AllowedPaths), len(b.ExcludedPaths)),
		})
	}
	seen := map[string]bool{}
	for _, paths := range [][]string{b.AllowedPaths, b.ExcludedPaths} {
		for _, p := range paths {
			if !fs.ValidPath(p) || p == "." || strings.ContainsAny(p, "\\\x00\r\n*?[") {
				return notices, errors.New("bug analysis: invalid or escaping remedy path")
			}
			if seen[p] {
				notices = append(notices, Notice{
					Reference: "buganalysis.boundaries_duplicate:" + p,
					Text:      fmt.Sprintf("remedy boundaries repeat path %q", p),
				})
				continue
			}
			seen[p] = true
		}
	}
	return notices, nil
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
// The returned notices are policy-class observations (from Boundaries.Validate)
// that the caller records as facts instead of refusing; every other rule here
// protects the minimum content a review needs and stays a hard error.
func (a BugAnalysis) Validate(review bool) ([]Notice, error) {
	var faults []error
	var notices []Notice
	if a.Boundaries != nil {
		n, err := a.Boundaries.Validate()
		notices = append(notices, n...)
		if err != nil {
			faults = append(faults, err)
		}
	}
	if a.Prevention != nil {
		if err := a.Prevention.Validate(); err != nil {
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
		if (len(a.Reproduction.Steps) == 0 && a.Reproduction.Observation != "not_observed") || len(a.Evidence) == 0 || len(a.Exclusions) == 0 || len(a.Criteria) == 0 {
			faults = append(faults, errors.New("bug analysis: review needs reproduction steps or explicitly not-observed alternative evidence, exclusions and measurable criteria"))
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
		if review && q.Material {
			faults = append(faults, fmt.Errorf("bug analysis: material question %q remains unresolved", q.ID))
		}
		required("question", q.Text)
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		faults = append(faults, err)
	} else if len(encoded) > 1<<20 {
		faults = append(faults, errors.New("bug analysis exceeds 1 MiB"))
	}
	return notices, errors.Join(faults...)
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
