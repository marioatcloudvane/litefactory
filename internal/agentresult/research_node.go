package agentresult

import "encoding/json"

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
	d := newDiagnostics()
	if len(data) > MaxResearchNodeBytes {
		add(&d, "$", "limit", "node JSON at most 1 MiB", "oversized", "Reduce the node output and reference saved findings.")
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
	if ctx.Mode != "research" {
		add(&d, "$", "scope", "Research invocation", "different mode", "Use the factory-selected Research mode.")
	}
	switch node.Response {
	case "ask":
		if node.Result.Outcome != "incomplete" || len(node.Result.Questions) == 0 {
			add(&d, "$.response", "consistency", "ask with incomplete result and questions", "inconsistent result", "Supply permitted owner questions with an incomplete result.")
		}
	case "done":
		if node.Result.Outcome != "completed" || len(node.Result.Questions) != 0 {
			add(&d, "$.response", "consistency", "done with completed result without questions", "inconsistent result", "Complete Research only when no owner questions remain.")
		}
	default:
		add(&d, "$.response", "enum", "ask or done", "unsupported response", "Choose ask for owner questions or done for completion.")
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
