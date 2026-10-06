# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

An honest incomplete or blocked application outcome is turned into an error for every mode except plan and research, so Implement runs fail.

Evidence:

- `internal/turn/result_finalization.go:352`

## f2

After the agent exits, a run in the separated workflow is failed when any story of the feature lacks engineering evidence, even stories the run never touched. The same step also re-checks scope drift after exit.

Evidence:

- `internal/turn/harvest.go:70`

## f3

Plan is a graph with stage selection, one agent role per stage, factory activities, per-node lf_limit visit limits and durable ask nodes. It is the template for an Implement graph.

Evidence:

- `internal/teamsource/plan/plan.dot:16`

## f4

Plan's effect activity lf_plan_story is a registered action with declared outcomes. Implement needs analogous activities: next-ready story, serial merge, handoff report. None exist.

Evidence:

- `internal/graphexec/plan_story.go:16`

## f5

A go-unit-test-engineer role already writes and runs Go unit tests per story and reports files and results; acceptance and review are explicitly left to read-only Validate. This matches the owner's rule that tests are agent work and the full suite is Validate's.

Evidence:

- `.claude/agents/go-unit-test-engineer.md:1`
