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

// Validate checks operation shape independently from factory authorization.
func (r PlanStoryRequest) Validate() error {
	if !slices.Contains([]string{"create", "update", "retire"}, r.Operation) || !slices.Contains([]string{"story", "technical", "acceptance"}, r.Stage) {
		return errors.New("unsupported Plan story operation or stage")
	}
	for _, ref := range append([]string{r.RequestID, r.Candidate, r.SharedRevision}, r.Upstream...) {
		if strings.TrimSpace(ref) == "" || len(ref) > 256 {
			return errors.New("bounded nonempty request/candidate/shared/upstream references required")
		}
	}
	if len(r.Upstream) > 100 || len(r.ChangedSections) > 9 || len(r.Replacements) > 100 {
		return errors.New("plan story references exceed bounds")
	}
	if r.Operation != "create" && len(r.ExpectedRevision) != 64 {
		return errors.New("update/retire requires expected content revision")
	}
	if r.Operation == "create" && (r.ExpectedRevision != "" || len(r.ChangedSections) > 0 || r.Stage != "story") {
		return errors.New("create requires an initial story candidate")
	}
	if r.Operation != "retire" && len(r.Replacements) > 0 {
		return errors.New("replacement references require retirement")
	}
	if r.Operation == "retire" && len(r.ChangedSections) > 0 {
		return errors.New("retirement cannot edit candidate content")
	}
	seen := map[string]bool{}
	for _, v := range r.Upstream {
		if seen[v] {
			return fmt.Errorf("duplicate upstream %q", v)
		}
		seen[v] = true
	}
	seen = map[string]bool{}
	for _, key := range r.Replacements {
		if strings.TrimSpace(key) == "" || len(key) > 256 || seen[key] {
			return errors.New("invalid or duplicate replacement")
		}
		seen[key] = true
	}
	return nil
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
