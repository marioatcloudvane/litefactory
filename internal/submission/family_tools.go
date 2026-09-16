package submission

import (
	"encoding/json"
	"errors"
	"slices"
)

// FamilyCatalogVersion changes when immutable family call permissions change.
const FamilyCatalogVersion = "families-v2"

// NewFamilyToolProfile enables only explicitly installed catalog entries. Mode
// restrictions and the default-off creation gate cannot be supplied by callers.
func NewFamilyToolProfile(mode string, allowCreation bool, installed []string) (ToolProfile, error) {
	p, err := NewToolProfile(mode)
	if err != nil {
		return p, err
	}
	p.creation = allowCreation
	for _, name := range installed {
		if _, ok := familyTool(name); !ok || slices.Contains(p.families, name) {
			return ToolProfile{}, errors.New("invalid or duplicate family registration")
		}
		p.families = append(p.families, name)
	}
	slices.Sort(p.families)
	return p, nil
}

func familyAllowed(mode string, creation bool, name string) bool {
	if mode != "research" && mode != "implement" && mode != "validate" {
		return false
	}
	if name == "issues_create_batch" {
		return mode == "research" && creation
	}
	if name == "evidence_submit" {
		return mode == "implement" || mode == "validate"
	}
	return true
}

func familyTool(name string) (ToolSpec, bool) {
	spec := ToolSpec{Name: name, Effect: "read", InputSchema: json.RawMessage(`{"type":"object"}`)}
	switch name {
	case "feedback_preflight":
		spec.Description = "Check application, checkpoint or creation input with shared validators; grants no approval or proof."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string","enum":["application","checkpoint","creation"]},"input":{"type":"object"}},"required":["kind","input"],"additionalProperties":false}`)
	case "issues_list":
		spec.Description = "Read one bounded page of repository-local issue summaries."
	case "issues_get":
		spec.Description = "Read a repository-local issue summary."
	case "issues_comment":
		spec.Description = "Publish a receipted comment to an explicitly authorized existing issue."
		spec.Effect = "publication"
	case "issues_create_batch":
		spec.Description = "Publish an exactly reviewed batch using the factory-held live creation grant."
		spec.Effect = "creation"
	case "evidence_submit":
		spec.Description = "Apply the permitted shared stage checkpoint; engineering and acceptance remain separate."
		spec.Effect = "checkpoint"
	case "activity_update":
		spec.Description = "Report bounded advisory activity; cannot record proof or extend authority."
		spec.Effect = "advisory"
	case "activity_get":
		spec.Description = "Read current process-local advisory activity or unavailable state."
	case "receipts_get":
		spec.Description = "Observe a scoped retained request without redispatch or a recursive receipt."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)
	default:
		return ToolSpec{}, false
	}
	return spec, true
}
