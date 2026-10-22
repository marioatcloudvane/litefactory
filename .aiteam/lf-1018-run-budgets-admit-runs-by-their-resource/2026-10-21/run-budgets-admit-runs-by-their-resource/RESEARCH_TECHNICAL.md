# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The setting is currently a daemon-wide run count: default 3, validated from 1 through 16. pool.try takes one channel token and registers one worker; done returns one token. Weighted acquisition must be atomic and release the exact acquired cost while preserving shutdown joins and nonblocking admission.

Evidence:

- `internal/config/config.go:571`
- `internal/config/validate.go:31`
- `internal/daemon/pool.go:44`
- `internal/daemon/pool.go:106`

## f2

DOT parsing preserves arbitrary graph and node attributes and node defaults. Profile and Node expose attribute maps. An lf_slot_budget attribute can follow existing conventions, but requires scalar validation and actual admission consumption; preservation alone does not enforce it.

Evidence:

- `internal/graphexec/dot.go:31`
- `internal/graphexec/dot.go:122`
- `internal/graphexec/graphexec.go:11`
- `internal/graphexec/validate.go:125`

## f3

SemanticDigest retains lf_-prefixed attributes but omits an unprefixed slot_budgets attribute. Recommend lf_slot_budget="3" on worker_agent, optionally as a graph default for uniform workflows. Existing lf_budget="15" on Validate repair_agent denotes agent minutes, so reusing lf_budget would collide with another concept.

Evidence:

- `internal/graphexec/semantic.go:51`
- `internal/teamsource/implement/implement.dot:76`
- `internal/teamsource/validate/validate.dot:39`
- `internal/teamsource/validate/validate.dot:62`

## f4

Implement has coordinator and story-worker paths in the same graph. worker_agent is the code-writing agent; applying a graph-wide cost of 3 would also charge coordinator work unless overridden. Validate has costly suite and repair nodes plus parallel review work; slot policy must be generic across actions, not just lf_agent.

Evidence:

- `internal/teamsource/implement/implement.dot:43`
- `internal/teamsource/implement/implement.dot:76`
- `internal/teamsource/validate/validate.dot:54`
- `internal/daemon/pool.go:191`

## f5

Admission occurs in several paths: ordinary offers, graph dispatches, schedules, continuations and stopped-result recovery. runSlots lends the same pool to nested Validate reviewers. continueGraphSteps runs later stages on the same worker slot, so charging only new graph dispatches leaves a bypass. Nested acquisition can also compete with the parent reservation; shaping must define ownership and deferral without blocking all capacity.

Evidence:

- `internal/daemon/tick.go:1472`
- `internal/daemon/graph_dispatch.go:93`
- `internal/daemon/graph_dispatch.go:123`
- `internal/daemon/schedules.go:81`
- `internal/daemon/continuation_dispatch.go:80`
- `internal/daemon/stopped_results.go:89`
- `internal/daemon/pool.go:220`

## f6

Active capture APIs select embedded workflow packages rather than arbitrary owner paths. Implement launch persists definition and content pins and restores the admitted definition. Repository contains both embedded source graphs and versioned team copies; release/capture ownership must be respected when adding the configuration. Declarative configuration is feasible, but hot editing a team copy is not proven to change the running daemon.

Evidence:

- `internal/teamsource/implement.go:13`
- `internal/teamsource/research.go:54`
- `internal/turn/implement_graph.go:154`
- `internal/turn/implement_graph.go:190`
