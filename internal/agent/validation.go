package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// MaxValidationBytes bounds the structured terminal result, not a repository file.
const MaxValidationBytes = 4 << 20

// ValidationContext is factory-retained scope. EvidenceStatus describes original
// engineering facts; a reviewer cannot turn missing or failed checks into passes.
type ValidationContext struct {
	Version      int               `json:"version"`
	Parent       string            `json:"parent"`
	Generation   int               `json:"generation"`
	Stories      []ValidationStory `json:"stories"`
	Dependencies []CompletedStory  `json:"dependencies,omitempty"`
}

// ValidationStory binds one frozen story to its retained engineering evidence.
type ValidationStory struct {
	Story          ApprovedStory  `json:"story"`
	Criteria       []string       `json:"criteria"`
	Snapshot       string         `json:"snapshot"`
	Evidence       *StoryEvidence `json:"evidence,omitempty"`
	EvidenceStatus string         `json:"evidence_status"`
	EvidenceReason string         `json:"evidence_reason,omitempty"`
}

// Clone deep-copies the frozen context across runner boundaries.
func (v *ValidationContext) Clone() *ValidationContext {
	if v == nil {
		return nil
	}
	c := *v
	c.Dependencies = slices.Clone(v.Dependencies)
	c.Stories = slices.Clone(v.Stories)
	for i := range c.Stories {
		s := &c.Stories[i]
		s.Story.Dependencies = slices.Clone(s.Story.Dependencies)
		s.Criteria = slices.Clone(s.Criteria)
		if s.Evidence != nil {
			e := *s.Evidence
			e.Files = slices.Clone(e.Files)
			for j := range e.Files {
				e.Files[j].Content = slices.Clone(e.Files[j].Content)
			}
			e.Acceptance = slices.Clone(e.Acceptance)
			e.Review = slices.Clone(e.Review)
			e.Tests = slices.Clone(e.Tests)
			s.Evidence = &e
		}
	}
	return &c
}

// ValidationOutput is advisory until factory identity/coverage checks and durable
// decision processing succeed. Waived is deliberately not an agent status.
type ValidationOutput struct {
	Version    int                `json:"version"`
	Parent     string             `json:"parent"`
	Generation int                `json:"generation"`
	Stories    []ValidationResult `json:"stories"`
}

// ValidationResult attributes one story assessment to its revision and snapshot.
type ValidationResult struct {
	Key      string              `json:"key"`
	IssueID  int64               `json:"issue_id"`
	Revision string              `json:"revision"`
	Snapshot string              `json:"snapshot"`
	Criteria []ValidationFinding `json:"criteria"`
	Reviews  []ValidationFinding `json:"reviews"`
}

// ValidationFinding records a factual result and whether it blocks acceptance.
type ValidationFinding struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Classification string `json:"classification"`
	Evidence       string `json:"evidence"`
}

// ParseValidationOutput accepts only one bounded exact JSON object and complete
// criterion/review coverage. Evidence floors are enforced independently of prose.
func ParseValidationOutput(data []byte, scope *ValidationContext) (ValidationOutput, error) {
	var out ValidationOutput
	if scope == nil || scope.Version != 1 || len(data) > MaxValidationBytes {
		return out, fmt.Errorf("invalid validation scope or output size")
	}
	if err := uniqueStoryJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return out, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, fmt.Errorf("decode validation: %w", err)
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("validation output has trailing data")
	}
	if out.Version != 1 || out.Parent != scope.Parent || out.Generation != scope.Generation || len(out.Stories) != len(scope.Stories) || len(out.Stories) == 0 {
		return out, fmt.Errorf("validation identity or coverage mismatch")
	}
	known := map[string]ValidationStory{}
	for _, s := range scope.Stories {
		known[s.Story.Key] = s
	}
	for i := range out.Stories {
		r := &out.Stories[i]
		s, ok := known[r.Key]
		if !ok {
			return out, fmt.Errorf("duplicate or unknown story %s", r.Key)
		}
		delete(known, r.Key)
		if r.IssueID != s.Story.IssueID || r.Revision != s.Story.Revision || r.Snapshot != s.Snapshot {
			return out, fmt.Errorf("validation attribution mismatch for %s", r.Key)
		}
		if err := validationFindings(r.Criteria, s.Criteria, true); err != nil {
			return out, err
		}
		if err := validationFindings(r.Reviews, []string{"code", "architecture", "plan"}, false); err != nil {
			return out, err
		}
		for j := range r.Criteria {
			f := &r.Criteria[j]
			switch s.EvidenceStatus {
			case "unverified":
				f.Status = "unverified"
				prefix := "Initial engineering evidence unverified: " + s.EvidenceReason + ". Review: "
				if !strings.HasPrefix(f.Evidence, prefix) {
					f.Evidence = prefix + f.Evidence
				}
			case "failed":
				f.Status = "unmet"
				prefix := "Initial engineering checks failed: " + s.EvidenceReason + ". Review: "
				if !strings.HasPrefix(f.Evidence, prefix) {
					f.Evidence = prefix + f.Evidence
				}
			}
		}
	}
	return out, nil
}
func validationFindings(findings []ValidationFinding, names []string, criteria bool) error {
	if len(findings) != len(names) || len(names) == 0 {
		return fmt.Errorf("incomplete validation findings")
	}
	expected := map[string]bool{}
	for _, n := range names {
		expected[n] = true
	}
	for _, f := range findings {
		if !expected[f.Name] {
			return fmt.Errorf("duplicate or unknown finding %q", f.Name)
		}
		delete(expected, f.Name)
		if f.Status != "satisfied" && f.Status != "unmet" && f.Status != "unverified" {
			return fmt.Errorf("unsupported validation status")
		}
		if (f.Classification != "blocking" && f.Classification != "advisory") || (criteria && f.Classification != "blocking") || strings.TrimSpace(f.Evidence) == "" {
			return fmt.Errorf("missing proof or invalid finding classification")
		}
	}
	return nil
}

// ImplementSuggestion requires a concrete blocking defect, never missing evidence.
func (v ValidationOutput) ImplementSuggestion() string {
	for _, s := range v.Stories {
		for _, group := range [][]ValidationFinding{s.Criteria, s.Reviews} {
			for _, f := range group {
				if f.Status == "unmet" && f.Classification == "blocking" {
					return "You may explicitly request /implement for " + s.Key + " " + f.Name + ": " + f.Evidence + ". No mode transition was started."
				}
			}
		}
	}
	return ""
}

// ParseRetainedEngineering preserves factual failed checks while validating the
// same immutable envelope and snapshot structure as successful engineering.
// It does not authorize engineering completion or any new remote effect.
func ParseRetainedEngineering(data []byte) (StoryEvent, error) {
	var e StoryEvent
	if len(data) > MaxStoryEventBytes {
		return e, fmt.Errorf("retained engineering exceeds limit")
	}
	if err := uniqueStoryJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return e, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, err
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return e, fmt.Errorf("retained engineering has trailing data")
	}
	if e.Evidence == nil || e.Kind != "engineering_complete" {
		return e, fmt.Errorf("missing engineering facts")
	}
	structural := e
	evidence := *e.Evidence
	evidence.Tests = slices.Clone(evidence.Tests)
	for i := range evidence.Tests {
		evidence.Tests[i].Passed = true
	}
	structural.Evidence = &evidence
	if err := structural.Validate(); err != nil {
		return e, err
	}
	return e, nil
}
