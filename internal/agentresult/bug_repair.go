package agentresult

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// validateBugRepair checks only what routing and the owner's review need: a
// response, its matching outcome, a reason when the fixer did not succeed and
// a readable summary. Git holds the change; the agent echoes nothing back.
func validateBugRepair(r Result, ctx Context, d *Diagnostics) {
	p := r.Payload.Bug
	if ctx.Workflow != agent.WorkflowBug || ctx.Mode != "" || ctx.Bug == nil || ctx.Bug.Role != "bug-fixer" {
		add(d, "$.result.payload.bug", "scope", "factory-bound Bug fixer invocation", "missing binding", "Use the current factory fixer binding.")
		return
	}
	if p.Analysis != nil || r.Recommendation != nil || len(r.Questions) != 0 {
		add(d, "$.result.payload.bug", "forbidden", "repair report only", "analysis, recommendation or questions present", "Report the repair; reshape when the remedy must change.")
	}
	switch p.Response {
	case "succeeded":
		if r.Outcome != "completed" {
			add(d, "$.result.outcome", "consistency", "completed", "mismatch", "Succeeded uses outcome completed.")
		}
	case "reshape":
		if r.Outcome != "incomplete" || strings.TrimSpace(r.Reason) == "" {
			add(d, "$.result.outcome", "consistency", "incomplete with a reason", "mismatch", "Explain why the remedy must change.")
		}
	case "blocked":
		if r.Outcome != "blocked" || strings.TrimSpace(r.Reason) == "" {
			add(d, "$.result.outcome", "consistency", "blocked with a reason", "mismatch", "Explain the obstacle.")
		}
	default:
		add(d, "$.result.payload.bug.response", "enum", "succeeded, reshape or blocked", "unsupported", "Use a fixer response.")
	}
	if p.Repair == nil {
		add(d, "$.result.payload.bug.repair", "required", "repair summary", "absent", "Summarize the repair or the partial work.")
		return
	}
	if err := p.Repair.Validate(); err != nil {
		add(d, "$.result.payload.bug.repair.summary", "required", "nonblank summary", "blank", err.Error())
	}
}
