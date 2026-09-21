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

The parser rejects DOT subgraphs. Outcome navigation in internal/graphexec/decide.go chooses exactly one next target. A story scheduler, parallel fork/join or callable story workflow therefore requires additional behavior. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/graphexec/dot.go:27`

## f6

SEC-21 and CONC-11 keep execution and aggregate/save gates in Implement and make Validate a read-only assessment of retained evidence. Moving test execution requires a coordinated contract change. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:207`

## f7

Aggregate retention checks captured snapshot bytes against the worktree, retains check provenance and requires an Implement binding. It runs no checks. This is reusable evidence infrastructure, not a factory test runner. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/turn/agent_result_aggregates.go:17`

## f8

Plan #362 remains unverified. Both repository issue requests were rejected (receipts 29f15c67feddc25a3c96a4e86ac90e84 and 6a876fd98e22de8f2320a4ef83fe895e); the GitHub web fetch failed. HEAD deletes internal/teamsource/plan. Its parent fbeb5c5 contains a staged Plan reference graph, but the cited source comment explicitly describes it as unwired with unregistered actions. Historical reference is not delivered Plan behavior. Repository revision: 21f4d1771133983c4fd97e68bfd683611c190331; source inspection only.

Evidence:

- `internal/graphexec/purity_test.go:51`

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
