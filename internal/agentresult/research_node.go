package agentresult

import (
	"encoding/json"
)

// MaxResearchNodeBytes bounds the complete node envelope before decoding.
const MaxResearchNodeBytes = 1 << 20

// ResearchNode is private workflow output. It contains no factory identities or
// publication authority; Result uses the ordinary application-result contract.
type ResearchNode struct {
	Version  int    `json:"version"`
	Response string `json:"response"`
	Result   Result `json:"result"`
}

// DecodeResearchNode validates both the envelope and the existing application
// result. No candidate escapes a failed or incomplete inspection.
func DecodeResearchNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	return decodeNodeFor(data, ctx, nodeResearch)
}

// DecodePlanNode validates a graph Plan node the way DecodeResearchNode validates
// a Research node, over the same envelope and the same application result. A Plan
// role answers ask or done and never approved: only the factory-bound owner path
// promotes anything, so a role's own report of approval is refused by name.
func DecodePlanNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	return decodeNodeFor(data, ctx, nodePlan)
}

// DecodeSpikeNode validates a graph Spike node over the same envelope and the
// same application result. A Spike role answers ask or done.
func DecodeSpikeNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	return decodeNodeFor(data, ctx, nodeSpike)
}

// DecodeBugNode validates a graph Bug node exactly as a Spike node, in Bug mode.
func DecodeBugNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	return decodeNodeFor(data, ctx, nodeBug)
}

// DecodeDoctorNode validates a graph Doctor node over the same envelope and the
// same application result. A Doctor role answers ask or done.
func DecodeDoctorNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	return decodeNodeFor(data, ctx, nodeDoctor)
}

type nodeVocabulary int

const (
	nodeResearch nodeVocabulary = iota
	nodePlan
	nodeSpike
	nodeBug
	nodeDoctor
)

// decodeNodeFor is the one envelope decoder. The vocabulary selects which mode
// the invocation must be. Every role answers ask or done; approval belongs to
// the owner and is never a role response. The node is the retained result the
// agent submitted through the shared result route, never terminal text.
func decodeNodeFor(data []byte, ctx Context, vocabulary nodeVocabulary) (ResearchNode, Diagnostics) {
	plan := vocabulary == nodePlan
	d := newDiagnostics()
	if len(data) > MaxResearchNodeBytes {
		add(&d, "$", "limit", "node JSON at most 1 MiB", "oversized", "Reduce the node output and reference saved files.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
		finish(&d)
		return ResearchNode{}, d
	}
	var node ResearchNode
	d = Decode(data, &node)
	if !d.Valid {
		return ResearchNode{}, d
	}
	if node.Version != 1 {
		add(&d, "$.version", "version", "1", "unsupported version", "Use node schema version 1.")
	}
	switch {
	case plan && ctx.Mode != "plan":
		add(&d, "$", "scope", "Plan invocation", "different mode", "Use the factory-selected Plan mode.")
	case vocabulary == nodeSpike && ctx.Mode != "spike":
		add(&d, "$", "scope", "Spike invocation", "different mode", "Use the factory-selected Spike mode.")
	case vocabulary == nodeBug && ctx.Mode != "bug":
		add(&d, "$", "scope", "Bug invocation", "different mode", "Use the factory-selected Bug mode.")
	case vocabulary == nodeDoctor && ctx.Mode != "doctor":
		add(&d, "$", "scope", "Doctor invocation", "different mode", "Use the factory-selected Doctor mode.")
	case vocabulary == nodeResearch && ctx.Mode != "research":
		add(&d, "$", "scope", "Research invocation", "different mode", "Use the factory-selected Research mode.")
	}
	switch node.Response {
	case "ask":
		if node.Result.Outcome != "incomplete" || len(node.Result.Questions) == 0 {
			add(&d, "$.response", "consistency", "ask with incomplete result and questions", "inconsistent result", "Supply permitted owner questions with an incomplete result.")
		}
	case "done":
		if node.Result.Outcome != "completed" || len(node.Result.Questions) != 0 {
			advice := "Return done only with a completed result and no questions; use ask for owner questions."
			if plan {
				advice = "Return done only with a completed result, no questions and exactly one story request or story set; use ask for owner questions."
			}
			add(&d, "$.response", "consistency", "done with completed result without questions", "inconsistent result", advice)
		}
	case "approved":
		advice := "Return ask for owner questions or done for finished work; approval belongs to the owner and is never a role response."
		if plan {
			advice = "Plan roles return ask for owner questions or done for a story request or story set; approval belongs to the owner and is never a role response."
		}
		add(&d, "$.response", "enum", "ask or done", "approved", advice)
	default:
		advice := "Choose ask for owner questions, or done for finished work."
		if plan {
			advice = "Choose ask for owner questions, or done to return exactly one story request or story set."
		}
		add(&d, "$.response", "enum", "ask or done", "unsupported response", advice)
	}
	// Pass the original result bytes to Create so presence-sensitive optional
	// fields receive exactly the same checks as application submissions.
	var raw struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		add(&d, "$", "encoding", "valid JSON", "invalid", "Correct the node JSON.")
	} else {
		wrapped := append([]byte(`{"result":`), raw.Result...)
		wrapped = append(wrapped, '}')
		_, app := Create(wrapped, ctx)
		mergeDiagnostics(&d, app, "$")
	}
	finish(&d)
	if !d.Valid {
		return ResearchNode{}, d
	}
	return node, d
}
