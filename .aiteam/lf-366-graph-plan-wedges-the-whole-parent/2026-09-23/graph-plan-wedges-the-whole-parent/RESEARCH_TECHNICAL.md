# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At revision 40fa12605a4c5bb5b366a6e2806add039c7cab40, graph Plan recovers parent-scoped pending/uncertain candidates before selecting a story. Retirement returns an unconditional recovery error; other operations stop admission unless applied. This permits one story's unresolved effect to prevent unrelated story selection.

Evidence:

- `internal/turn/plan_graph.go:86`
- `internal/turn/plan_graph.go:598`
- `internal/store/plan_story.go:177`

## f2

At the reviewed revision, child discovery loops over non-retired bound drafts and rejects changed identity, parent or non-open state. The worker invokes discovery before parent input processing and stops on its error. A manually closed child or remotely retired child with stale local state can therefore block parent work; the loop also returns after handling a child reply, so not every sibling necessarily stops before an earlier reply is handled.

Evidence:

- `internal/daemon/plan_children.go:38`
- `internal/daemon/plan_children.go:52`
- `internal/daemon/plan_children.go:112`
- `internal/daemon/tick.go:1174`

## f3

At the reviewed revision, retirement validates authenticated exact-candidate authority, persists the candidate and mutation intent, closes remotely, verifies closure and only then sets local retirement. Existing reconciliation accepts an exact retained intent with not-planned closure and matching revision after identity/provenance and parent checks. A crash or lost response between remote close and local persistence exposes the discovery obstruction.

Evidence:

- `internal/turn/plan_story_activity.go:201`
- `internal/turn/plan_story_activity.go:210`
- `internal/turn/plan_story_activity.go:289`
- `internal/turn/plan_story_activity.go:315`
- `internal/store/plan_story.go:146`

## f4

The permanent/store-edit-only characterization needs qualification. At the reviewed revision, admitReply runs before effect recovery, and the original retirement reply calls the retirement activity again. This can recover while discovery still sees an open child. Reopening an otherwise unchanged manually closed child removes the explicit discovery state mismatch. These are control-flow inferences, not live verified workarounds.

Evidence:

- `internal/turn/plan_graph.go:86`
- `internal/turn/plan_graph.go:209`
- `internal/turn/plan_graph.go:518`
- `internal/turn/plan_graph.go:585`
- `internal/daemon/plan_children.go:52`

## f5

At the reviewed revision, the failing admission checks precede workflow launch and ordinary preparation. Discovery errors call stopped/prepareFailed; comments remain unconsumed and polling retries on subsequent eligible attempts. Failure counting logs escalation rather than repairing state. This supports a persistent parent availability problem, not a daemon-wide halt or an unconditional assertion about every clock tick.

Evidence:

- `internal/turn/execute.go:631`
- `internal/turn/plan_graph.go:120`
- `internal/daemon/tick.go:1241`
- `internal/daemon/tick.go:1258`
- `internal/daemon/tick.go:1932`

## f6

At the reviewed revision, fresh Plan route admission selects graph, while persisted routes win. The documented contract requires independent story selection, exact owner-authorized retirement and no final readiness with unresolved effects. A fix should isolate story failure without relaxing those guarantees.

Evidence:

- `internal/store/plan_workflow.go:13`
- `docs/workflow/plan-graph.md:29`
- `.aiteam/APP_CONSTRAINTS.md:222`
- `internal/turn/plan_graph.go:190`

## f7

Existing tests provide reusable fixtures: direct activity retirement lost-response recovery, independent graph stories, confirmed retirement, and daemon child routing/restart cursors. The direct retirement test calls the activity again after restarting its fixture, bypassing daemon discovery and generic recovery admission. The reviewed test files do not exercise the combined stuck-retirement/healthy-story or manually closed-child parent scenarios. Tests were inspected, not executed; no numerical coverage claim is made.

Evidence:

- `internal/turn/plan_story_boundary_test.go:187`
- `internal/turn/plan_graph_boundary_test.go:434`
- `internal/turn/plan_graph_boundary_test.go:568`
- `internal/daemon/plan_children_boundary_test.go:17`
