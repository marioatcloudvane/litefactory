# Code Review

Feature: a-closed-issue-keeps-its-needs-poll-flag  
Reviewed by: go-code-reviewer  
Date: 2026-09-09  
Status: CLEAN

## Scope

US-1, GitHub issue #179 (5396991973), revision `96542e65fbc2199159a9698271747405c9d4c6801335da7f6a094708f3df6024`, no dependencies. Reviewed the supplied frozen body, shared plan context, changes in `internal/daemon/tick.go`, `internal/store/sessions.go`, `internal/daemon/backlog_test.go`, and the complete new `internal/daemon/backlog_closure_test.go`. Inspected existing fixture, dispatch, abandoned-report, and marker round-trip regression dependencies. Test review is included because the approved story explicitly requires acceptance-evidence review.

## BLOCKING findings

None outstanding.

### CR-B-001 resolved: Remaining concurrency-derived read-budget comment

Re-review inspected only the corrected `TickResult.Repos` comment in `internal/daemon/tick.go` and the associated AC-7 finding. It now correctly states that backlog reads occur after admission guards and released slots permit additional reads, so worker concurrency is not a request budget. The change is comment-only and resolves the finding; AC-1 through AC-6 assessments are retained unchanged.

## ADVISORY findings

None.

## Acceptance assessment

| Identifier | Review assessment and concrete validation reference |
| --- | --- |
| AC-1 | Implementation clears only explicit `closed` observations through the existing store accessor before returning a skipped candidate. `offer` releases both resources before returning and never reaches worker launch/counting. `TestClosedBacklogRetirementSurvivesRestart` covers both idle and failed sessions and zero dispatch/execution. |
| AC-2 | The marker query excludes cleared rows; no new state or cache participates. The same test checks three later ticks and closes/reopens the real database, constructs a fresh daemon, calls Start, and checks the retained clear marker and unchanged direct-GET count. |
| AC-3 | `TestClosedBacklogReleasesAdmissionForOpenWork` places three older closed candidates ahead of useful open work with one slot, requires all three direct GETs, clears every marker, and dispatches the open comment in that tick. |
| AC-4 | Lookup errors return before clearing; clear errors wrap their cause and follow the same resource-release and rate-limit path. `TestUncertainBacklogObservationRetainsRetry` covers explicit unknown, missing, inaccessible, transport, and rate-limit observations with successful subsequent admission. `TestFailedClosureClearRetainsReadableRetry` cancels immediately after successful HTTP lookup, causing the real context-first store clear to fail; it checks the clear error log, cancellation result, a readable still-flagged row, re-admission of the same task, and later useful dispatch. This exercises persistence failure rather than an absent-row shortcut. |
| AC-5 | Existing SQL changes only needs_poll and updated_at. The restart test compares complete session values after normalizing those two fields and checks the watermark. `TestClosureRetirementPreservesOwedAbandonedReport` compares the owed run before/after retirement, then verifies Start reports it on the closed issue, advances its watermark only after delivery, and marks it reported. Existing `TestAnAbandonedRunIsReconciledEvenWhenItsIssueWasClosed` remains applicable. |
| AC-6 | `TestRetiredBacklogReopensWithoutReplayingConsumedComments` consumes comment 401, retires with 402 pending, reopens through normal discovery, and asserts the same session identity, triggers exactly [401,402], watermark 402, and no additional backlog GET. |
| AC-7 | Existing clean drain/bootstrap, failed bootstrap, unconsumed comment, contended lock, and backoff deferral tests retain their assertions. Marker round-trip tests cover persistence and missing-row errors. Production protocol comments describe both clear paths and repeated timestamp writes correctly. Re-review confirmed the remaining Repos comment no longer promises a concurrency-derived issue-read budget; AC-7 passes scoped review. |

## Architecture, concurrency, and resource assessment

The production behavior change is confined to existing ordinary materialisation. SQL ownership, single-connection storage, context propagation, error unwrapping, and in-flight-before-capacity ordering remain unchanged. No goroutine, interface, dependency, schema, or external execution is added. Clear errors are propagated rather than swallowed by the clean-drain logging helper. Reports, watermarks, continuation/schedule policy, and normal discovery are unchanged. Tests reuse the fake HTTP forge, migrated SQLite, fake executor, and existing Quiesce/settle synchronization; they introduce no sleeps or capacity observables.

The approved boundary deliberately preserves the race after an already-open discovery observation and does not guarantee immediate reopening detection. A closed issue may still be read because ordinary discovery observes recent comments or because another independent obligation needs it; retirement removes only ordinary durable backlog intent.

## Validation status

The orchestrator supplied passing current-run `go build ./...`, `go test ./internal/daemon ./internal/store`, and targeted closure race/coverage results. Full scoped race/coverage results are pending with the orchestrator. This reviewer performed static inspection rather than duplicating those commands. CR-B-001 is resolved. Per-story verification requires confirmed passing scoped checks from the orchestrator; parent aggregate gates remain separate.

## Verdict

CLEAN — CR-B-001 is resolved. No outstanding production correctness, architecture, or acceptance-evidence review findings remain for AC-1 through AC-7.
