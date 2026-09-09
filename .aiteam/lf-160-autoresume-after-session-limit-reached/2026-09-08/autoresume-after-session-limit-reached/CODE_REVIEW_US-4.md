# Code Review — US-4

Feature: autoresume-after-session-limit-reached
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: CLEAN

Frozen story: #164, issue_id 5390504428, revision `744d8b5669eb5bcb112eb9def735d7b26b1b4abc6af02825a1c085e2e6946c27`; dependencies: US-3.

## BLOCKING findings

None remaining.

### Resolved CR-B-001: Durable manual-recovery cancellation notices

Re-reviewed migration 0017's separate `cancellation_comment_id`, store notice discovery and confirmed-post acknowledgement, `daemon/continuation_cancellations.go`, its tick hooks, and the delayed original report rendering in `turn/continuations.go`.

Cancelled actions now remain discoverable independently of the source-run report. Delivery uses existing session inflight/task ownership, rechecks notice debt under the lock, renders through the existing reporter/redaction boundary, and acknowledges only confirmed positive comment identities. Failed or uncertain posts retain debt for a later tick. The notice uses a private automatic trigger and RunID zero; `consumeSource` makes that trigger a no-op, so delivery allocates no run and advances no owner watermark. A delayed original interruption report now names cancellation instead of repeating a stale automatic deadline. Replaying notices never changes cancelled execution status.

The dedicated test engineer owns cancellation failure/retry/idempotency validation; this verdict records static review and does not claim those tests have finished.

## ADVISORY findings

None.

## Scope and acceptance assessment

Reviewed production changes in store continuation reservation/allocation and run reporting; daemon continuation dispatch and tick/run wiring; turn continuation validation, preparation, delivery, harvest and private-source consumption. `internal/turn/run.go` does not exist; `runClaude` is in harvest.go and was reviewed there. Test implementation and execution remain with the dedicated test engineer.

- AC-4.1: Stored UTC due times gate reservation; dispatch reuses bounded pool, session inflight ownership and existing task/clone locking. Saved mode avoids classification and explicit resume retains original request and segment.
- AC-4.2: Resume-only runs intercept `ErrSessionNotResumable` before fallback. Under-lock validation refuses missing or changed identities. The resolved CR-B-001 adds durable owner-visible manual-recovery notices.
- AC-4.3: Continuation validates approved generation under task ownership and uses the existing preparation/approved-context and relationship-validation paths. Frozen story context is reconstructed for the resumed runner; no new live-body story source was introduced.
- AC-4.4: Pending future and due rows are examined before eligibility dispatch. Owner admission plus spawning-command classification excludes schedule controls. Issue closure, loaded false and identity/label changes durably cancel and cannot requeue from cancelled state.
- AC-4.5: Preflight and final forge observations do not advance owner watermarks. Observation errors defer; conditional reservation/allocation rejects cancellation that wins before allocation. The documented final observation boundary precedes ordinary locked turn preparation; newer external work after that boundary follows the stated already-running limitation.
- AC-4.6: UTC deadlines are compared directly rather than reparsed locally. Existing permission resolution remains in daemon run; lock contention requeues a reservation with no run allocation.

## Cross-story summary

US-3 immutable context and source-run ownership feed US-4 without introducing SQL outside store or provider syntax into daemon. Private automatic IDs are excluded from ordinary consumption. Root allocation accounting remains transactional; final chain settlement and restart reconciliation belong to US-5. Cancellation persistence now has its required durable delivery consumer, separate from source-report acknowledgement.

## Verdict

CLEAN — CR-B-001 is resolved; no remaining blocking or advisory findings in the scoped review. Scoped test evidence remains the test engineer’s responsibility.

## Attempt 2 renewed independent review after US-5 overlaps

Status: CLEAN. Reviewed current store/runs.go, continuations.go, continuation_dispatch.go, continuation_settlement.go, migration 0017, daemon/schedules.go, tick.go and continuation dispatch/report/cancellation helpers, plus turn continuation/harvest/preparation/report integration. All paths are under internal/. The full independent cross-story review is recorded in CODE_REVIEW.md.

US-5 settlement additions preserve AC-4.1 UTC/pool/inflight/lock admission, AC-4.2 explicit resume-only behavior and durable manual notices, AC-4.3 factory-frozen generation/context, AC-4.4 permanent current-intent cancellation including future deadlines, AC-4.5 failed-observation deferral without watermark consumption and conditional cancellation-before-allocation, and AC-4.6 persisted UTC deadlines and ordinary permission/manual paths. Transactional settlement never requeues a consumed allocated action; cancellation settles the original prerequisite while retaining cancellation notice debt.

No blocking or advisory production finding remains. This is a renewed static verdict, not a claim that the concurrently running scoped tests or aggregate gates have completed. Publish renewed evidence only after the test owner reports success and capture current immutable implementation/validation bytes.

## Final lint-fix review refresh

CLEAN. Inspected the exported API comments added in internal/store/continuations.go and continuation_dispatch.go: they accurately describe existing ownership, selection, attempt and cancellation behavior and change no execution logic. Inspected the lifecycle test declaration/conditional cleanup in internal/daemon/continuation_lifecycle_test.go: removing redundant empty-string initialization and flattening the terminal-success condition preserve the assertions. `git diff --check` exits zero. The orchestrator reports the focused lifecycle race/coverage test passed in 15.506s; no duplicate test run was performed by this reviewer. Prior production and renewed story verdicts remain valid. Final aggregate checks are still owned by the orchestrator.
