package agentresult

// Responses of a graph Implement story worker. They name the outcome of the
// accepted application result and nothing else.
const (
	ImplementResponseCompleted  = "completed"
	ImplementResponseIncomplete = "incomplete"
	ImplementResponseBlocked    = "blocked"
)

// DecodeImplementNode reads back the retained node of a graph Implement story
// worker. The application result was judged once, when the worker submitted it,
// against the one story it was given; this reads the envelope only, so bytes
// changed after the submit never fail a result that was accepted. The response
// must agree with the result's outcome, and the invocation must be Implement.
func DecodeImplementNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
	d := newDiagnostics()
	if len(data) > MaxResearchNodeBytes {
		add(&d, "$", "limit", "node JSON at most 1 MiB", "oversized", "Reduce the node output.")
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
	if ctx.Mode != "implement" {
		add(&d, "$", "scope", "Implement invocation", "different mode", "Use the factory-selected Implement mode.")
	}
	switch node.Response {
	case ImplementResponseCompleted, ImplementResponseIncomplete, ImplementResponseBlocked:
		if node.Response != node.Result.Outcome {
			add(&d, "$.response", "consistency", "response equal to the result outcome", "inconsistent result", "The response names the outcome of the accepted result.")
		}
	default:
		add(&d, "$.response", "enum", "completed, incomplete or blocked", "unsupported response", "Choose the outcome of your result.")
	}
	finish(&d)
	if !d.Valid {
		return ResearchNode{}, d
	}
	return node, d
}
