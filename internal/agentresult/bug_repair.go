package agentresult

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugrepair"
)

func validateBugRepair(r Result, ctx Context, d *Diagnostics) {
	p := r.Payload.Bug
	b := ctx.Bug
	if b != nil && b.GraphInstanceID > 0 {
		validateGraphBugRepair(r, ctx, d)
		return
	}
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
	if e.Validation != nil {
		add(d, "$.result.payload.bug.repair.validation", "forbidden", "legacy authored evidence only", "validation generation", "Final validation belongs only to the admitted simplified Bug graph.")
	}
	if e.Reservation != b.Reservation || e.AnalysisDigest != b.ApprovedDigest || e.SourceCommit != b.SourceCommit || e.SourceTree != b.SourceTree {
		add(d, "$.result.payload.bug.repair", "stale", "exact source and reservation", "mismatch", "Use the factory binding.")
	}
	var criteria []string
	if err := json.Unmarshal([]byte(b.CriteriaJSON), &criteria); err != nil || len(criteria) == 0 {
		add(d, "$.result.payload.bug.repair", "scope", "factory criteria", "invalid", "Factory criterion context is unavailable.")
		return
	}
	// Notices are policy-class observations the authoritative store write
	// records as facts (internal/store/bug_repairs.go); this consistency pass
	// only judges whether the submission decodes, so only a hard fault is
	// surfaced here.
	if _, err := e.Validate(criteria, p.Response == "done"); err != nil {
		add(d, "$.result.payload.bug.repair", "content", "complete bounded evidence", "invalid", err.Error())
	}
}

func validateGraphBugRepair(r Result, ctx Context, d *Diagnostics) {
	p, b := r.Payload.Bug, ctx.Bug
	refuse := func(message string) {
		add(d, "$.result.payload.bug.repair", "validation", "current approved scope and complete final evidence", "invalid", message)
	}
	if ctx.Workflow != agent.WorkflowBug || ctx.Mode != "" || b.Role != "bug-fixer" || b.ApprovedRevision < 1 || b.ApprovedDigest == "" || b.ApprovalReview == "" || b.Reservation == "" {
		refuse("Missing factory-approved graph repair binding.")
		return
	}
	if p.Invocation != b.Invocation || p.BaseRevision != b.BaseRevision || p.Analysis != nil || p.Approval != nil || r.Recommendation != nil || len(r.Questions) != 0 {
		refuse("Do not change the invocation or approval binding.")
	}
	switch p.Response {
	case "succeeded":
		if r.Outcome != "completed" {
			refuse("Succeeded requires completed outcome.")
		}
	case "reshape":
		if r.Outcome != "incomplete" || r.Reason == "" {
			refuse("Reshape requires incomplete outcome and reasons.")
		}
	case "blocked":
		if r.Outcome != "blocked" || r.Reason == "" {
			refuse("Blocked requires the obstacle.")
		}
	default:
		refuse("Use succeeded, reshape or blocked; authored done is not validation success.")
	}
	if p.Repair == nil {
		refuse("Retain final or partial work evidence.")
		return
	}
	e := p.Repair
	if e.Reservation != b.Reservation || e.AnalysisDigest != b.ApprovedDigest || e.SourceCommit != b.SourceCommit || e.SourceTree != b.SourceTree {
		refuse("Repair differs from the current immutable factory binding.")
	}
	var criteria []string
	if err := json.Unmarshal([]byte(b.CriteriaJSON), &criteria); err != nil || len(criteria) == 0 {
		refuse("Factory verification conditions unavailable.")
		return
	}
	if _, err := e.Validate(criteria, p.Response == "succeeded"); err != nil {
		refuse(err.Error())
	}
	if p.Response != "succeeded" {
		return
	}
	var commands []bugrepair.ValidationRequirement
	if err := json.Unmarshal([]byte(b.ValidationCommandsJSON), &commands); err != nil {
		refuse("Factory validation commands unavailable.")
		return
	}
	if e.Validation == nil {
		refuse("Succeeded requires executed final validation.")
		return
	}
	if err := e.Validation.Validate(commands, criteria); err != nil {
		refuse(err.Error())
	}
	if e.Validation.InputsDigest != b.ValidationInputsDigest || e.Validation.TargetMain != b.TargetMain {
		refuse("Validation inputs or target main differ from factory observations.")
	}
}
