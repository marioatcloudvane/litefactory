# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

At shaping recheck on commit 64760c77fea50a1e43463ef63c52f371ff7aefeb, Plan still selects one of set/story/technical/acceptance and sends all three story-stage drafts to owner gates, now with advisory judges before reviews; the previous sequential-stage observation remains valid and no S-story parallel path is declared in this graph.

Evidence:

- `teams/source/base-team/plan/plan.dot:29`
- `teams/source/base-team/plan/plan.dot:51`
- `teams/source/base-team/plan/plan.dot:64`

## F-02

#984 is implemented in this checkout, not merely proposed. lf_plan_approve has approved/review/superseded/blocked/failed outcomes. The validator allows it after non-set drafts with a same-source owner review fallback. It forbids graph approval of the set and allows only owner approval paths to promotion. Current Plan control conditions are a closed list with no size condition.

Evidence:

- `internal/graphexec/plan_approve.go:5`
- `internal/graphexec/plan_control.go:40`
- `internal/graphexec/plan_control.go:330`

## F-03

Production graph approval runs under the parent progress lock and checks/saves against fresh retained state. Retirements always return review. Existing guards cover exact applied draft identity, prior/upstream approvals, holds and open waits/questions. Approval provenance is retained and final review lists graph-approved stages. Reuse these protections rather than equating a completed agent result with owner approval.

Evidence:

- `internal/turn/plan_approve.go:13`
- `internal/turn/plan_approve_test.go:138`
- `internal/turn/plan_graph_handoff.go:120`
- `docs/workflow/plan-graph.md:22`

## F-04

The earlier finding that per-story size fields were absent is superseded by the current checkout: PlanStorySetEntry now carries size and size_reason, retained sets carry a sizing policy version, and missing or invalid sizing is reported as advisory while legacy sets remain exempt; the feature can consume this existing contract without owning sizing or requiring historical migration.

Evidence:

- `internal/planbrief/plan_set.go:25`
- `internal/planbrief/plan_set.go:200`
- `internal/planbrief/plan_set_sizing_test.go:70`
- `.aiteam/lf-1218-plan-fast-track-for-small-work/2026-10-26/plan-fast-track-for-small-work/dependency-1217.json`

## F-05

Outcome-ledger guidance already exists: S-01 sets depth; each story has Covers; narrowing requires Ruling; final acceptance review exposes uncovered outcomes, rulings and outcome-to-story mapping. Fast-track guidance must preserve this existing contract.

Evidence:

- `teams/source/base-team/plan/skills/plan-evidence/SKILL.md:39`
- `teams/source/base-team/plan/plan-story.md:49`
- `teams/source/base-team/plan/plan-acceptance.md:54`

## F-06

Impact, expanded by owner parallelism: base Plan graph, three role prompts and skills, graph validation, turn scheduling, concurrent contribution retention and coherent assembly, per-story routing/fallback state, sizing integration, final reporting, workflow docs and contract tests; parent set review, story continuations, assembled approval and four inheriting teams consume it, while Implement/Validate keep consuming the existing approved plan format.

Evidence:

- `teams/source/base-team/plan/plan.dot:29`
- `internal/graphexec/plan_control.go:48`
- `internal/turn/plan_graph.go:31`
- `internal/planbrief/plan_set.go:25`
- `internal/teamsource/plan_contract_test.go`
- `teams/README.md:25`

## F-07

Compatibility — canonical base source feeds four generated 1.0.0 teams, which must not be edited directly. Release/linking is owner-controlled; saved captures replay retained resources. Legacy saved Plan discussions keep their old behavior. No migration is established by this request. A saved fast-track/fallback decision or new size fields would need compatibility treatment during shaping.

Evidence:

- `teams/README.md:3`
- `teams/README.md:78`
- `docs/workflow/plan-graph.md:58`

## F-08

Superseded/context — #957 is closed and split into #1217 sizing and #1218 fast track. Retained #984 research explicitly excluded shipping a rush graph, while settling generic gates and owner-only set/final promotion. Current main includes #984 via merge #1215 and generated-team refresh #1216 at checkout 6a54c1dffff3c23bb7606b9a2a2f57cd341cd86f. This request fills the previously deferred routing policy; it does not replace the gate system. Related issue/PR search and retained #984 brief revealed no existing shipped size-based path.

Evidence:

- `.aiteam/lf-1218-plan-fast-track-for-small-work/2026-10-26/plan-fast-track-for-small-work/predecessor-957.json`
- `.aiteam/lf-1218-plan-fast-track-for-small-work/2026-10-26/plan-fast-track-for-small-work/recent-history.txt`
- `.aiteam/lf-1218-plan-fast-track-for-small-work/2026-10-26/plan-fast-track-for-small-work/recent-history.txt`
- `.aiteam/lf-984-graph-topology-still-decided-in-go/2026-10-19/graph-topology-still-decided-in-go/RESEARCH_BRIEF.md:9`

## F-09

Current role contracts are sequential: the story role produces intent, the technical role consumes intent and owns architecture, and acceptance consumes both and owns tasks/criteria; current graph approval also requires current prior-stage approvals and upstream story approvals, so parallel reasoning must be reconciled before preserving ordered guarded approval rather than treating concurrent drafts as independent approvals.

Evidence:

- `teams/source/base-team/plan/plan-story.md:10`
- `teams/source/base-team/plan/plan-acceptance.md:10`
- `internal/planbrief/plan_workflow.go:576`
- `internal/planbrief/plan_workflow.go:602`

## F-10

At commit 1e2183b36a1ba7403416dff6351118adcd1a7b36, graph-only delivery cannot satisfy the retained scope: PlanControlPolicy has no size condition; Decide accepts exactly one destination per node outcome; Plan validation admits no parallel fork/join action; production prepare refuses an agent node other than the factory-selected stage_agent; and control only resolves existing stage/create/ask/answer/approval responses. Existing guarded graph approval can be reused, but size routing and concurrent stage contributions require runtime changes rather than additional DOT edges alone.

Evidence:

- `internal/graphexec/plan_control.go:40`
- `internal/graphexec/plan_control.go:140`
- `internal/graphexec/decide.go:7`
- `internal/turn/plan_graph.go:634`
- `internal/turn/plan_graph.go:714`
