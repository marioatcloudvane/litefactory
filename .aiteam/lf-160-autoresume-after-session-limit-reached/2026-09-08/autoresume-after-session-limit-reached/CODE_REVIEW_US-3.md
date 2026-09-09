# Code Review — US-3

Feature: autoresume-after-session-limit-reached
Reviewed by: go-code-reviewer
Date: 2026-09-08
Status: CLEAN

Frozen identity: github:marioatcloudvane/litefactory#163, issue_id 5390503975, revision b77f99942ecd471904ac8708773d010c83e06d624368ee731a373fb4961c2cdd. Dependencies: verified US-1 and US-2.

## BLOCKING findings

None remain.

### CR-B-301 resolved: Independent announcement discovery connected to delivery

The re-review read `internal/daemon/continuation_reports.go`, the new hook in `internal/daemon/tick.go`, and `internal/daemon/recovery_announcement_test.go`. Every eligible tick enumerates durable report debt before ordinary issue discovery, independently of owner watermarks. It restricts replay to registered GitHub repositories, acquires per-session in-flight exclusion and the cross-process session lock, reconstructs the interrupted report from store data and invokes the existing reporter without allocating a run. Locks are released on every path, delivery failures retain report debt, and rate-limit errors flow through existing tick observation. The regression advances the watermark, fails a post, then drives another tick to assert one confirmed announcement, unchanged owner progress and no agent invocation; a further tick does not repeat the confirmed report. Test execution remains the test engineer/orchestrator's gate.

## ADVISORY findings

None.

## Cross-story summary

The reviewed store transaction persists semantic interruption, optional source-run-unique continuation, resume token and original prerequisite suspension together. Duplicate completion returns the original decision, and `CompleteRunReport` preserves semantic outcome while acknowledging the announcement only after a confirmed post. Automatic identities remain separate from manual schedule uniqueness. The neutral interruption contract and opt-in configuration align with US-1/US-2. Recovery rendering uses persisted UTC values, fixed command syntax and the existing sanitization/redaction pipeline; interrupted work stays outside the successful save path. US-4 dispatch and US-5 reconciliation are intentionally not reviewed here.

## Scope and validation

Read the supplied production files, immediate report/tick callers needed to verify reachability, migration, architecture hints, and currently available scoped continuation tests. This is an independent static review; test execution and criterion evidence are owned by the test engineer/orchestrator. The concurrency, resource handling, parameterized SQL, error handling and package boundary checks found no additional blocking issue in this scope.

## Verdict

CLEAN — CR-B-301 is resolved. Scoped static review passes; publish verification only after the separate scoped test gate passes.

## Attempt 2 revalidation after US-4 shared edits

Static verdict: CLEAN. Reviewed transactional automatic allocation in `store/runs.go` and its immediate validation helper, the cancellation acknowledgement migration field, tick dispatch/notice ordering, resume-only harvest guard, cancellation-aware recovery rendering, and the immediate notice/report helpers. These preserve the atomic interruption/intent decision, original source-run uniqueness, semantic outcome retention, confirmed-post bookkeeping, and watermark-independent delivery contract. The automatic resume guard does not alter ordinary manual fallback.

Validation command: `go test -race ./internal/store ./internal/turn ./internal/daemon -run 'TestContinuation|TestInterruption|TestRecoveryAnnouncement'` exited 1: store passed (22.512s), turn passed (32.082s), daemon failed `TestRecoveryAnnouncementReplayIgnoresAdvancedWatermark`. Its fixture leaves automatic recovery disabled, so the new US-4 cancellation hook correctly cancels the seeded continuation and produces cancellation/manual notices instead of the expected single schedule announcement. Explicitly enable recovery in that fixture before renewing AC-3.6 evidence. No production regression identified. `go test -race ./internal/report` passed (6.486s).

The test engineer corrected the fixture to explicitly enable recovery and align its saved worktree with the session. Read-only fixture re-review passed. Reran only the failed package scenario: `go test -race ./internal/daemon -run 'TestRecoveryAnnouncement'` exited 0, daemon passed (7.808s). Prior green store/turn/report results remain applicable because no production code changed during this correction. The helper `recoveryDaemon` in `internal/daemon/continuation_admission_test.go` is now a validation dependency to include in the scoped snapshot.

### Attempt 2 criterion mapping

- AC-3.1: `TestInterruptionDurableBeforeAnnouncementAndReplay` passes enabled eligible persistence and disabled/missing-reset manual handling; store rollback test confirms invalid intent cannot partially finish the run or token. Static eligibility review retains conversation-identity and attempt guards.
- AC-3.2: `TestContinuationCompletionIsAtomicAndIdempotent` passes source triggers 0, owner comment and private schedule identity, duplicate completion and report replay. `TestContinuationPreservesScheduledPrerequisiteAndManualUniqueness` passes original manual schedule uniqueness.
- AC-3.3: `TestContinuationRestartPreservesIntentAndIndependentReportDiscovery` passes real SQLite close/reopen round-trip of continuation identity/context/lifecycle. Prerequisite test passes UTC normalization and original prerequisite retention.
- AC-3.4: `TestInterruptionDurableBeforeAnnouncementAndReplay` and `TestInterruptionRetainsHandleOnResumedTurn` pass first/resumed handle retention, uncommitted partial file preservation and interrupted/not-complete report wording.
- AC-3.5: Interruption replay test passes exact persisted absolute-UTC schedule payload; `TestRecoveryAnnouncementCannotCreateScheduleOrRun` passes bot inertness. Full report package race tests pass rendering and redaction regressions.
- AC-3.6: Corrected `TestRecoveryAnnouncementReplayIgnoresAdvancedWatermark` passes failed post, independently discovered report debt, confirmed-post acknowledgement, unchanged advanced watermark and no additional agent execution or confirmed duplicate announcement. Static replay locking and source-run uniqueness remain intact.
- AC-3.7: Real SQLite completion/rollback/idempotency/reopen tests pass; semantic interruption and non-success survive `RunReported`, and failed report retry leaves the durable action recoverable.

### Attempt 2 final verdict

CLEAN — scoped review and refreshed acceptance race regressions pass. All seven original US-3 criterion mappings remain valid; ready for the orchestrator's renewed immutable snapshot and evidence event.

## Attempt 3 renewed independent review after US-5 overlaps

Status: CLEAN. Reviewed current store/runs.go, continuations.go, continuation_dispatch.go, continuation_settlement.go, migration 0017, daemon/schedules.go, tick.go and continuation dispatch/report/cancellation helpers, plus turn continuation/harvest/preparation/report integration. All paths are under internal/. The full independent cross-story review is recorded in CODE_REVIEW.md.

US-5 settlement additions preserve AC-3.1 atomic eligibility/outcome/intent ordering, AC-3.2 source-run idempotency and manual uniqueness, AC-3.3 durable UTC/context/lifecycle fields, AC-3.4 handle retention and success-only partial-work save policy, AC-3.5 persisted inert UTC announcement rendering, AC-3.6 watermark-independent confirmed-post debt, and AC-3.7 persistent semantic interruption after reporting. `settleContinuationRun` runs after successor creation inside the interruption transaction; terminal chain settlement does not erase source report debt or interruption evidence.

No blocking or advisory production finding remains. This is a renewed static verdict, not a claim that the concurrently running scoped tests or aggregate gates have completed. Publish renewed evidence only after the test owner reports success and capture current immutable implementation/validation bytes.

## Final lint-fix review refresh

CLEAN. Inspected the exported API comments added in internal/store/continuations.go and continuation_dispatch.go: they accurately describe existing ownership, selection, attempt and cancellation behavior and change no execution logic. Inspected the lifecycle test declaration/conditional cleanup in internal/daemon/continuation_lifecycle_test.go: removing redundant empty-string initialization and flattening the terminal-success condition preserve the assertions. `git diff --check` exits zero. The orchestrator reports the focused lifecycle race/coverage test passed in 15.506s; no duplicate test run was performed by this reviewer. Prior production and renewed story verdicts remain valid. Final aggregate checks are still owned by the orchestrator.
