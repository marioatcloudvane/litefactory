package submission

import (
	"encoding/json"
	"errors"
	"slices"
)

// FamilyCatalogVersion changes when immutable family call permissions change.
const FamilyCatalogVersion = "families-v4"

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
	// Drafting retains a proposal and publishes nothing, so it needs no grant.
	// The creation flag means a live grant exists and must never gate this tool;
	// installation, not that flag, is the story-creation gate.
	if name == "issues_create_prepare" {
		return mode == "research"
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
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"state":{"type":"string"},"page":{"type":"integer"},"per_page":{"type":"integer"},"include_mode_labels":{"type":"boolean"}},"additionalProperties":false}`)
	case "issues_get":
		spec.Description = "Read a repository-local issue summary."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"issue_number":{"type":"integer"},"state":{"type":"string"},"include_mode_labels":{"type":"boolean"}},"required":["issue_number"],"additionalProperties":false}`)
	case "issues_comment":
		spec.Description = "Publish a receipted comment to an explicitly authorized existing issue."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"issue_number":{"type":"integer"},"message":{"type":"string"}},"required":["issue_number","message"],"additionalProperties":false}`)
		spec.Effect = "publication"
	case "issues_create_prepare":
		spec.Description = "Draft 1 to 5 issues the owner asked to have created and have the factory retain them as a creation proposal. Call it only when the owner's request asks for issues or stories to be created. It publishes nothing and needs no approval; the factory presents the proposal to the owner, who decides. Repository, source task and version are supplied by the factory. The whole proposal must stay under about 48000 bytes; a larger one is refused, so shorten it or split the work. Recommendation: split large work into several focused issues rather than one big one; this is advice only and never blocks a proposal."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"relationship":{"type":"string","enum":["linked","attach"],"description":"linked (default) links each draft to the source issue; attach also attaches each draft as a sub-issue of the source issue."},"drafts":{"type":"array","minItems":1,"maxItems":5,"items":{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":256},"body":{"type":"string","minLength":1,"maxLength":65536},"parent_issue":{"type":"integer","minimum":1,"description":"Only with attach; must be the source issue."},"link_issues":{"type":"array","maxItems":16,"items":{"type":"integer","minimum":1}}},"required":["title","body"],"additionalProperties":false}}},"required":["drafts"],"additionalProperties":false}`)
		spec.Effect = "input"
	case "issues_create_batch":
		spec.Description = "Publish an exactly reviewed batch using the factory-held live creation grant."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"envelope":{"type":"object","description":"The exact reviewed creation envelope."},"hash":{"type":"string"}},"required":["envelope","hash"],"additionalProperties":false}`)
		spec.Effect = "creation"
	case "evidence_submit":
		spec.Description = "Apply the permitted shared stage checkpoint; engineering and acceptance remain separate."
		// Stage-specific active fields stay with the server: it already forbids
		// fields inactive for the stage and answers with typed diagnostics.
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"stage":{"type":"string"},"story":{"type":"string"},"outcome":{"type":"string"},"reason":{"type":"string"},"capture":{"type":"object"},"plan":{"description":"Typed Plan content for the plan stage."},"context":{"type":"object"},"engineering":{"type":"object"},"assessment":{"type":"object"}},"required":["stage","outcome"],"additionalProperties":false}`)
		spec.Effect = "checkpoint"
	case "activity_update":
		spec.Description = "Report advisory activity. Implement story workers require kind and receive durable receipts; cannot record completion or proof."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"sequence":{"type":"integer","minimum":1},"message":{"type":"string","minLength":1},"kind":{"type":"string","enum":["implementation","testing","repair"]}},"required":["sequence","message"],"additionalProperties":false}`)
		spec.Effect = "advisory"
	case "activity_get":
		spec.Description = "Read latest durable Implement worker activity, or process-local activity in other modes."
		// Genuinely argument-free: the handler decodes into an empty struct.
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	case "receipts_get":
		spec.Description = "Observe a scoped retained request without redispatch or a recursive receipt."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}},"required":["request_id"],"additionalProperties":false}`)
	default:
		return ToolSpec{}, false
	}
	return spec, true
}
