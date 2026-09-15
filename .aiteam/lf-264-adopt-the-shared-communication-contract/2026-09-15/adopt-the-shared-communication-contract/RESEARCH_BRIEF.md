# #264 — Current communication-contract status

## Request and disposition

Research run 0001, 2026-09-15. Owner request: check the current prerequisites and get an overview; do nothing else yet. #274 and #296 are deliberately open. This bounded status investigation is complete. No implementation, Plan, issue creation, external comments, tests, deployment, commits or runtime-state inspection were performed. No owner decision is needed to finish this overview.

Repository evidence is from worktree revision `eb893565687d582bcb79d75edb14023881d0dd73`. GitHub issue states and comments were read on 2026-09-15 through the GitHub connector. Source inspection used shell read commands in the supplied macOS/zsh managed worktree; no provider binaries were probed. Test coverage below means existing test source or a specifically attributed historical report, not fresh execution. No source-byte/token-cost benchmark was performed; source bytes and billed tokens are not equated.

## Summary

The shared contract, MCP groundwork and tool families are present, and both native host integrations have reached this worktree. #264 is therefore no longer starting from disconnected infrastructure. However, tools remain additive to existing completion routes: adoption as the authoritative final-result path and retirement of the old inputs remain material work. Authentic host verification remains explicitly outstanding in #296.

## Current issue status

| Issue | State observed | Meaning for #264 |
| --- | --- | --- |
| [#258 — communication research](https://github.com/marioatcloudvane/litefactory/issues/258) | Closed | Original inventory and split into contract, adoption and MCP work. |
| [#263 — shared contract](https://github.com/marioatcloudvane/litefactory/issues/263) | Closed, September 13 | Contract implementation exists: strict five-mode results, revision-checked operations, storage/history/receipts, aggregate evidence and advisory progress. |
| [#265 — MCP research](https://github.com/marioatcloudvane/litefactory/issues/265) | Closed, September 13 | Research/decomposition predecessor; its closure alone is not implementation proof. |
| [#272 — MCP groundwork](https://github.com/marioatcloudvane/litefactory/issues/272) | Closed, September 14 | Transport, attempt authority, mode profiles and creation authorization groundwork exists. |
| [#273 — MCP tool families](https://github.com/marioatcloudvane/litefactory/issues/273) | Closed, September 14 | Shared-service tools and durable effect/recovery services exist; availability still depends on actual runtime registration. |
| [#274 — native integration and verification](https://github.com/marioatcloudvane/litefactory/issues/274) | Deliberately open | Host implementation is present. Its five engineering stories [#291](https://github.com/marioatcloudvane/litefactory/issues/291), [#292](https://github.com/marioatcloudvane/litefactory/issues/292), [#293](https://github.com/marioatcloudvane/litefactory/issues/293), [#294](https://github.com/marioatcloudvane/litefactory/issues/294), [#295](https://github.com/marioatcloudvane/litefactory/issues/295) are all now closed (September 15). |
| [#296 — authentic evidence](https://github.com/marioatcloudvane/litefactory/issues/296) | Deliberately open | Four retained real-host captures remain outstanding: Claude working, Codex working, Claude Validate and Codex Validate, with provenance and bounded replay. |
| [#264 — adoption and retirement](https://github.com/marioatcloudvane/litefactory/issues/264) | Open, Research | Production final-result cutover and compatibility retirement remain. |

Issue descriptions retain original dependency language and unchecked criteria even after closure; do not infer the current code state from those checkboxes alone. #272's [implementation report](https://github.com/marioatcloudvane/litefactory/issues/272#issuecomment-5661933718) explicitly placed #264 wiring afterward. #273's [implementation report](https://github.com/marioatcloudvane/litefactory/issues/273#issuecomment-5666119148) reports all eight stories and `make check` passing. These are historical reports, not checks rerun here.

#274's latest [run report](https://github.com/marioatcloudvane/litefactory/issues/274#issuecomment-5675803241) says Codex integration and all batched check groups passed, but its outer run remained failed with incomplete engineering evidence. The later closed states of #291–#295 and actual source presence are stronger evidence of current engineering status than that older failure summary. They do not prove authentic host acceptance. #296's [retained report](https://github.com/marioatcloudvane/litefactory/issues/296#issuecomment-5670459576) records the capture handoff and unavailable external observations.

## What is already wired, and what #264 still needs

All path:line references in this section refer to revision `eb893565687d582bcb79d75edb14023881d0dd73`.

| Area | Current source observation | Remaining adoption work |
| --- | --- | --- |
| Shared application service | `internal/turn/agent_results.go:92`, `:139`, `:198`, `:272` provide scoped reads, merged validation, submit/replay and receipts. `internal/agentresult/update_test.go:153` and `:186` cover complete final validation and stale/scoped updates. | Reuse these semantics across production result adapters and workflow finalization. Existing service tests do not establish end-to-end cutover. |
| Actual launches | `internal/daemon/setup.go:260` constructs runtime authority; `internal/turn/submission_runner.go:20` enters `runMCPBound`; `internal/turn/mcp_runtime.go:64` prepares registration, appends guidance and cleans up each invocation. Research/Spike can bind without managed stories; unmanaged/legacy Implement/Validate with no submission session keep their old runner route (`:79`). | Broader supported-mode adoption must account explicitly for these bypasses and completion policy. |
| Native providers | Claude generates attempt-local configuration (`internal/harness/mcp.go:28`, runner `:529`) and retains strict configuration. Codex supplies overrides (`internal/codex/mcp.go:42`, runner `:414`) with Validate shell/delegation restrictions. | Authentic behavior proof remains #296. Do not widen Validate to obtain final results. |
| Final outcomes | `runMCPBound` returns the runner result directly (`internal/turn/mcp_runtime.go:95`). Validate still consumes terminal contributions (`internal/turn/submission_runner.go:78`); Plan guidance retains the typed helper publication path (`internal/turn/mcp_guidance.go:74`). | Shared mandatory final submission, result-to-report/workflow integration and actionable rejection/missing-output rules across success, failure, interruption and cancellation are not established by the additive binding. |
| Prior results | Scoped service reads exist. Current application guidance supplies only the existing comment and expected revision (`internal/turn/mcp_guidance.go:136`); checkpoint modes take a separate branch (`:81`). | Supply sufficient authoritative prior state and stable IDs for actual edits across all applicable modes; a comment baseline is not the whole result. |
| Progress | Runtime binds a bounded lease and installs `activity_update` (`internal/turn/mcp_runtime.go:170`, `:186`). `internal/submission/family_tools.go:30` excludes Plan/Spike from family tools. Activity lifecycle API exists (`internal/turn/activity.go:79`). | Spike's requested advisory progress is not currently exposed. Runtime preparation does not retain the activity owner for terminal/retention lifecycle handling. Complete the factory-owned lifecycle without allowing activity to grant completion. |
| Catalog versus production availability | Current preparation registers feedback/evidence/receipts, activity update and optional issue reads (`internal/turn/mcp_runtime.go:148`). | A tool-family implementation is not proof it is enabled: this preparation does not install issue comment/creation or activity-get handlers. Recovery/control-intake seams should be reconciled with #264's existing scope, without turning this issue into a new MCP-server project. |
| Instructions and documentation | Runtime guidance exists and preserves separate application/checkpoint contracts (`internal/turn/mcp_guidance.go:45`). Canonical finalization skill and examples exist under `teams/source/skills/agent-finalization/`. | Update canonical and rendered instructions with actual adoption. `docs/agent-result-contract.md:3` still describes unchanged/offline routing; `docs/agent-evidence-capture.md:302` still describes Codex as deferred in run 0005. These historical statements need context when used for current status. |

Existing focused coverage includes cold/resume/fallback, cancellation isolation and stale registration (`internal/turn/mcp_runtime_lifecycle_test.go:99`, `:139`, `:224`); Codex enabled/disabled setup, collisions and launch paths (`internal/turn/mcp_codex_lifecycle_test.go:256`, `:429`, `:484`); and live/terminal Validate reconciliation (`internal/turn/submission_reconciliation_test.go:16`). They are useful reuse points, not fresh evidence in this run. Auxiliary classifier/scheduler/conflict conventions were not deeply audited in this bounded overview; their explicitly limited mapping remains an item in #264.

## Retirement overview

These are separate dispositions, not one interchangeable old path:

| Input | Current evidence and disposition |
| --- | --- |
| Plan v1 file handoff | Still present: `internal/turn/plan_handoff.go:137` reads/validates the attempt outcome. The same owner has v2 internal transport checks (`:148`), so deleting the whole handoff owner would remove live shared behavior. Inventory/version-gate the legacy ingress separately. |
| Implement event-file ingress | Still present: `internal/turn/approved_context.go:68` supplies an event directory; `internal/turn/story_progress.go:232` reads it. Retire ingress only after supported legacy scopes/effects are drained; preserve shared records, recovery and delivery. |
| Validate v1 parser | Still present: `internal/turn/validation.go:91` falls back to `agent.ParseValidationOutput` when typed terminal validation has not already handled the run. Preserve the restricted terminal path while replacing compatibility parsing. |
| Markdown story discovery | Present but explicitly production-unreachable: `internal/turn/subissues.go:117` explains the always-taken production `PlanForge` gate and shared live `PublishPlan`/`RenderCompleteStory` path. Candidate for removal with only exclusive wiring/helpers. |

Supported content/submission upgrade rules require a safe planning cohort and refuse active/frozen/mixed cohorts or pending legacy effects; completed legacy dependencies and old workflow versions remain readable (`docs/story-contract.md:152`). Thus upgrading content/submission does not by itself retire workflow-v1 combined verification. No live database or pending-effect inventory was inspected, so this run cannot establish that compatibility readers are safe to delete.

## Recommendation, risks and decisions

The next useful work, when requested, is to reconcile #264 around the infrastructure now present, keeping **adoption** and **verified retirement** as separate milestones. Preserve #274/#296 as the intentionally open authentic-evidence track. No new issues, scope revision or mode transition was made here.

Settled: status overview only; both host implementations are present; #274/#296 stay open; existing infrastructure should be reused. Open for a later planning/retirement investigation: exact supported legacy cohort and effect drain, final-result policy across failure paths, full baseline delivery, and ownership of remaining activity/recovery/control connections. These do not block this status overview and are not questions being asked of the owner now.

Material limits: closed issues and source presence do not establish deployment, acceptance receipts or real-host enforcement. No live daemon/database inspection, tests or authentic captures were performed. Earlier comments/docs may describe superseded states. There is no claim that #264 is complete or that legacy data can already be removed.
