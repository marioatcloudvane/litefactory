package submission

import (
	"encoding/json"
	"errors"
	"slices"
)

// FamilyCatalogVersion changes when immutable family call permissions change.
const FamilyCatalogVersion = "families-v5"

// NewFamilyToolProfile enables only explicitly installed catalog entries. Mode
// restrictions cannot be supplied by callers.
func NewFamilyToolProfile(mode string, installed []string) (ToolProfile, error) {
	p, err := NewToolProfile(mode)
	if err != nil {
		return p, err
	}
	for _, name := range installed {
		if _, ok := familyTool(name); !ok || slices.Contains(p.families, name) {
			return ToolProfile{}, errors.New("invalid or duplicate family registration")
		}
		p.families = append(p.families, name)
	}
	slices.Sort(p.families)
	return p, nil
}

func familyAllowed(mode string, name string) bool {
	if name == "standards_categories_list" || name == "standards_select" || name == "standards_read" {
		return slices.Contains([]string{"research", "spike", "plan", "implement", "validate"}, mode)
	}
	if mode != "research" && mode != "implement" && mode != "validate" {
		return false
	}
	// Drafting retains a proposal and publishes nothing, so it needs no grant.
	// Installation is the story-creation gate.
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
	case "standards_categories_list":
		spec.Description = "Discover categories from the invocation's verified retained standards book; advisory data grants no authority."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"page_size":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string","maxLength":4096}},"additionalProperties":false}`)
	case "standards_select":
		spec.Description = "Record an explicit initial or additive category choice for this logical consumer; does not approve or publish standards."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"expected_version":{"type":"integer","minimum":0},"category_ids":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":256}},"reason":{"type":"string","minLength":1,"maxLength":512}},"required":["expected_version","category_ids","reason"],"additionalProperties":false}`)
		spec.Effect = "advisory"
	case "standards_read":
		spec.Description = "Read bounded exact retained standards pages. Follow every cursor; complete acknowledges offered ranges as an agent report, not proof of comprehension."
		spec.InputSchema = json.RawMessage(`{"type":"object","properties":{"selection_version":{"type":"integer","minimum":1},"selection_digest":{"type":"string","minLength":1,"maxLength":128},"cursor":{"type":"string","maxLength":4096},"complete":{"type":"boolean"}},"required":["selection_version","selection_digest"],"additionalProperties":false}`)
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
