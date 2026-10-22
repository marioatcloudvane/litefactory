# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Creation constants are 30 minutes from authenticated comment creation, five artifacts per proposal, and 20 charged artifacts per repository. The confirmed-artifact window is rolling 24 hours, not a calendar day.

Evidence:

- `internal/store/mcp_creation.go:12`

## f2

Owner approval admission checks authenticated actor identity, immutable proposal hash, repository, relationship, current proposal and revision. It stores expiry from comment creation; publication rechecks expiry and persists refusals. Replaying an already elected publication returns its stored outcome.

Evidence:

- `internal/store/creation_owner_publication.go:38`

## f3

Quota counts historical agent operations plus factory owner publications. Unreleased/unconfirmed operations remain charged without a 24-hour age cutoff; only confirmed operations age out. Thus uncertain remote effects can keep capacity occupied indefinitely. The owner publication quota check is transactional and occurs before election.

Evidence:

- `internal/store/creation_owner_publication.go:87`

## f4

Claude and Codex both default to 1800 seconds. Both are already configurable and validated from 60 through 86400 seconds. Claude's comment explicitly explains the hard kill as protection against indefinite credential retry without a result event.

Evidence:

- `internal/config/config.go:358`

## f5

Optional lf_budget already supplies per-agent-node whole-minute limits from 1 through 60; absence means no graph budget. The ceiling guards against typos. It is an agent-invocation bound, not a timeout for every graph action.

Evidence:

- `internal/graphexec/registry.go:110`

## f6

Shared invocation nests the graph deadline and runner timeout; earliest deadline wins. Runner timeout also covers waits and relaunches for the whole invocation. Therefore raising lf_budget alone cannot extend a 30-minute runner. Stack setup also consumes invocation time.

Evidence:

- `internal/turn/research_agent.go:189`

## f7

Graph deadline branch records attempt timeout and process evidence, then returns before retained-result acceptance/recovery. The later process-failure path can accept retained submissions where policy permits; Research/Bug also retain failed submissions. Implement failure handling records a failed attempt and routes to the graph's failure edge. This is inconsistent recovery, not proof that every timeout deletes work.

Evidence:

- `internal/turn/research_agent.go:235`

## f8

Implement failure handling requires a process result and excludes shutdown/abandonment, records story failure and keeps process diagnostics. The source Implement graph has no lf_budget on its story worker; Validate repair explicitly has 15 minutes. Implement resume documentation states a resumed story keeps its branch and work.

Evidence:

- `internal/turn/implement_steps.go:322`

## f9

Git history introduces creation constants in 2e34ce99 (2026-09-14, task #272 MCP authorization work); node budgets in 5f7af59a (2026-10-06, task #497 bounded Validate repair). Later 4e1405ba removes agent creation grants while retaining owner publication and historical recovery. Original guardrails now serve a changed ownership model.

Evidence:

- `internal/store/mcp_creation.go:12`

## f10

Related issue #1018 is explicitly admission by resource cost and excludes changing admitted-run behavior. Issue #984 concerns DOT ownership of topology. These are adjacent concerns; longer timeouts do not resolve memory/swap pressure. Read via authenticated gh issue view; #728 is closed, but its summary does not verify the claimed two story-006 timeouts.

Evidence:

- `internal/teamsource/implement/implement.dot:63`
