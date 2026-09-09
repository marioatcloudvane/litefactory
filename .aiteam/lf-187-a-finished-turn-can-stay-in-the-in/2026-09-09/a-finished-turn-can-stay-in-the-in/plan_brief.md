# Plan Brief — Explain polling worker activity
# Feature: a-finished-turn-can-stay-in-the-in
# Branch: lf/187-a-finished-turn-can-stay-in-the-in
# Date: 2026-09-09
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/187
# Citation base: ac78c27abd38a28977d5e139acec6c566df67f05
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions
- Preserve polling, admission, queue order/rotation, schedules, continuations and execution behavior. This change explains worker activity; the earlier in-flight leak diagnosis was withdrawn.
- Keep diagnostic metadata private to daemon and use the existing injected structured logger. Add no package, exported API, schema, dependency, configuration or background goroutine.
- Match dispatch and terminal context using existing `repo`, canonical `task`, `session_id`, `dispatch_sources`, applicable `schedule_id`/`continuation_id`, and ordinary tick grouping time. Do not invent a globally unique worker ID; tick time groups events only.
- Source categories have canonical order `forge_discovery`, `managed_registry`, `backlog`, `due_schedule`, `automatic_continuation`. They describe enrollment, not actionable work. Mutable collections are copied before sharing with workers.
- Preserve legacy logging and exported counter contracts. Worker handoffs, executor requests and prepared runs remain distinct observations; zero execution requests does not prove no useful work.
- Full story contracts and exact-AC evidence inventories are transported in the version-1 proposal and become factory-owned GitHub stories. Implementation uses only factory-supplied frozen revisions. Verify and publish each story's acceptance/review/test evidence promptly; final aggregate gates remain additional.

## Workflow
- Ship US-1 (dispatch attribution, M), then US-2 (terminal outcomes, M), then US-3 (summary clarity and composed polling verification, M).
- Dependency graph: US-1 → US-2; US-1 and US-2 → US-3. Sequential work avoids competing edits to task/admission context; terminal diagnostics consume that context, and composed regression uses both event boundaries.
- Stories are independently verifiable once their dependencies pass. This run ends at Plan; implementation requires its own factory run.

## Cross-Story Constraints
- Preserve daemon clock isolation: the injected scheduling clock is read only in Tick. Diagnostic elapsed measurement cannot influence scheduling.
- Retain bounded worker admission, context propagation, store settlement and shutdown join semantics. No lock-based reconciliation or slot-release test observable.
- Keep existing error reporting and security boundaries. No raw SQL outside store, forge authority in agents, permission changes, fallback agents or recovery wrappers.
- Use existing offline HTTP/SQLite fixtures and bounded channel/Quiesce synchronization. No sleeps, integration tags or live nested-agent probes. Exact AC names identify evidence; no criterion is satisfied merely because its fixture is planned.
- Maintain format, vet, pinned lint and race-test requirements through `make check`. Do not build into dist, change runtime state or restart the daemon.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Attribute daemon worker dispatches to sessions and discovery sources | [#192](https://github.com/marioatcloudvane/litefactory/issues/192) |
| US-2 | Report terminal worker outcomes and execution requests | [#193](https://github.com/marioatcloudvane/litefactory/issues/193) |
| US-3 | Clarify polling counters and verify quiet managed-parent polling | [#194](https://github.com/marioatcloudvane/litefactory/issues/194) |

## Publication Handoff
- Generation 1 targets `github:marioatcloudvane/litefactory#187`. No existing story identities or base revisions were supplied; all three intents are create.
- The factory validates `.litefactory-plan-proposal.json`, publishes full story bodies, and replaces the empty Story Index with verified GitHub links. It then removes the transient ignored transport before save. No child URLs or issue identities have been guessed, and no per-story Markdown bodies are written.

## Out of Scope
- Polling optimization, the withdrawn leak fix, concurrency policy changes and lock-based self-healing.
- New metrics backends, process-launch counters, global worker IDs or control-action taxonomies.
- Deployment, runtime-state edits, daemon restart, and production identity/deployed-revision confirmation. Those remain operational follow-up enabled by the diagnostics.
- Authentic nested-agent capture, historical issue recovery and sandbox expansion.

## Provenance
- Citation base: ac78c27abd38a28977d5e139acec6c566df67f05; [architecture review](ARCH_REVIEW.md) and [requirements](feature-spec.md) document source review and evidence bounds.
- Research's managed-parent explanation fits repository behavior but does not identify the two production sessions or establish deployed revision equivalence.
- Headless assumptions: preserve legacy counters, count executor requests explicitly, and scope acceptance to offline repository behavior. No owner clarification was required.
- Architecture refined terminal timing before acceptance freeze: ownership cleanup precedes terminal logging, but final pool release/join follows it so shutdown joins the diagnostic. Earlier wording promising all releases before logging is superseded.
- Before implementation, recheck source locations and the factory-supplied frozen contract. Planned tests and reviews are outstanding; this plan claims no implementation acceptance or production recovery.
