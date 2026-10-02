# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

The global setting bounds daemon workers, but graph dispatch first acquires parent-session in-flight ownership. A second layer in Execute acquires a persisted session lock before preparation. Parallel story work cannot be enabled solely by raising the pool size or removing one guard.

Evidence:

- `internal/daemon/graph_dispatch.go:84` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/daemon/inflight.go:58` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/execute.go:582` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/config/config.go:555` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/config/validate.go:31` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-2

Plan picks one stage: initial drafts in topological order, then the first eligible story. The daemon chains at most 32 owed stages on the same worker. This confirms that back-to-back dispatch removes tick waits without overlapping agent stages.

Evidence:

- `internal/turn/plan_graph_pick.go:22` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/daemon/graph_dispatch.go:110` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-3

Plan state is one JSON document per parent. Saves compare the whole document revision, replace it, retain notices and append an immutable history snapshot in one transaction. Two disjoint story writers reading the same revision still conflict. The existing per-story draft records do not remove this shared-state update point.

Evidence:

- `internal/store/plan_workflow.go:79` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/store/plan_workflow.go:100` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph.go:183` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-4

Readiness and exact approval bindings already distinguish stories. Eligibility excludes waiting, held and inactive stories and requires prior stages' current approvals. Upstream references bind the selected candidate and transitive dependency candidates; shared revision and existing issue revision also bind submissions. Existing independence tests cover sequential independent progress and sibling preservation, not simultaneous agents.

Evidence:

- `internal/planbrief/plan_workflow.go:248` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/planbrief/plan_workflow.go:555` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph_binding.go:26` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph_independence_integration_test.go:13` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-5

Graph dispatch persistence allows one open dispatch per graph instance. Plan admission and mismatched-parking recovery use the parent session and latest Plan instance; recovery may cancel workflow graphs by session. These identities and cancellation boundaries require review before several story stages can remain in flight.

Evidence:

- `internal/store/migrations/0053_graph_dispatches.sql:1` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph.go:132` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph.go:156` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-6

Graph story issues are mapped to a parent rather than becoming standalone tasks. Existing child refinement execution uses parent locking and independent discovery/consumption machinery. Concurrent work must preserve child-to-parent routing, deduplication and per-story reporting rather than simply treating children as unrelated issues.

Evidence:

- `internal/store/plan_workflow.go:23` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/daemon/story_refinements.go:57` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-7

Preparation derives a worktree from the session issue number and stores it on the session; graph Plan finalization uses normal preparation and writes/saves the assembled Plan. The clone gate protects git critical sections, not a whole agent's workspace lifetime. Isolated story execution and coordinated parent finalization are therefore part of the affected surface.

Evidence:

- `internal/turn/prepare.go:1545` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/prepare.go:197` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/execute.go:71` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/turn/plan_graph_handoff.go:118` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-8

Project constraints require global worker bounds, durable reporting before consumption, exact candidate/upstream approval and recoverable effects. Current source admits new sessions to graph Plan, while APP_CONTEXT and CONC-24 still say it is held back. Treat this as a documentation/source discrepancy, not evidence of live deployment status.

Evidence:

- `.aiteam/APP_CONSTRAINTS.md:179` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `.aiteam/APP_CONSTRAINTS.md:222` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`
- `internal/store/plan_workflow.go:13` @ `2c35ff1d655f5f553abd1860b022f7a831b4765c`

## F-9

At the current worktree revision, initial story-set drafting checks whether dependency drafts exist, not whether their story text has owner approval; ordinary eligibility separately checks a story's own preceding-stage approvals, and review revisions bind dependency content rather than testing dependency approvals, making a new upstream approval gate a behavior change.

Evidence:

- `internal/planbrief/plan_set.go:70` @ `d119859a7888e8661239da45d805fab98cf94c62`
- `internal/planbrief/plan_workflow.go:97` @ `d119859a7888e8661239da45d805fab98cf94c62`
- `internal/planbrief/plan_workflow.go:248` @ `d119859a7888e8661239da45d805fab98cf94c62`

## F-10

The quoted refusal is generated when an approval does not match the current story wait, and the no-approval-pending suffix is used when no current command exists; dependency invalidation clears affected waits and approvals, while the child routing loop reads and binds comments per mapped child issue, so the supplied report alone does not establish cross-story misrouting or the live invalidation cause.

Evidence:

- `internal/planbrief/plan_notice.go:77` @ `cdc04491a2bed31313749adc8999afa27c3b6fdb`
- `internal/planbrief/plan_notice.go:102` @ `cdc04491a2bed31313749adc8999afa27c3b6fdb`
- `internal/planbrief/plan_workflow.go:177` @ `cdc04491a2bed31313749adc8999afa27c3b6fdb`
- `internal/daemon/plan_children.go:95` @ `cdc04491a2bed31313749adc8999afa27c3b6fdb`
