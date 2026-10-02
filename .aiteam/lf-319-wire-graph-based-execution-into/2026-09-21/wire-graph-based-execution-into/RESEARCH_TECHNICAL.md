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
