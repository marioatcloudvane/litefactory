# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Spike is defined as a fast lane: quick research or a small time-boxed change, with no orchestrator and no prerequisite. It must write SPIKE.md and must hand back to Research or Plan when the work grows. It does not say what problem Spike is meant to solve.

Evidence:

- `.claude/modes/spike.md:5`
- `.claude/modes/spike.md:39`

## f2

Spike has no graph. The shipped graphs are research, plan and bug. Spike runs as a single agent under the router, so the factory does not control its steps, limits or checkpoints.

Evidence:

- `internal/teamsource/research/research.dot:13`
- `internal/teamsource/bug/bug.dot:13`
- `internal/teamsource/research.go:108`
- `internal/teamsource/plan.go:27`

## f3

Spike is a first-class mode and workflow identity in code (ModeSpike, WorkflowSpike, the /spike command) but borrows the Implement tool profile on purpose, so it can write code without any review step.

Evidence:

- `internal/agent/workflow.go:14`
- `internal/daemon/tools.go:43`
- `internal/config/template.go:117`
- `internal/turn/command.go:11`

## f4

The Spike result format has request, work_done, findings, decisions and not_done. Questions are rejected in Spike, so a blocked Spike cannot wait for the owner; it can only record the blocker and hand the decision back in the final report.

Evidence:

- `internal/agentresult/types.go:170`
- `internal/agentresult/validate.go:151`
- `teams/source/skills/agent-finalization/references/spike.md:6`
- `.claude/modes/spike.md:32`

## f5

CLAUDE.md and AGENTS.md route Spike to a guide at docs/workflow/spike.md. That file does not exist in the repository; docs/workflow holds only Bug, Plan-graph and Research-graph documents.

Evidence:

- `CLAUDE.md:16`
- `AGENTS.md:18`

## f6

Eighteen archived SPIKE.md notes show three real uses: diagnose or measure (disk growth, startup hang, run timeout), a small fix with a failing-then-passing regression test (commit staging), and bundles of several fixes found along the way (the admit-mode validate gate had four stacked defects). The third use conflicts with the mode's own hand-back rule.

Evidence:

- `.aiteam/main/2026-09-05/claude-startup-hang/SPIKE.md:1`
- `.aiteam/main/2026-09-06/disk-growth/SPIKE.md:1`
- `.aiteam/lf-157-commit-staging-fails-when-the-repo/2026-09-08/commit-staging-fails-when-the-repo/SPIKE.md:1`
- `.aiteam/main/2026-09-10/admitmode-validate-gate/SPIKE.md:1`

## f7

The graph kit is reusable: shared agent, loop, if, ask and comment steps. Per the Research graph doc, a new role using an existing result contract and permissions needs only Markdown and a DOT file, with no new handler. Research graph execution is opt-in through a repo setting (legacy or graph).

Evidence:

- `docs/workflow/research-graph.md:1`
- `docs/workflow/research-graph.md:13`
- `internal/config/config.go:892`

## f8

Graph Research and Plan pair each stage with an owner checkpoint (ask or review, then park). Bug adds a test stage and a delivery review. A Spike graph would have no equivalent gate unless one is designed.

Evidence:

- `internal/teamsource/research/research.dot:45`
- `internal/teamsource/bug/bug.dot:1`

## f9

The if step reads only the response word of an agent result, and each mode declares its own closed word list. A 'small fix or hand back' verdict therefore cannot be routed from a payload field; it needs a gate activity or an extra response word.

Evidence:

- `internal/graphexec/research_control.go:39`

## f10

A mode-specific activity is admitted only if its name starts with the workflow name. Spike's own activities must be named for Spike; Bug's check runner cannot be registered under Spike.

Evidence:

- `internal/graphexec/registry.go:38`

## f11

The document generation step is Research-only and the shared node decoder refuses any mode but Research or Plan. Spike needs its own note activity and decoder variant; the envelope itself is reused.

Evidence:

- `internal/turn/workflow_policy.go:45`
- `internal/agentresult/research_node.go:75`

## f12

Bug already gives its two roles separate tool profiles, and Plan and Bug each supply a workflow policy object. This is the pattern Spike would follow for a read-only investigator and a write-capable fixer.

Evidence:

- `internal/agent/bug_permissions.go:8`
- `internal/turn/workflow_policy.go:45`
