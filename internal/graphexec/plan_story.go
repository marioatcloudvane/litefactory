package graphexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// PlanStoryAction is the single Plan-specific effect capability.
const PlanStoryAction = "lf_plan_story"

// PlanStoryRequest contains content, never parent/issue/owner authority.
type PlanStoryRequest struct {
	RequestID        string              `json:"request_id"`
	Operation        string              `json:"operation"`
	Stage            string              `json:"stage"`
	Candidate        string              `json:"candidate"`
	ExpectedRevision string              `json:"expected_revision,omitempty"`
	Upstream         []string            `json:"upstream"`
	SharedRevision   string              `json:"shared_revision"`
	Draft            planbrief.PlanDraft `json:"draft"`
	ChangedSections  []string            `json:"changed_sections,omitempty"`
	Replacements     []string            `json:"replacements,omitempty"`
}

// PlanStorySpec declares the activity outcomes without executing effects.
func PlanStorySpec() ActionSpec {
	return ActionSpec{Name: PlanStoryAction, Mode: agent.ModePlan, Outcomes: []string{"applied", "conflict", "failed", "uncertain"}}
}

// ParsePlanStoryRequest refuses unknown fields, trailing values and oversize
// submissions before the effect owner can reserve identity or contact the forge.
func ParsePlanStoryRequest(data []byte) (PlanStoryRequest, error) {
	var r PlanStoryRequest
	if len(data) > 128*1024 {
		return r, errors.New("plan story request exceeds 128 KiB")
	}
	if err := uniquePlanJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return r, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, err
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return r, errors.New("trailing Plan story data")
	}
	if err := r.Validate(); err != nil {
		return PlanStoryRequest{}, err
	}
	return r, nil
}

// PlanStoryFault is one refused request field with the correction to make. Field
// is the JSON member name the correction concerns, so a caller can point at it.
type PlanStoryFault struct {
	Field   string
	Message string
}

// Validate checks operation shape independently from factory authorization. It
// reports every fault at once, joined in field order, so one correction pass can
// fix them all.
func (r PlanStoryRequest) Validate() error {
	faults := r.Faults()
	if len(faults) == 0 {
		return nil
	}
	messages := make([]string, len(faults))
	for i, f := range faults {
		messages[i] = f.Message
	}
	return errors.New(strings.Join(messages, "; "))
}

// Faults returns the shape faults of a complete request, each naming the field
// to correct. Nothing is returned for a request the effect owner may accept.
func (r PlanStoryRequest) Faults() []PlanStoryFault {
	var faults []PlanStoryFault
	fault := func(field, format string, args ...any) {
		faults = append(faults, PlanStoryFault{Field: field, Message: fmt.Sprintf(format, args...)})
	}
	operationKnown := slices.Contains([]string{"create", "update", "retire"}, r.Operation)
	stageKnown := slices.Contains([]string{"story", "technical", "acceptance"}, r.Stage)
	if !operationKnown {
		fault("operation", "unsupported Plan story operation or stage: operation must be create, update or retire")
	}
	if !stageKnown {
		fault("stage", "unsupported Plan story operation or stage: stage must be story, technical or acceptance")
	}
	refs := []struct{ field, value string }{{"request_id", r.RequestID}, {"candidate", r.Candidate}, {"shared_revision", r.SharedRevision}}
	for _, u := range r.Upstream {
		refs = append(refs, struct{ field, value string }{"upstream", u})
	}
	for _, ref := range refs {
		if strings.TrimSpace(ref.value) == "" || len(ref.value) > 256 {
			fault(ref.field, "bounded nonempty request/candidate/shared/upstream references required: %s is blank or over 256 bytes", ref.field)
		}
	}
	if len(r.Upstream) > 100 || len(r.ChangedSections) > 9 || len(r.Replacements) > 100 {
		fault("changed_sections", "plan story references exceed bounds")
	}
	if !operationKnown {
		return faults
	}
	if r.Operation != "create" && len(r.ExpectedRevision) != 64 {
		fault("expected_revision", "update/retire requires expected content revision: set expected_revision to the 64-character revision of the story being changed")
	}
	if r.Operation == "create" {
		// Three separate messages, because each names a different field and a
		// different correction. One shared sentence told the agent nothing about
		// which of its fields to remove.
		if len(r.ChangedSections) > 0 {
			fault("changed_sections", "create must not carry changed_sections: remove changed_sections; a section list is valid only when updating an existing story")
		}
		if r.ExpectedRevision != "" {
			fault("expected_revision", "create must not carry expected_revision: remove expected_revision; a new story has no revision to expect")
		}
		if stageKnown && r.Stage != "story" {
			fault("stage", "create is valid only at the story stage, but the stage is %q: use operation update to change an existing story at this stage", r.Stage)
		}
	}
	if r.Operation != "retire" && len(r.Replacements) > 0 {
		fault("replacements", "replacement references require retirement: remove replacements or use operation retire")
	}
	if r.Operation == "retire" && len(r.ChangedSections) > 0 {
		fault("changed_sections", "retirement cannot edit candidate content: remove changed_sections")
	}
	seen := map[string]bool{}
	for _, v := range r.Upstream {
		if seen[v] {
			fault("upstream", "duplicate upstream %q", v)
		}
		seen[v] = true
	}
	seen = map[string]bool{}
	for _, key := range r.Replacements {
		if strings.TrimSpace(key) == "" || len(key) > 256 || seen[key] {
			fault("replacements", "invalid or duplicate replacement")
		}
		seen[key] = true
	}
	return faults
}

// PlanSectionFaults applies the per-stage authority over changed sections: the
// story role may touch only intent, dependencies and conversation, and the
// technical role may not touch criteria or tasks. Section names themselves are
// checked when the change is merged.
func PlanSectionFaults(stage string, sections []string) []PlanStoryFault {
	var faults []PlanStoryFault
	for _, section := range sections {
		if stage == "story" && section != "intent" && section != "dependencies" && section != "conversation" {
			faults = append(faults, PlanStoryFault{Field: "changed_sections", Message: fmt.Sprintf("story role cannot author later-stage sections: remove %q from changed_sections (the story stage may change intent, dependencies and conversation)", section)})
		}
		if stage == "technical" && (section == "criteria" || section == "tasks") {
			faults = append(faults, PlanStoryFault{Field: "changed_sections", Message: fmt.Sprintf("technical role cannot author acceptance sections: remove %q from changed_sections", section)})
		}
	}
	return faults
}

// Reject duplicate object members at every depth rather than accepting the
// encoding/json last-key-wins behavior for security-relevant operation fields.
func uniquePlanJSON(dec *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("plan JSON nesting exceeds bound")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return errors.New("invalid or duplicate Plan JSON member")
			}
			seen[key] = true
			if err := uniquePlanJSON(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := uniquePlanJSON(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid Plan JSON delimiter")
	}
	_, err = dec.Token()
	return err
}
