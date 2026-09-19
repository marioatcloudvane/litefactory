package agentresult

import (
	"bytes"
	"encoding/json"

	"github.com/cloudvane/litefactory/internal/agent"
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
	d := newDiagnostics()
	// The bound applies to what the agent actually emitted, before narrowing:
	// otherwise an oversized preamble is a way past it.
	if len(data) > MaxResearchNodeBytes {
		add(&d, "$", "limit", "node JSON at most 1 MiB", "oversized", "Reduce the node output and reference saved findings.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
		finish(&d)
		return ResearchNode{}, d
	}
	// Terminal text, not a bare object: an agent that wraps its answer in a
	// sentence or a Markdown fence has still answered. Narrowing applies only
	// to text that is not itself JSON — an array, a string or a number at the
	// root is the wrong shape rather than the right one inside prose, and
	// hunting for braces inside it would accept what this contract refuses.
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) > 0 && !startsJSON(trimmed[0]) {
		data = agent.JSONEnvelope(data)
	}
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
	case "approved":
		// Reporting the owner's verdict, not forming one. The result has to be
		// as complete as a done result, because approving unfinished work is
		// not something an owner can be asked to mean.
		if node.Result.Outcome != "completed" || len(node.Result.Questions) != 0 {
			add(&d, "$.response", "consistency", "approved with completed result without questions", "inconsistent result", "Report the owner's approval only over work you would otherwise call done.")
		}
	default:
		add(&d, "$.response", "enum", "ask, done or approved", "unsupported response", "Choose ask for owner questions, done to show your work, or approved to report that the owner accepted it.")
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

// startsJSON reports whether text begins a JSON value. Such text is a document
// already and is judged as written, which is what keeps this narrowing from
// weakening the contract: an array holding the required object is not the
// required object, and a second value after the object is one value too many.
// Only text that does not begin as JSON — a sentence, a Markdown fence — is
// narrowed to the object inside it.
//
// The cost is that an object followed by commentary is still refused, because
// nothing distinguishes that from an object followed by another value without
// deciding which trailing bytes are prose. The diagnostic says so plainly, and
// refusing beats guessing.
func startsJSON(c byte) bool {
	switch c {
	case '{', '[', '"', 't', 'f', 'n', '-':
		return true
	}
	return c >= '0' && c <= '9'
}
