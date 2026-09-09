# Plan Brief — a-failed-run-s-uncommitted-work
# Branch: lf/171-a-failed-run-s-uncommitted-work
# Date: 2026-09-09
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/171
# Citation base: 679ed5932ba1d9e6a711175d29cb54d820e5e3a7
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

- Choose nonmutating synchronization deferral for ordinary dirty session worktrees. Recovery runs use the current task base and preserve retained edits without attributing their ownership.
- Use `deferred_dirty` consistently in turn, store and report, with divergence and no synchronization commit SHA. Existing text storage needs no migration.
- Preserve up-to-date, explicitly disabled and distance-cap precedence. Check ordinary dirty status before committed-state merge preview; preserve low-level merge/apply guards and proposal revision validation.
- Keep success-only harvest and existing reserved transport exclusion. Failed output must not be committed merely to clear preparation.
- Ordinary owner comments provide recovery. No new command, configuration, permission or scheduling behavior is introduced.

## Workflow

- Shipping order: US-1 establishes the usable recovery contract; US-2 and US-3 then independently protect confirmation and Plan boundaries. Final completion requires all three.
- Dependency graph: US-1 → US-2; US-1 → US-3. No dependency between US-2 and US-3.
- Implement consumes factory-supplied frozen GitHub revisions. Verify each story immediately with its own acceptance checks, review and tests, and publish the supplied per-run completion evidence with the exact leading AC identifiers before proceeding to dependents. Final aggregate gates remain required.

## Cross-Story Constraints

- Keep git CLI ownership in `internal/git`, SQL in `internal/store`, preparation orchestration in `internal/turn`, and timing/admission in existing daemon boundaries. Use existing clone gating and caller context; introduce no workers or retry loops.
- Preserve task HEAD, index, retained working-file bytes/modes and untracked files across deferral/refusal. Fetch may advance remote-tracking refs. Do not weaken admission, authorization, confirmation revision checks or dirty merge/apply protections.
- Keep reports truthful and redacted, and retain existing outbound-report formatting rules. An explicit deferral is not a successful merge or resolved conflict.
- Test with existing fakes, real local git and migrated SQLite using stdlib tests; no external services, real agents, new dependencies, integration tags or sleep-based synchronization.
- Run required scoped checks for each story and `make check` as the final implementation gate. `go build ./...` is safe; never run `make build`, write `dist/`, or edit live daemon/runtime state.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Preserve retained work and let retries run with synchronization deferred | [#180](https://github.com/marioatcloudvane/litefactory/issues/180) |
| US-2 | Make dirty conflict-confirmation refusals recoverable through ordinary retries | [#181](https://github.com/marioatcloudvane/litefactory/issues/181) |
| US-3 | Protect failed planning and reserved proposal transport from retry regressions | [#182](https://github.com/marioatcloudvane/litefactory/issues/182) |

## Out of Scope

- Stashes, scratch refs, snapshots, ownership manifests, crash recovery state machines, automatic cleanup, or automatic reconciliation of dirty edits.
- Unconditional failed-output commits, success classification changes, automated conflict confirmation, or new owner controls.
- Repairing issue #73 or changing live runtime state; agents reading or mutating GitHub.
- Broad transport lifecycle redesign or migration of tracked reserved paths.

## Provenance

- Citation base: `679ed5932ba1d9e6a711175d29cb54d820e5e3a7`. See [feature-spec.md](feature-spec.md) and [ARCH_REVIEW.md](ARCH_REVIEW.md) for scope, architecture and source landmarks.
- Assumptions: the headless request authorizes completing this plan; disclosed recovery on the current base is acceptable; existing distance caps remain independent limits. No factory-supplied existing story identities or base revisions were provided, so all proposed stories are creates with zero identities and an empty expected base.
- The historical incident is supplied evidence; this checkout already filters proposal-only untracked transport. Source landmarks must be rechecked against the implementation checkout and factory-frozen story revisions before changes.
- Managed handoff: root `.litefactory-plan-proposal.json`, version 1, parent `github:marioatcloudvane/litefactory#171`, generation 1. It is transient transport and must never be committed. The factory alone publishes stories, fills index links and verifies publication; no story Markdown files are produced.
