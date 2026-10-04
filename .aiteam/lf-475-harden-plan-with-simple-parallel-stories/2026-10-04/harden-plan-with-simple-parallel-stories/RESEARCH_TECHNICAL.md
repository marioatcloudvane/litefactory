# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Initial story creation is sequential: story_pick selects one promoted story without a draft, story_agent drafts it, story_review publishes, then story_pick runs again (limit 13).

Evidence:

- `teams/source/plan/plan.dot`

## f2

pickPlanStep returns exactly one story per call. NextSetStory gives first-draft work in topological order; EligiblePlanStories gives later-stage work.

Evidence:

- `internal/turn/plan_graph_pick.go:43`

## f3

EligiblePlanStories already skips waiting, held and dependency-blocked stories, so the independence rules exist to reuse.

Evidence:

- `internal/planbrief/plan_workflow.go:250`

## f4

The daemon has a worker pool bounded by max_concurrent_runs, which can bound parallel story runs.

Evidence:

- `internal/daemon/pool.go:26`

## f5

All story stages share one revision-checked Plan state, so parallel agents must merge into freshly read state to avoid conflicts.

Evidence:

- `internal/planbrief/plan_workflow.go`
