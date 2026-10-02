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

// validationEnvelope returns the JSON object inside a reviewer's final message,
// discarding whatever it wrapped that object in.
//
// The contract asks for a bare object and no Markdown wrapper. A model that
// leads with "Based on direct inspection…" and then fences the object has still
// done the review; on issue #207 that cost a complete twelve-story assessment,
// 24 turns and $1.32, thrown away on the "B" of "Based". Refusing an envelope
// buys nothing — every check that matters is applied to the object itself, and
// the identity checks below (parent, generation, story count, and per-story
// issue id, revision and snapshot) are what make a wrong object impossible to
// mistake for the right one. Strictness inside the object is unchanged:
// unknown fields, duplicate stories and trailing data after it are still
// rejected.
//
// The first "{" to the last "}" spans a fenced block and a prose preamble
// alike, so neither is special-cased. Prose after the object that carries its
// own brace overshoots and fails to parse, which is what happens today anyway.
func validationEnvelope(data []byte) []byte { return JSONEnvelope(data) }

// JSONEnvelope narrows terminal agent text to the JSON object inside it.
//
// An agent asked for one strict JSON object routinely returns one wrapped in a
// sentence of commentary or a Markdown fence, and reads its instruction as
// satisfied. Refusing that is not rigour, it is discarding finished work over
// punctuation: a Research run did exactly this, completing its investigation
// and durably submitting its result, only for the terminal echo to be rejected
// for arriving inside ```json.
//
// The bound is the caller's, and it belongs on the raw text as well as on what
// comes out, or a large preamble becomes a way past it. Taking the last brace
// is a heuristic: prose after the object containing one takes the wrong slice.
// That trade has been Validate's for some time and is why the decoders that
// follow still validate everything.
func JSONEnvelope(data []byte) []byte {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed
	}
	start := bytes.IndexByte(trimmed, '{')
	end := bytes.LastIndexByte(trimmed, '}')
	if start < 0 || end <= start {
		return trimmed
	}
	return bytes.TrimSpace(trimmed[start : end+1])
}

// ParseValidationOutput accepts only one bounded exact JSON object and complete
// criterion/review coverage. Evidence floors are enforced independently of prose.
func ParseValidationOutput(data []byte, scope *ValidationContext) (ValidationOutput, error) {
	var out ValidationOutput
	if scope == nil || scope.Version != 1 || len(data) > MaxValidationBytes {
		return out, fmt.Errorf("invalid validation scope or output size")
	}
	data = validationEnvelope(data)
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
				// Missing initial proof does not erase a concrete reviewed defect.
				if f.Status == "unmet" {
					continue
				}
				f.Status = "unverified"
				f.Classification = "gap"
				prefix := "Initial engineering evidence unverified: " + s.EvidenceReason + ". Review: "
				if !strings.HasPrefix(f.Evidence, prefix) {
					f.Evidence = prefix + f.Evidence
				}
			case "failed":
				f.Status = "unmet"
				f.Classification = "blocking"
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
	for i, f := range findings {
		if !expected[f.Name] {
			return fmt.Errorf("duplicate or unknown finding %q", f.Name)
		}
		delete(expected, f.Name)
		if f.Status != "satisfied" && f.Status != "unmet" && f.Status != "unverified" {
			return fmt.Errorf("unsupported validation status")
		}
		if !validFindingClassification(f.Status, f.Classification, criteria) || strings.TrimSpace(f.Evidence) == "" {
			return fmt.Errorf("missing proof or invalid finding classification")
		}
		if f.Status == "unverified" {
			findings[i].Classification = "gap"
		}
	}
	return nil
}

// validFindingClassification accepts historical classifications at ingestion;
// unverified findings are normalized to gaps before retention. This describes
// evidence, never satisfaction or an owner waiver.
func validFindingClassification(status, classification string, criteria bool) bool {
	if status == "unverified" {
		return classification == "gap" || classification == "blocking" || classification == "advisory"
	}
	return (classification == "blocking" || classification == "advisory") && (!criteria || classification == "blocking")
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
