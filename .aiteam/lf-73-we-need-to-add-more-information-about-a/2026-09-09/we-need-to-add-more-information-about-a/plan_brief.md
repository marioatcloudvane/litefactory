# Plan Brief — Token and Model Metering
# Feature: we-need-to-add-more-information-about-a
# Branch: lf/73-we-need-to-add-more-information-about-a
# Date: 2026-09-09
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/73
# Citation base: 2c7d6d9527d606762f9acdbec52b6823a7b71ba1
# Research brief: none in this session; current owner scope and fresh repository inspection

## Shared Decisions
- Full root and all causally related descendant consumption with actual-model provenance for both Claude and Codex is mandatory in v1. Exact supported versions require real-execution proof; unknown models or missing children cannot be represented as complete support.
- Extend internal/agent; provider collection belongs in harness/codex, attempt orchestration in turn, SQL and durable DTOs in store, presentation in cli/report. Pure shared normalization/reconciliation/valuation may live in internal/meter without subprocess, database, clock or network ownership.
- Identity hierarchy is task/run/attempt/invocation/measurement. Allocate a distinct attempt before every subprocess, including fallback. Incremental checkpoints preserve observed work; cumulative baselines and source identities prevent replay double counting. Inclusive totals reconcile rather than add to exclusive measurements.
- Use nonnegative integer uncached input, cache read, cache write and output quantities, nullable when unknown. Preserve cache-duration and reasoning details as non-additive subsets where appropriate. Complete zero differs from unknown; partial totals include diagnostics and coverage.
- Use editable immutable full price catalogs, exact USD-per-million rates and deterministic documented rounding. Pin the complete catalog before attempt execution, including missing rates. Store valuation provenance and never recalculate historical or in-flight estimates after edits. No rates means unpriced tokens, not free usage. Legacy reported costs remain separately labelled.
- Assumed CLI entrypoint: meter. Aggregate all attempts by existing repository/task/mode/agent metadata and actual model; preserve uncertainty under filters. Separate daemon helper calls are excluded and labelled as overhead outside managed-run totals.

## Workflow
- US-1 establishes contract and proves collection feasibility. US-2 (Claude), US-3 (Codex) and US-4 (durability) follow US-1 and can proceed independently.
- US-5 follows US-4; US-6 follows US-4 and US-5. US-7 follows US-2 through US-6 and gates the combined release. No provider may ship as full support while its actual-model descendant proof is unresolved.
- Each managed story is verified immediately using its exact AC evidence identifiers, scoped review and appropriate tests, and the factory-supplied per-run event is published by Implement. Final aggregate gates remain required after all stories.
- All seven stories are one coordinated v1 scope. Story bodies are exclusively in the transient proposal until factory publication; there are no editable local story files.

## Cross-Story Constraints
- Preserve store's leaf ownership, context-first accessors, one SQLite connection, numbered embedded migrations and explicit migration. Keep transactions short and outside subprocess/forge I/O.
- Preserve config purity, consumer-owned interfaces, report dependency direction and daemon/turn clock separation. Clone mutable shared payloads at ownership boundaries and use bounded context-aware collector lifetimes.
- Collect only demonstrably associated session/child data through validated safe paths; never sweep global transcripts. Preserve environment allowlists and permissions. Sanitize committed fixtures and keep credentials/prompts out of measurements and summaries.
- Preserve process outcome classification, report transaction/watermark ordering and running-state rules; report measurement completeness separately from process success. No manual LF_HOME edits, permission bypasses, daemon restarts or dist writes.
- Use fixture/unit tests plus existing real SQLite tests, race-safe synchronization and scoped reviews. Manual version-pinned provider probes remain scratch-only; no committed tests launch real paid agents. Implement must pass make check and go build ./...; never make build or invented integration tags.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Define a shared usage contract and prove provider collection capabilities | [#172](https://github.com/marioatcloudvane/litefactory/issues/172) |
| US-2 | Collect complete Claude invocation and model usage | [#173](https://github.com/marioatcloudvane/litefactory/issues/173) |
| US-3 | Collect complete Codex invocation and model usage | [#174](https://github.com/marioatcloudvane/litefactory/issues/174) |
| US-4 | Persist attempts and deduplicated invocation measurements | [#175](https://github.com/marioatcloudvane/litefactory/issues/175) |
| US-5 | Manage editable prices and immutable attempt valuations | [#176](https://github.com/marioatcloudvane/litefactory/issues/176) |
| US-6 | Expose metering scopes, details and run summaries | [#177](https://github.com/marioatcloudvane/litefactory/issues/177) |
| US-7 | Verify full descendant metering and release readiness | [#178](https://github.com/marioatcloudvane/litefactory/issues/178) |

## Out of Scope
- Historical-log rollup/import/backfill code, scripts, commands or migration behavior in this repository. Any recovery script is separate outside-repository work and is not produced by this plan.
- Repricing historical/in-flight attempts, automatic remote prices, invoices, subscriptions, credits or taxes, and additional coding agents.
- Separate daemon helper overhead in managed-run consumption; mandatory managed-root descendants are not excluded.

## Provenance
- Citation base: 2c7d6d9527d606762f9acdbec52b6823a7b71ba1. Fresh inputs: feature-spec.md and ARCH_REVIEW.md in this session, current owner requirements and current repository inspection.
- Prior run completion claims and upstream provider capabilities were not adopted as facts. No existing factory story identities were supplied; the proposal therefore contains only create intents with zero numeric IDs and empty bases.
- The empty link-only Story Index is a pre-publication placeholder. Factory verifies and publishes the proposal, replaces this section with actual GitHub links and removes transient transport. No local or invented issue links substitute for that operation.
- Local validation establishes handoff shape only. Factory generation/feature state, publication, verified issue identities and final readiness remain pending factory execution. Re-pin repository citations and exact provider/source versions before implementation or merge if they change.
