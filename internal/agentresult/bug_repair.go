package agentresult

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
)

func validateBugRepair(r Result, ctx Context, d *Diagnostics) {
	p := r.Payload.Bug
	b := ctx.Bug
	if ctx.Workflow != agent.WorkflowBug || ctx.Mode != "" || b == nil || b.Role != "bug-fixer" || b.Reservation == "" || b.ApprovedRevision <= 0 || b.ApprovedDigest == "" || b.ApprovalReview == "" {
		add(d, "$.result.payload.bug", "scope", "factory-bound authorized fixer", "missing binding", "Use the exact factory repair reservation.")
		return
	}
	if p.Invocation != b.Invocation || p.BaseRevision != b.BaseRevision || p.Analysis != nil || p.Approval != nil || r.Recommendation != nil || len(r.Questions) != 0 {
		add(d, "$.result.payload.bug", "binding", "unchanged authorized repair context", "mismatch", "Do not change approval, analysis or invocation bindings.")
	}
	switch p.Response {
	case "done":
		if r.Outcome != "completed" {
			add(d, "$.result.outcome", "consistency", "completed", "mismatch", "Done means authored work only.")
		}
	case "reshape":
		if r.Outcome != "incomplete" || r.Reason == "" {
			add(d, "$.result.outcome", "consistency", "incomplete with reason", "mismatch", "Retain why the remedy must change.")
		}
	case "blocked":
		if r.Outcome != "blocked" || r.Reason == "" {
			add(d, "$.result.outcome", "consistency", "blocked with reason", "mismatch", "Retain the obstacle.")
		}
	default:
		add(d, "$.result.payload.bug.response", "enum", "done, reshape or blocked", "unsupported", "Use a fixer response.")
	}
	if p.Repair == nil {
		add(d, "$.result.payload.bug.repair", "required", "repair evidence", "absent", "Retain authored or partial work.")
		return
	}
	e := p.Repair
	if e.Reservation != b.Reservation || e.AnalysisDigest != b.ApprovedDigest || e.SourceCommit != b.SourceCommit || e.SourceTree != b.SourceTree {
		add(d, "$.result.payload.bug.repair", "stale", "exact source and reservation", "mismatch", "Use the factory binding.")
	}
	var criteria []string
	if err := json.Unmarshal([]byte(b.CriteriaJSON), &criteria); err != nil || len(criteria) == 0 {
		add(d, "$.result.payload.bug.repair", "scope", "factory criteria", "invalid", "Factory criterion context is unavailable.")
		return
	}
	if err := e.Validate(criteria, p.Response == "done"); err != nil {
		add(d, "$.result.payload.bug.repair", "content", "complete bounded evidence", "invalid", err.Error())
	}
}
