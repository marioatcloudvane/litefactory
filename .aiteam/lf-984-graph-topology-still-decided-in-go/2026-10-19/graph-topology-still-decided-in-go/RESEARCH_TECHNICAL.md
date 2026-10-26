# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Validate ship order is still a Go switch in shipStep; the DOT only loops back to resolve.

Evidence:

- `internal/turn/validate_resolve.go:346`

## f2

ValidateBugParent still requires ProfileVersion 2 and a digest equal to the shipped bug.dot or historical profile.

Evidence:

- `internal/teamsource/bug_parent.go:39`

## f3

Owner reply resume still uses COALESCE(w.origin_node, a.node) in SQL.

Evidence:

- `internal/store/graph_owner_waits.go:104`

## f4

#1006 partly fixed: only github.ErrRateLimited is treated as not sent (UnmarkReportDeliverySent). Other refusals stay possibly_sent and there is no general lifetime for uncertain deliveries; the only settlement is doctor-consume specific.

Evidence:

- `internal/turn/report_delivery.go:229`
- `internal/store/report_deliveries.go:113`

## f5

#1007 fixed for the reported shape: a reply bound to a finished Plan launch is refused as not applied and consumed, not rebound.

Evidence:

- `internal/turn/plan_graph.go:168`

## f6

#1008 fixed: a reserved unallocated dispatch is cancelled after MaxGraphDispatchAttempts=3 with an owner notice. 'No longer matches' errors are not covered by this cap.

Evidence:

- `internal/store/graph_dispatches.go:36`
- `internal/daemon/graph_dispatch.go:463`

## f7

ErrTicketIdentityConflict has about 130 raise sites; many guard foreign actor or ticket ownership. The #989 evidence points to the story comment sweep (retained comment and watermark checks) only. The path was inferred, not traced.

Evidence:

- `internal/store/session_tickets.go:16`
- `internal/store/ticket_comments.go:47`

## f8

Story refinement: locked stories now get an explicit reply (cause of open #1157); refinePlanChildren no longer re-routes promoted features. Whether the comment is consumed after the refusal, and whether a failed coordinator restarts, were not verified.

Evidence:

- `internal/daemon/story_refinements.go:186`
- `internal/daemon/plan_children.go:31`

## f9

Transient handling is partial: Plan treats ErrDiscoveryDeferred as retry and Implement merge retries deferrals and rate limits; StopOnFailure paths (Research, Bug, judge recovery) end the instance on one failed activity.

Evidence:

- `internal/turn/graph_node.go:287`
- `internal/turn/implement_merge.go:50`
- `internal/turn/research_workflow.go:277`

## f10

Doctor today: 11 typed repair kinds, durable holds with no expiry column, drain, held-session gate, per-step backup and journal. None of the #1009 additions exist (settle delivery, retire or rebind trigger, clear stale recovery, exact-row update, retry of transient step failures).

Evidence:

- `internal/agent/repair.go:19`
- `internal/store/doctor_repairs.go:71`
- `internal/doctor/repair.go:471`

## f11

A doctor hold ends only through a state change; a needs_review legacy repair or a process death can keep a session held. Only the first failure is logged for consume_comment refusals; other step refusals carry a generic reason.

Evidence:

- `internal/store/doctor_repair_journal.go:203`
- `internal/doctor/repair.go:540`

## f12

The doctor and recovery docs still say there is no general row editor and that a repair is attempted once, which conflicts with the #1009 direction.

Evidence:

- `docs/operations/doctor.md:66`
- `docs/operations/recovery.md:38`

## f13

Light guardrails C (validate repair reset, result envelope extras, plan ceilings, doctor verbatim gaps, conversation protocol) and E (issue creation quotas, 30/60 minute run limits) are still present in code and have not been changed.

Evidence:

- `internal/turn/validate_repair.go:258`
- `internal/agentresult/validate.go:62`
- `internal/graphexec/registry.go:116`

## f14

Open bugs still caused in code: #1160 hard error in ReviewRevision for a mid-review dependency; #1164 resolver loop ends only with an error log at the task queue.

Evidence:

- `internal/planbrief/plan_workflow.go:121`
- `internal/daemon/tick.go:3324`

## f15

Silent stops still exist: continuation for graph sessions is cancelled with only a store reason; owner-wait sessions skipped without a log; low-disk hold has no timeout.

Evidence:

- `internal/daemon/continuation_dispatch.go:34`
- `internal/daemon/graph_issue_closure.go:34`
- `internal/daemon/tick.go:3422`

## f16

Per-mode vocabulary items (ImplementAwaitNode, PlanFailureNode, MaxImplementAttempts, ValidateMaxRepairRounds) are still present by symbol; their wider uses were not re-read.

_No source reference was recorded for this finding._

## f17

One DOT file is one workflow. team.config maps six fixed keys (research, plan, implement, validate, spike, bug) to one path each, the file name is free, and a mixed-mode graph fails node/mode validation. The workflow itself is chosen by command, label or classifier, not by the graph.

Evidence:

- `internal/daemon/mode.go:132`
- `internal/graphexec/validate.go:126`
- `internal/agent/workflow.go:58`

## f18

Plan validation pins shape but no node set; the runtime fixes one stage per run, names agents <stage>_agent, answers the approval gate review_ready always, and advances stages only through /approve-plan. A chained three-stage plan with no approvals therefore cannot run through DOT edges alone.

Evidence:

- `internal/graphexec/plan_control.go:68`
- `internal/turn/plan_graph.go:701`
- `internal/turn/plan_graph_pick.go:38`

## f19

An all-lf_agent graph is valid in Research (and Spike without a final comment) only. Plan, Implement, Validate and Bug require their own start nodes and actions. Research publication needs a successful lf_research_brief. Research tool profile restricts Bash, and runs stop at 30 minutes by default.

Evidence:

- `internal/graphexec/research_control.go:132`
- `internal/turn/research_brief_activity.go:145`
- `internal/config/config.go:569`

## f20

Plan stage rights are Go: create is valid only at stage story, the story stage may change only intent, dependencies and conversation, the technical stage may not touch criteria or tasks, and the stage vocabulary is story/technical/acceptance. A one-pass plan needs a new full stage; prompts, roles, skills and bounds per stage are already DOT attributes.

Evidence:

- `internal/graphexec/plan_story.go:92`
- `internal/graphexec/plan_story.go:154`
- `internal/graphexec/plan_control.go:41`
- `internal/teamsource/plan/plan.dot:1`

## f21

The DOT already has a lf_field attribute on lf_if, with Go supplying the value (for example response), and Validate already has separate push, PR, merge and close nodes. Both are footholds for moving decisions into the graph: more published facts for lf_if, and generic effect nodes.

Evidence:

- `internal/teamsource/plan/plan.dot:28`
- `internal/graphexec/validate_flow.go:217`

## f22

Plan approval is not wired into agent activities. It comes from the validator-required shape (effect, owner gate, review ask), the gate always answered review_ready, and the approval fact written only by ApplyOwnerReply. The daemon offers a story's next stage only when earlier approvals are current. The final plan approval (/approve-plan-set) is a separate Go handoff and stays owner-only.

Evidence:

- `internal/turn/plan_graph.go:727`
- `internal/graphexec/plan_control.go:252`
- `internal/planbrief/plan_workflow.go:510`
- `internal/planbrief/plan_workflow.go:631`
