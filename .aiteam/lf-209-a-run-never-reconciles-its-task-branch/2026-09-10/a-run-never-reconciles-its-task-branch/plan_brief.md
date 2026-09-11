# Plan Brief — a-run-never-reconciles-its-task-branch

- Branch: `lf/209-a-run-never-reconciles-its-task-branch`
- Date: 2026-09-10
- Parent issue: [#209](https://github.com/marioatcloudvane/litefactory/issues/209)
- Citation base: `97c411a82a61302a1d6729d940f9910a2fb7c7a1`.
- Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

- Automate reconciliation when it preserves work and existing safeguards. Where automation cannot complete, reuse the current proposed-resolution and owner-confirmation experience, including its explicit manual fallback for unsupported cases.
- Task and base synchronization remain distinct observations using shared outcome terminology. Keep historical base-only records readable without inventing task observations.
- Existing `internal/git`, `internal/turn`, `internal/store` and `internal/report` retain their responsibilities. Extend bounded values and existing flows; introduce no package, policy switch, history tracker, scheduler or arbitrary recovery-command executor.
- Preserve local edits and commits, reserved-path guards, numeric-owner authorization and exact approval-input checks. Preparation and confirmation must not silently change approved inputs.
- Publication remains subject to the existing successful-run guard. Changed code after checks requires renewed verification; retained local work is not confirmed remote publication.

## Workflow

Three sequential stories cover startup, approved recovery and successful save. The first establishes shared task/base observations and safe startup behavior; the second extends existing recovery using those observations; the third completes publication and aggregate regression coverage. Dependencies are US-001 → US-002 → US-003, with US-003 also depending on US-001.

The factory publishes the typed story contribution, supplies issue identities and replaces the Story Index links. Input acknowledgment alone does not establish publication or readiness. Implement performs engineering and executed checks; Validate uses retained evidence in accordance with the factory's persisted workflow. No mode is started automatically.

## Cross-Story Constraints

- Keep all Git operations in `internal/git`, all SQL in `internal/store`, additive embedded migrations and SQLite's single connection. Preserve clone-gate ownership, cancellation and existing external-operation boundaries.
- Preserve dirty work and failed-run work. Never force-push, destructively reset or introduce automatic stash/discard behavior.
- Validate remains read-only and retains its identified evidence snapshot; current branch movement does not expire historical acceptance. Concrete delivery approval remains bound to the current PR/head/main target.
- Use existing offline real Git/SQLite and fake runner/advisor fixtures, with deterministic concurrency controls. Implement retains actual executed results and version/context notes; Validate runs no fresh checks. Required final engineering checks include `make check`; no integration tags or live nested-agent probes.
- No protected instruction-tree changes are required. Do not write runtime state, `dist/`, commits or pushes from agent work.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Reconcile fetched task commits before ordinary execution | [#234](https://github.com/marioatcloudvane/litefactory/issues/234) |
| US-002 | Recover task conflicts through existing owner-approved proposals | [#235](https://github.com/marioatcloudvane/litefactory/issues/235) |
| US-003 | Publish preparation-only commits and report publication races | [#236](https://github.com/marioatcloudvane/litefactory/issues/236) |

## Out of Scope

- Other issue/session-cache freshness problems and reconstruction of the reported historical incidents.
- History-rewrite detection heuristics, remote-history tracking, new stale-execution settings and automatic background retry scheduling.
- General recovery command execution, new model capability claims, live nested-agent experiments and instruction-tree regeneration.
- Changes to historical acceptance, approval authority or automatic mode selection.

## Provenance

- Requirements: [feature-spec.md](feature-spec.md); architecture: [ARCH_REVIEW.md](ARCH_REVIEW.md).
- Owner decisions are from runs 0004–0005. Earlier assistant recommendations are not separate approved requirements.
- Research citations precede the current architecture review; implementation must recheck touched interfaces and migration numbering against its supplied checkout.
- Assumptions: existing manual fallback covers cases the advisor cannot represent; confirmed remote absence stays normal without a new history tracker; late changes requiring a different verified tree use recovery and renewed execution, while unchanged-tree publication may complete automatically when verified.
- Planning executes no implementation checks and claims no engineering or acceptance result.
