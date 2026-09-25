# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Item 1 open: allocation binds a graph dispatch to its run only when the run is not prelaunch, and graph runs are always allocated prelaunch.

Evidence:

- `internal/store/runs.go:327`
- `internal/turn/prepare.go:1589`

## f2

Item 1 open: the prelaunch-to-running transition binds only automatic continuations. It has no graph branch, so the dispatch never gets a run id.

Evidence:

- `internal/store/prelaunch.go:19`

## f3

Item 1: the AllocatedRunID guard in RequeueGraphDispatch is therefore dead code. The daemon protects itself by looking up the run by trigger and checking for prelaunch instead.

Evidence:

- `internal/store/graph_dispatches.go:169`
- `internal/daemon/graph_dispatch.go:113`

## f4

Item 2 open: a dispatch is cancelled by the closed-issue path, by the retry cap in requeue, and by the cancel call. Each clears the instance due time. No code re-parks an instance with no open dispatch and no owner wait, and there is no operator command (cmd has only litefactory, team-benchmark, team-publish).

Evidence:

- `internal/daemon/graph_dispatch.go:59`
- `internal/store/graph_dispatches.go:177`
- `internal/store/graph_dispatches.go:236`

## f5

Item 3 open: the DOT loops exit as soon as the response is not ask, and the stage response vocabulary has no 'invoke again' value (ask, done, approved). New since the issue: ParkGraphInstanceAfter chains different stages one open dispatch at a time. It does not repeat a stage.

Evidence:

- `internal/teamsource/research/research.dot:18`
- `internal/agentresult/research_node.go:78`
- `internal/store/graph_dispatches.go:72`

## f6

Item 4 open: Plan is offered only RESEARCH_BRIEF.md by the brief-candidate list. RESEARCH_TECHNICAL.md is unreachable by omission.

Evidence:

- `internal/turn/session_artifacts.go:58`

## f7

Item 4 open: the requirements-engineer role still says to pre-fill intent, stories and constraints from RESEARCH_BRIEF.md. The generated brief has only Request, Decisions, Assumptions, Open questions and Recommendation.

Evidence:

- `teams/source/agents/shared/requirements-engineer.md:12`
- `.claude/agents/requirements-engineer.md:83`
- `internal/turn/research_briefs.go:122`

## f8

Item 4 context: graph Plan roles do not name either research file. The Plan set role reads 'saved research scope'. Graph Plan is still held back for new sessions, so the legacy role text is the live one.

Evidence:

- `internal/teamsource/plan/plan-set.md:28`

## f9

Item 5 open: the doc still says opt-in and default activation disabled, and requires authorized observations on the exact shipped snapshot and CLI versions for both hosts. This run is itself a Claude graph node run, and commits fixed Claude role naming and tool inheritance. That is new evidence but not the authorized observation the gate requires.

Evidence:

- `docs/workflow/research-graph.md:3`
- `docs/workflow/research-graph.md:68`

## f10

Item 6 open: the doc states a wait has no expiry. The owner wait store has no expiry or reminder field or logic.

Evidence:

- `docs/workflow/research-graph.md:40`
- `internal/store/graph_owner_waits.go:13`
