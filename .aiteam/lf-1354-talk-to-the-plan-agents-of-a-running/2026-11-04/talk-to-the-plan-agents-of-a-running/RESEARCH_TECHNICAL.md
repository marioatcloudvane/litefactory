# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-01

The issue background is stale at this commit. selectAdvisoryTarget dispatches Plan to selectPlanAdvisoryTarget. Parent unpromoted story sets map to plan-set; assembled parent review also maps to plan-set. Story issue membership selects story/technical/acceptance resources from the captured profile. No client-selected role parameter is supported.

Evidence:

- `internal/taskdiscussion/capture.go:135`

## F-02

Between story stages there is no fallback to a previous role: a story must have a named candidate and matching current wait stage, candidate and recomputed review revision. Retired stories, holds, pending effects and unsealed parallel groups are refused. A parent after set promotion and before assembled review is also refused. Parent eligibility is based on saved set/assembled state, not the same explicit story wait checks.

Evidence:

- `internal/taskdiscussion/capture.go:243`

## F-03

Plan judgment adds no content eligibility rule after target selection: eligibleAdvisoryJudgment returns true for plan-* roles. Research requires at least one finding, decision or assumption; Bug requires a saved cause. Thus both issue open questions already have implemented answers, although shaping can consider changing them.

Evidence:

- `internal/taskdiscussion/capture.go:334`

## F-04

Advisory execution runs the dedicated task-advisory role with captured stage guidance as evidence, not a resumed Plan graph agent. It uses a separate result binding and read-only judge transport, detached committed checkout and retained context. Its contract forbids normal stage-result and publication instructions. Advice cannot itself change a candidate, stage, approval or wait; changes still require the existing workflow paths.

Evidence:

- `internal/turn/task_advisory.go:52`

## F-05

discussion_open is not stage selection: it authorizes task advisory or separate publication consent and opens a conversation without calling Capture or launching a paid job. ask/judge perform capture. A client can therefore open a conversation even where no eligible saved analytical target is available. Summary publication remains a separate consented operation.

Evidence:

- `internal/taskdiscussion/service.go:81`

## F-06

Current snapshots require the latest graph instance to be running and resolve story membership to exactly one graph Plan parent. They load retained launch resources and Plan state from local storage. Snapshot guards and revision hashes bind advice to graph, waits, outputs, Plan revision and committed basis; later graph or shared-state changes can mark the old advice outdated, without moving the workflow.

Evidence:

- `internal/store/task_discussion_capture.go:43`

## F-07

Reuse/collision: #1274 (closed) already delivered private task conversations including Plan mapping in commit e02da58a57019910eeeab73d465d60be1a5dc578. #1297 explicitly excludes Plan-agent chat as #1354; its stories #1382–#1384 concern overview/prompts, brainstorming and task creation. Recent merged PRs #1223/#1224 add in-graph stage judges, a separate path from connector judge_task. Avoid equating connector advice with graph judge execution or Plan approval.

Evidence:

- `internal/turn/workflow_advisory.go:28`

## F-08

Impact: touched areas for any remaining extension are taskdiscussion capture/service/freshness, store snapshot guards, connector tool schemas/guidance, dedicated advisory role/skill and runner tests. Callers include hosted connector clients and daemon advisory dispatch; Research/Bug share selection/capture logic. Plan graph transitions and approval commands need no change for the behavior already present. No replacement or new migration is indicated by this bounded pass: existing discussion/advisory tables and captured team resources serve Plan too. Existing retained installations depend on captured role resources; missing or mismatched resources refuse rather than silently substitute roles.

Evidence:

- `internal/taskdiscussion/capture.go:30`

## F-09

Verification executed: go test ./internal/taskdiscussion ./internal/connector -run 'TestCapture|TestTaskAdvisoryPrivateFollowupJourney' -count=1 passed in both packages. Tests cover set, assembled, story, technical, acceptance target selection and stale story review refusal; the connector private-follow-up journey checks non-mutation of saved Plan workflow state. That connector test injects a capture fixture, so it is not proof of the full production store-to-provider route. No live connector/provider checks or deployed-version inspection were performed.

Evidence:

- `internal/taskdiscussion/capture_test.go:63`

## F-10

Issue-thread Plan discussions are a separate existing route: tests show answer-only replies preserve stage/wait, while change requests reopen current story review. This route must not be confused with private connector advice, which has no workflow-edit authority. Existing final parent approval and story review gates remain the mechanism for progressing Plan.

Evidence:

- `internal/turn/plan_graph_discussion_test.go:35`

## F-11

Shaping inspection at commit 45de2d5b265d082bec98f673ba58afe165b1d27d confirms the connector guide already advertises Plan set, assembled, story, technical and acceptance advice, task:advisory permission, isolated execution and non-mutation; current target selection still refuses mismatched story waits, holds and unsealed parallel groups, and Plan judgment adds no further content threshold.

Evidence:

- `docs/guides/remote-connector.md:741`
- `internal/taskdiscussion/capture.go:253`

## F-12

Proposed verification scope impacts advisory capture/store/runner and connector tests plus existing connector usage guidance; hosted clients and shared Research/Bug advisory paths depend on these boundaries. No replacement, data migration, new tool schema, new permission or captured-team rewrite is indicated by the inspected existing capability. The remaining evidence gap is production capture-to-provider integration and authentic hosted-client observation, not an established missing Plan selector.

Evidence:

- `internal/taskdiscussion/capture.go:253`
- `internal/connector/task_advisory_e2e_test.go:136`
- `docs/guides/remote-connector.md:768`
