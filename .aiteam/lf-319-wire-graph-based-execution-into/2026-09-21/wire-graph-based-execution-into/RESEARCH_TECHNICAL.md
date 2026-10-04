# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Research ships a bounded two-stage workflow. The factory controls agent invocation, document generation, publication and owner waits. Generic activities provide the reusable pattern. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/teamsource/research/research.dot:14`

## f2

Implement instructions assign frozen-story dispatch, dependency ordering, disjoint-file parallelism, immediate scoped tests and two fix/retest cycles to an agent. They retain final aggregate and save gates. This is instructional policy, not proof of a deterministic factory retry loop. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `teams/source/agents/shared/implement-orchestrator.md:12`

## f3

Persisted approved story revisions supply identities, bodies and dependencies. Engineering completion releases dependencies. All stories finishing can still leave aggregate/save recovery outstanding. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/turn/approved_context.go:18`

## f4

Existing tests check that engineering completion retains snapshot evidence and releases dependencies without closing stories; adjacent cases reject failed tests, missing outcomes and stale snapshots. These tests were inspected, not run. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/turn/engineering_progress_test.go:48`

## f5

Current graph outcome navigation still selects exactly one edge; Bug now has a dedicated durable test child, which is not general parallel story scheduling. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/graphexec/decide.go:7`
- `internal/turn/bug_test_graph.go:114`

## f6

SEC-21 and CONC-11 keep execution and aggregate/save gates in Implement and make Validate a read-only assessment of retained evidence. Moving test execution requires a coordinated contract change. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:207`

## f7

Aggregate retention checks captured snapshot bytes against the worktree, retains check provenance and requires an Implement binding. It runs no checks. This is reusable evidence infrastructure, not a factory test runner. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/turn/agent_result_aggregates.go:17`

## f8

Current checkout includes the shipped Plan graph with direct stage agents and registered Plan story activity, superseding prior uncertainty about repository presence; this does not claim an external #362 deployment observation. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/teamsource/plan/plan.dot:1`
- `internal/turn/plan_graph.go:293`

## f9

The action registry permits generic agent/control/publication mechanisms and mode-prefixed capabilities, while the activity interface separates declared work outcomes from inability to conduct or record work; these are extension seams, not existing Implement scheduling. Repository revision: 8f4514896e48bc8fcd5942e939f1e04a6d51aa9c; source inspection only.

Evidence:

- `internal/graphexec/registry.go:37`
- `internal/turn/graph_activity.go:21`

## f10

Graph validation requires exactly one edge per declared outcome and a positive visit limit on every cyclic node; its current attribute specifications use closed string values, so a numeric repair configuration requires explicit admission validation. Repository revision: 8f4514896e48bc8fcd5942e939f1e04a6d51aa9c; source inspection only.

Evidence:

- `internal/graphexec/validate.go:76`
- `internal/graphexec/validate.go:98`
- `internal/graphexec/validate.go:129`

## f11

Bug DOT directly dispatches its fixer within reserved attempts and routes to a dedicated check child; its test child currently uses factory checks for regression, focused and full phases. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/teamsource/bug/bug.dot:40`
- `internal/teamsource/bug/bug_tests.dot:9`

## f12

Bug check execution uses Codex sandbox containment and records command directory, exit, timing, output completeness and cleanup observations. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/bugchecks/executor.go:30`
- `internal/bugchecks/executor.go:60`

## f13

The current generic registry permits agent, condition, loop, comment and ask actions; generic check verification is proposed, not established by the inspected registry. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/graphexec/registry.go:46`

## f14

Codex usage collection includes input, cached-input and output token fields plus explicit missing-evidence diagnostics; no performance benchmark or reliable cross-harness command-proof validation was conducted in this pass. Repository revision: 6b16a5bc78835d6f68146187e06b77ddc4581ed9; source inspection only.

Evidence:

- `internal/codex/usage.go:39`
- `internal/codex/usage.go:82`

## f15

Implement is not a graph workflow: the registered workflow policies cover Research, Plan, Bug and Spike only, and Implement runs as one agent process whose orchestrator performs scheduling, dispatch and the repair loop by prompt text. Source inspection only.

Evidence:

- `internal/turn/workflow_registry.go:14`

## f16

Story and aggregate check evidence is agent-reported: capture checks are marked executed by construction and the retained provenance states the check was not independently observed. The factory verifies snapshot bytes against the worktree but not that any command ran. There is no test manager component; test engineers are roster roles dispatched by the orchestrator. Source inspection only.

Evidence:

- `internal/agent/contribution_capture.go:63`
- `internal/turn/submission_records.go:37`

## f17

Identified token and time costs, unmeasured: complete base64 file snapshots resubmitted per story and again at aggregate, the full story bundle and helper instructions resent on every launch, a serial story pipeline inside the orchestrator, repeated test runs at specialist, orchestrator and aggregate level, and report files nothing reads. Source inspection only; no usage data was read.

Evidence:

- `internal/harness/stories.go:10`
- `internal/turn/agent_result_aggregates.go:23`

## f18

Audit claims partly confirmed in this checkout: a successful run fails when any story of the feature generation lacks engineering completion (checked at the end-of-run harvest step), and a failed run skips the commit and leaves work uncommitted in the worktree. The exact error text for incomplete or blocked results being turned into failed runs was not found at the audited commit and is taken from the owner's audit. The 72 percent failure rate and run counts are the owner's audit figures, not re-measured. Source inspection only.

Evidence:

- `internal/turn/harvest.go:77`
- `internal/turn/harvest.go:457`

## f19

The workflow engine already supports a numeric per-node visit limit and requires a positive one on every node on a cycle, so a per-story repair limit can reuse it without a new counter; whether the limit can be configured per workflow as the owner wants is not yet confirmed. Source inspection only.

Evidence:

- `internal/graphexec/validate.go:110`
- `internal/graphexec/validate.go:124`
