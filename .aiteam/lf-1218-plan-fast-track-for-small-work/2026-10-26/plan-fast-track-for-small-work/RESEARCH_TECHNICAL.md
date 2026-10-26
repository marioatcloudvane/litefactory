# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

Today Plan has set, story, technical and acceptance stages. The shipped source graph routes all three story-stage applied drafts to lf_gate owner review; it does not declare a size selector. The set still uses its existing approval check/review. The docs state each run enters one selected stage, so fewer stops need not mean fewer agent stages or one combined run.

Evidence:

- `teams/source/base-team/plan/plan.dot:26`
- `docs/workflow/plan-graph.md:9`

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

#1217 remains OPEN and explicitly owns per-story S/M/L, reason, visibility in set/final review and planning depth. Current PlanStorySetEntry has only id, name, intent and dependencies. The final sizing contract is therefore an integration dependency, not something this research can claim already exists.

Evidence:

- `internal/planbrief/plan_set.go:23`
- `.aiteam/lf-1218-plan-fast-track-for-small-work/2026-10-26/plan-fast-track-for-small-work/dependency-1217.json`

## F-05

Outcome-ledger guidance already exists: S-01 sets depth; each story has Covers; narrowing requires Ruling; final acceptance review exposes uncovered outcomes, rulings and outcome-to-story mapping. Fast-track guidance must preserve this existing contract.

Evidence:

- `teams/source/base-team/plan/skills/plan-evidence/SKILL.md:39`
- `teams/source/base-team/plan/plan-story.md:49`
- `teams/source/base-team/plan/plan-acceptance.md:54`

## F-06

Impact — touched areas are base Plan graph/roles/skills, Plan control validation, turn scheduling/approval state, size/result integration from #1217, final reporting, workflow docs and their contract tests. Direct consumers include parent set planning, per-story continuation, assembled approval and all four inheriting teams. Existing focused validator and production approval tests provide starting coverage; no tests were executed during this read-only pass.

Evidence:

- `internal/graphexec/plan_control.go:48`
- `internal/turn/plan_graph.go:31`
- `internal/turn/plan_approve_test.go:138`
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
