package agentresult

import "slices"

// ValidateResponseDone is the response of a reviewer that finished its assessment.
const ValidateResponseDone = "done"

// DecodeValidateNode reads back the retained node of a Validate repair or review
// agent. The application result was judged once, when the agent submitted it;
// this reads the envelope only. A repair node's response must equal its repair
// report's response. A reviewer answers done with a completed result, or blocked
// with an unfinished one.
func DecodeValidateNode(data []byte, ctx Context) (ResearchNode, Diagnostics) {
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
	if ctx.Mode != "validate" {
		add(&d, "$", "scope", "Validate invocation", "different mode", "Use the factory-selected Validate mode.")
	}
	switch {
	case ctx.RepairRole:
		repair := node.Result.Payload.Validate
		if repair == nil || repair.Repair == nil {
			add(&d, "$.result.payload.validate.repair", "required", "a repair report", "absent", "Report the repair.")
		} else if node.Response != repair.Repair.Response || !slices.Contains(RepairResponses, node.Response) {
			add(&d, "$.response", "consistency", "response equal to the repair report's response", "inconsistent result", "Answer repaired, handback or blocked, as your repair report says.")
		}
	default:
		want := ValidateResponseDone
		if node.Result.Outcome != "completed" {
			want = "blocked"
		}
		if node.Response != want {
			add(&d, "$.response", "consistency", "done for a completed assessment, blocked otherwise", "inconsistent result", "A reviewer answers done with a completed result.")
		}
	}
	finish(&d)
	if !d.Valid {
		return ResearchNode{}, d
	}
	return node, d
}
