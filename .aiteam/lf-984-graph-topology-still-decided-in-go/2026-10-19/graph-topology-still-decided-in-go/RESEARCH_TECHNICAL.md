# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f23

The Plan validator forces the shape effect, then an lf_if owner gate on the 'approved' condition, then an lf_ask of kind review on the false branch. A draft with no gate or review is refused, so a graph with no approval stop is not valid today.

Evidence:

- `internal/graphexec/plan_control.go:252`
- `internal/graphexec/plan_control.go:210`

## f24

A story advances only when Approvals[stage] equals the current review revision; the daemon offers the next stage through StoryNextStep and EligiblePlanStories, which both require every earlier stage to be approved and upstream stories approved. With no approval record a rush graph would stall, so Go must record an implicit approval when the graph has no approval node.

Evidence:

- `internal/planbrief/plan_workflow.go:290`
- `internal/planbrief/plan_workflow.go:349`

## f25

lf_ask is already a generic action allowed in every mode and the Plan validator already distinguishes lf_kind question and review on it; a new approval kind on lf_ask avoids extending the action registry, whereas a new lf_approval action does not match the registry rule of generic set plus lf_<mode>_*.

Evidence:

- `internal/graphexec/registry.go:47`
- `internal/graphexec/plan_control.go:278`

## f26

The owner command is built in Go as '/approve-plan <key> <stage> <candidate>' and parsed in the owner-decision code; the command list is fixed in Go. A DOT-declared command and parameter list would replace that text, with Plan's handler still writing the approval.

Evidence:

- `internal/planbrief/plan_workflow.go:494`
- `internal/turn/command.go:27`
- `internal/turn/owner_decisions.go:105`
