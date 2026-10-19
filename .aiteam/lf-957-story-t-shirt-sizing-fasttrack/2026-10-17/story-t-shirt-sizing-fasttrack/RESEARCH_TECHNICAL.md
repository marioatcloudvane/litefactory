# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Plan control policy declares closed responses set, story, technical, acceptance, create, ask, answer, review-ready, approved; the defaults are story, technical, acceptance.

Evidence:

- `internal/graphexec/plan_control.go:41`

## f2

Step selection is per story: a story task gets its one next stage from StoryNextStep; the parent picks the next undrafted story with stage 'story'. This is strictly sequential.

Evidence:

- `internal/turn/plan_graph_pick.go:40`

## f3

Graph validation does not pin the node set or edges; stages are declared by the graph definition. A new sizing node is therefore possible without changing the validator, as long as the listed properties hold.

Evidence:

- `internal/graphexec/plan_control.go:48`

## f4

No code references t-shirt sizing or fast-track; only older research documents mention the terms.

Evidence:

- `.aiteam/lf-317-new-team-and-workflow-setup-for-graph/2026-10-07/new-team-and-workflow-setup-for-graph/RESEARCH_BRIEF.md`

## f5

Plan runtime binds a story task to the parent's plan state and runs one stage of its own story only.

Evidence:

- `internal/turn/plan_graph.go:246`

## f6

Plan is set up as one agent per stage (set, story, technical, acceptance), each with its own shaping skill. A fast-track agent can load these skills together.

Evidence:

- `teams/swift-ios/v1.0.1/plan/plan-story.md`
- `teams/swift-ios/v1.0.1/plan/plan-technical.md`
- `teams/swift-ios/v1.0.1/plan/skills/plan-acceptance-shaping/SKILL.md`

## f7

The Plan dot file has one block per stage (set, story, technical, acceptance); the factory picks the stage from stored state via select_* decisions on the response value, and the story stage runs a bounded pick cycle.

Evidence:

- `teams/swift-ios/v1.0.1/plan/plan.dot`

## f8

The technical agent already writes Architecture - General always, and Data Model, Service Design and Solution Design only where applicable.

Evidence:

- `teams/swift-ios/v1.0.1/plan/plan-technical.md`
- `teams/swift-ios/v1.0.1/plan/skills/plan-technical-shaping/SKILL.md`
