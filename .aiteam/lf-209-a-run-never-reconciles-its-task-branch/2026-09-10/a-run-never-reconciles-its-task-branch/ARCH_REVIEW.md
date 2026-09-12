# Architecture Review: task-branch reconciliation

Date: 2026-09-10
Verdict: READY for typed story authoring. No owner question or standard deviation required.

## System overview

Extend the existing preparation, conflict recovery and successful-save lifecycle. No new package, configuration switch, scheduler, history tracker or command execution language is needed. The defect sits between fetch/worktree selection and base synchronization in `internal/turn/prepare.go`.

Data flow: existing clone gate → fetch with established task-ref coverage → ensure task worktree → observe task remote → safe task reconciliation or refusal → existing base synchronization policy → runner → successful save of unpublished commits → truthful report. Validate takes the observation-only branch. Confirmation is a distinct path: observe all approved inputs before mutation, refuse stale approval, then apply the exact approved target through existing conflict machinery.

Package boundaries:
- `internal/git` owns every Git invocation and preservation/rollback guarantee. Fetch stays on Clone; branch mutations stay on Worktree (SEC-7). Add only minimal operations/options needed to obtain authoritative task-ref observation and guarded fast-forward.
- `internal/turn` owns task-before-base ordering, target selection, freshness decisions, proposal orchestration, save eligibility and conversion to report/store values.
- `internal/store` owns additive migration and all persistence. Keep SQLite's single connection and context-first accessors; do not add SQL or transaction ownership elsewhere.
- `internal/report` renders phase-aware values and proposals without importing turn. Continue redaction and numeric-owner authorization in their existing owners.
- Existing `internal/agent`, harness and Codex conflict contracts are reused where target context needs to pass through. No new model capability is assumed.

All preparation observation and mutation stays within the existing CloneGate section. The gate serializes factory operations, not an external operator: record observed SHAs, use exact resolved inputs, and handle push rejection. Advisor execution remains outside the clone gate. Add no goroutines, sleeps or turn-local retry loops; preserve cancellation and final SessionRunning write (CONC-2, CONC-5–7).

## Cross-story decisions

1. Safe automatic behavior is equal/ahead-only no-op, clean behind-only fast-forward, and compatible divergence merge preserving both histories. Base sync continues using its existing policy; task reconciliation has no opt-out, distance cap or permissive stale execution switch. Confirmed remote absence is normal; uncertain coverage is not absence.
2. Task/base are two observations sharing terminology, not one mutable result. Preserve existing base-only record interpretation, represent historical task observation as absent/unrecorded, and keep branch/ref identity plus compared commits and resulting commit where applicable. Prelaunch failure uses existing refusal/report lifecycle; do not allocate a fake agent run merely for sync persistence.
3. Dirty work remains untouched. Missing task commits plus dirty work stops ordinary execution with explicit manual fallback; task up-to-date plus dirty work still reaches existing deferred-base recovery. This distinction preserves CONC-16. Existing confirmation of dirty work stays pending without launching an agent (CONC-17).
4. Extend the existing proposal with target phase/ref and observed task remote state, including known absence. Both task and base proposals verify local HEAD and all relevant remote inputs before any merge/apply. Old proposals without sufficient binding retain history but require refreshed approval. Existing region limits, preview redaction, raw-byte reconstruction, excluded paths, staged validation and restoration guards remain mandatory (SEC-22–24).
5. Save remains success-only (CONC-8), but publication eligibility is based on unpublished commits rather than only change since post-preparation HEAD. A no-op successful runner must publish a preparation merge or earlier retained commit; a failed runner cannot authorize publication. A verified identical remote tip may be recognized as already published. A late remote change requiring another merge changes the verified tree and follows proposed recovery/newly verified execution; never silently merge after checks, force-push, or undo local commits.
6. Validate observes/discloses freshness and preserves code and retained snapshot targets; it never becomes a synchronization or test execution path (SEC-21, CONC-11). No changes to historical acceptance or parent delivery approval semantics.

## Recommended story order and task guidance

Three sequential stories are sufficient. These are authoring hints, not a second editable story source; the product owner supplies final typed tasks and canonical criterion IDs.

### Story 1 — reconcile normal task startup safely

Scope: Git coverage/observation, guarded fast-forward/merge, task-before-base execution, truthful refusals, task/base result representation and initial persistence/reporting, Validate observation-only behavior. Cover spec AC-001/002 and basic AC-003/005.

Implementation guidance: extend the current synchronization result vocabulary in turn/store/report with a phase discriminator and bounded observation metadata, keeping existing base fields compatible. Introduce additive storage only where needed for run observations. Keep prelaunch outcomes on existing refusal report path with observable compared inputs; do not redesign preparation records. Ensure reused worktrees and a recreated worktree using an existing local branch converge on the same preparation logic. Establish task fetch coverage before absence decisions; no assumption that the user's fetch refspec is broad enough. Worktree fast-forward must enforce the same reserved-transport/staged/dirty barriers and failed-operation preservation as current merge. Do not reuse `MergeBase` unchanged for fast-forward: it explicitly forces `--no-ff`.

Test guidance: use existing real local Git/bare-origin and migrated SQLite fixtures; assert runner inspected expected HEAD, both histories survive divergence, absent and remote-only cases remain valid, narrow refspec cannot hide commits, task refusal is independent of base settings, Validate preserves snapshot, and dirty base-only recovery remains intact. Include store compatibility and reporting checks with scoped evidence.

### Story 2 — reuse reviewed conflict recovery for either target

Depends on Story 1. Scope: task-target advisor proposal/preview, phase-aware durable proposal fields, confirmation freshness for both base and task proposals, supported automatic application after owner acceptance, unsupported/manual fallback. Cover remaining AC-003/004.

Implementation guidance: existing `turn` conflict refusal/advisor pipeline owns proposal generation; existing store proposal records gain bounded target and task observation fields through a new migration. Task target must not be mislabeled as base in prompts/previews. Keep proposal generation separate from apply, and stale checks before any automatic reconciliation on confirmation. Current exact region validation and restoration remain the authority; do not invent an arbitrary command executor. Applying a task resolution ends that existing confirmation operation; subsequent normal execution handles remaining base synchronization using existing flow. Preserve postcommit push-failure evidence and pending/consumption rules.

Test guidance: fake advisor with real Git conflicts proves factory orchestration, never advisor quality. Cover owner confirmation, changed HEAD, changed base, changed task remote, known absence becoming present, old unbound proposal, dirty confirmation, invalid/excluded proposal content and failure restoration. No live model invocation.

### Story 3 — save preparation-only work and report publication races

Depends on Stories 1 and 2. Scope: successful final save eligibility, accurate no-op versus unpublished state, earlier retained commit publication, bounded second-writer race handling and aggregate regressions. Cover AC-006/007 plus final AC-005 reporting.

Implementation guidance: stay in `turn/harvest.go`'s current gate and success guard. Keep code-change reporting distinct from publication need so remote-only fast-forward does not invent agent work. Determine whether local commits still need publication through Git-owned observation; stale tracking data alone must not justify a completed-publication claim. Preserve push verification and report errors with local commit/worktree recovery evidence. Automatic completion may recognize an already-published unchanged verified tip; diverged remote state yields proposed/manual recovery and no false save success. Avoid new retry scheduling.

Test guidance: no-op runner after preparation merge, no-op runner after prior failed attempt, failed runner retaining local work, already-published remote head, remote advancing deterministically before push, and failed push preservation. Assert result publication state and report text, not only command exit. Run required repository `make check` after engineering, retain output for read-only Validate.

## Constraints and platform review

- Platform: layout and interfaces — existing internal packages; consumer-owned minimal interfaces only (TEST-5). No new service/binary/router or third-party dependency.
- Platform: errors — typed failure/refusal values and wrapped Git causes; no string matching or hidden failures (CONC-3, STYLE-5).
- Platform: concurrency — existing clone gate, context-first I/O, no new worker or clock (CONC-2/5/6); avoid transactions spanning Git/advisor/forge calls.
- Platform: data access — store-only SQL, one connection, additive embedded migrations, historical compatibility. No migration framework.
- Platform: testing — stdlib/table-driven tests and existing offline real infrastructure fixtures (TEST-1–3/6); deterministic channel rendezvous for concurrency (TEST-8). No integration tags, paid nested agents or new assertion library. Project make-check requirements take precedence over generic platform suggestions.
- Platform: logging — injected slog and redacted outbound report values (SEC-6, STYLE-2/11); no metrics subsystem.
- Platform: formatting/lint — gofmt, vet, pinned lint and race suite via make check. Surface/gate test changes need design review, never weakening solely to pass (TEST-10).
- Platform: HTTP/server lifecycle — not applicable (no new endpoint/service).
- Platform: dependencies — not applicable (none introduced).
- Instruction ownership — no protected instruction edits are required; no cross-harness delivery dependency introduced.

## Evidence inventory review

PASS for planning readiness: feature-spec inventory covers each candidate criterion with proof method, source availability, version/context bounds and missing execution. Story authoring must carry matching rows/notes keyed to final story criteria. Existing local Git + SQLite + fake-runner/advisor fixtures can establish all scoped factory behaviors. Implementation must retain actual passing outputs, implementation snapshot and criterion references; no current test pass is claimed. Source-only architecture review is not executed evidence. The nested-agent decision imposes no unavailable authentic capture prerequisite because this feature makes no live provider quality/capability claim. For new Git invocation behavior, retain the Git version and scratch/offline re-verification procedure under EXT-1/2.

## Source citation base

- `internal/turn/prepare.go:543` — clone-gated fetch, worktree setup, ordinary versus confirmed paths.
- `internal/turn/prepare.go:599` — existing local/base SHA freshness checks; target currently fixed to base.
- `internal/turn/prepare.go:150` — refusal before segment/run allocation.
- `internal/turn/prepare.go:788` — Validate skip, existing base policy and dirty deferred recovery.
- `internal/git/clone.go:127` — configured-refspec fetch.
- `internal/git/merge.go:297` — no-ff base merge and reserved-path guards.
- `internal/git/worktree_apply.go` — exact validated proposal apply and restoration.
- `internal/store/conflict_proposals.go:61` — existing proposal SHA persistence.
- `internal/store/runs.go:406` — single base sync observation storage.
- `internal/turn/harvest.go:324` — success-only save.
- `internal/turn/harvest.go:374` — gate and post-preparation SHA change shortcut causing missed publication.
- `docs/decisions/nested-agent-evidence.md` — evidence scope and inventory contract.

No architecture deviation is requested. No implementation or tests were executed by this review.
