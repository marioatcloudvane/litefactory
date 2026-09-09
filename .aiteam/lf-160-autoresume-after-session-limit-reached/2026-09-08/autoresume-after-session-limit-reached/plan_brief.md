# Plan Brief — autoresume-after-session-limit-reached
# Branch: lf/160-autoresume-after-session-limit-reached
# Date: 2026-09-08
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/160
# Citation base: ca9d101b217695b94f16a03b5688fc7f5c2c5754
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)
# Status: PLAN COMPLETE — FACTORY PUBLICATION PENDING

## Shared Decisions

- Both Codex and Claude Code ship through their existing CLI adapters. Use global strict YAML `auto_resume_on_session_limit`, absent/default false, with the existing loaded-configuration lifecycle and no hot reload.
- Only verified terminal provider quota evidence grants recovery eligibility. Arbitrary generated text cannot authorize scheduling. The adapters own provider syntax and publish a neutral outcome through `internal/agent`; `internal/turn` records one execution, `internal/daemon` owns time/admission, `internal/store` owns persistence, and `internal/report` renders redacted outcomes.
- Select the latest necessary reset among explicitly known exhausted windows; missing required reset prevents recovery. Claude exact seconds use reset+60 elapsed seconds. Codex timezone-less displayed minute uses machine-local interpretation, next-minute boundary+60 elapsed seconds. Persist UTC once; never override agent timezone. Ambiguous/malformed/stale evidence is report-only.
- Durably record semantic interruption and uniquely source-run-owned continuation intent before posting the visible absolute-UTC `/schedule` record. That factory comment describes existing state and remains inert to bot-command ingestion. Delivery may be at least once; schedule/run allocation must not duplicate.
- Continue the original worktree, task, provider, mode, segment, request and explicit conversation. Preserve discovered handles on verified interruption, require resume for automatic recovery and retain existing manual fallback. Managed Implement always consumes factory-frozen approved story revisions, including completed-story and aggregate-only recovery semantics.
- Adopt the prior plan's **three actual automatic invocations per root interruption** as a disclosed headless assumption, not an owner-specified number. Reservations/report retries do not consume attempts; further attempts require new verified future-reset evidence.
- Adopt permanent cancellation of pending recovery on newer admitted work/context reset, observed closure or loaded disable as the carried-forward owner-intent assumption. Schedule controls alone do not supersede unrelated work; failed observation defers; reopening/re-enabling does not resurrect canceled actions.
- Preserve original scheduled prerequisite identity throughout recovery. Eligible interruptions suspend it; final semantic success releases descendants at final completion time, and terminal failure/cancellation blocks them. Exit zero and reported state alone cannot prove success.

## Workflow

US-1 and US-2 can begin independently. US-3 consumes their configuration and evidence contracts; US-4 consumes US-3's durable context and reservation state. US-5 completes restart reconciliation and dependency settlement after US-3 and US-4.

Dependency graph: `US-1 + US-2 → US-3 → US-4 → US-5`, with an additional direct `US-3 → US-5` dependency. Agree the neutral evidence and complete persistence contracts, including suspended prerequisites, before US-3 lands. Both adapters and the entire durable dispatch/reconciliation path ship together; intermediate story completion does not imply feature availability.

Implement consumes factory-supplied frozen selected story identities/revisions, never local story files or live edits. Verify every story immediately with its own acceptance checks, independent review and tests; publish its supplied per-run evidence event before siblings/parent finish. Use each leading AC identifier exactly as its evidence name. Story closure does not replace final aggregate gates, and later invalidation remains possible.

## Cross-Story Constraints

- Keep SQL and migrations in store with one SQLite connection and explicit migration. Add a numbered embedded migration; preserve manual schedule identity. Reuse existing workers, session exclusion, cross-process turn locks and clone gates. No sleeps or detached retry loops below daemon.
- Preserve bot/owner authorization, numeric admission, render validation/redaction, permission resolution, approved managed scope and normal uncommitted partial-work policy. Do not snapshot credentials or commit/push incomplete work.
- Reuse stdlib table tests, handwritten fakes, real migrated SQLite, httptest forge and deterministic channel/Quiesce synchronization. No integration build tags, new test naming convention or separately provisioned infrastructure is needed. Each story carries its own test and review tasks.
- Aggregate implementation gates: `go build ./...`, `make check` (format, vet, pinned lint, race tests) and repository-required coverage checks, with no open blocking review findings or STANDARD_CONFLICT flags. No additional dependencies are planned.
- Do not write `dist/`, run `make build`, restart the live daemon, edit runtime state/configuration or construct permission-bypass flags. The factory alone reads/mutates GitHub, commits/pushes and verifies publication.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Make automatic session-limit recovery an explicit opt-in | [#161](https://github.com/marioatcloudvane/litefactory/issues/161) |
| US-2 | Recognize trustworthy quota interruptions from both coding agents | [#162](https://github.com/marioatcloudvane/litefactory/issues/162) |
| US-3 | Persist and announce one recovery action for each interruption | [#163](https://github.com/marioatcloudvane/litefactory/issues/163) |
| US-4 | Resume the original conversation only while owner intent remains current | [#164](https://github.com/marioatcloudvane/litefactory/issues/164) |
| US-5 | Recover across restarts and settle scheduled dependencies after bounded retries | [#165](https://github.com/marioatcloudvane/litefactory/issues/165) |

## Out of Scope

Other agents; generic failure retries; provider-wide quota coordination; speculative reset dates; automatic cold starts; arbitrary model-prose authority; provider transport migration or fresh paid exhaustion experiments; replacement scheduler or public grammar changes; relaxed permissions/bot admission; changed ordinary partial-work policy; per-repository overrides, configurable retry cap, hot reload or new public cancellation commands; adoption of superseded stories; implementation in this Plan run.

## Provenance

- Current citation base: `ca9d101b217695b94f16a03b5688fc7f5c2c5754`. Inputs: [feature-spec.md](feature-spec.md), [ARCH_REVIEW.md](ARCH_REVIEW.md), imported [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md) and [PROVIDER_EVIDENCE.md](PROVIDER_EVIDENCE.md).
- Historical #142 research/design was reused as evidence, never as managed story identity or editable scope. Its old detection blocker and forced-UTC proposal are superseded by preserved authentic diagnostics and #160's settled scope.
- Historical Codex binary version remains unknown; Claude captures identify 2.1.261/2.1.263. Implementation must document supported diagnostic forms and current version verification separately from authentic historical fixtures without claiming new quota captures.
- The architect checked current runner/harvest/scheduler/store boundaries, including managed story progress absent from the old plan. Before implementation/merge, re-pin affected source contracts, migration numbering and dependency/tool versions against the factory-frozen plan and current base.
- No standards deviation was found. Plan completion means ready for factory publication and a later owner-triggered Implement run; it does not claim code, tests or GitHub publication completed.
