# Code review — run 0005

Status: CLEAN  
Reviewer: go-code-reviewer  
Factory run: 246; generation: 1  
Story: US-1, GitHub #179, issue ID 5396991973  
Parent: github:marioatcloudvane/litefactory#169  
Approved revision: `96542e65fbc2199159a9698271747405c9d4c6801335da7f6a094708f3df6024`  
Dependencies: none

## Current verification scope

This review checked the current worktree and HEAD `86c5d54`, rather than treating previous completion reports as evidence. The worktree was clean at inspection. The implementation is already committed; this run needs no reimplementation. The reviewed current paths are `internal/daemon/tick.go`, `internal/store/sessions.go`, `internal/daemon/backlog_closure_test.go`, `internal/daemon/backlog_test.go`, and the existing fixture/dispatch/abandoned-report/marker regression dependencies. The frozen inline story remains the requirements source.

## Findings

No blocking or advisory findings. Explicit closure classification, persistence-error propagation, admission release, local-state preservation, and regression coverage match the approved architecture. The previously corrected read-budget comment remains correct in the current source.

## Acceptance assessment

| Identifier | Current review result and test evidence mapping |
| --- | --- |
| AC-1 | PASS review. `materialise` clears through `ClearSessionNeedsPoll` only on explicit `closed`; a skipped candidate releases pool and in-flight ownership without launch or dispatch count. `TestClosedBacklogRetirementSurvivesRestart` checks idle and failed flagged sessions, cleared persistence, zero dispatch and zero executor calls. |
| AC-2 | PASS review. `ListBacklogSessions` selects only flagged rows. `TestClosedBacklogRetirementSurvivesRestart` checks three subsequent ticks, reopens migrated on-disk SQLite, constructs and starts a fresh daemon, and verifies no further ordinary backlog GET or restored flag. |
| AC-3 | PASS review. `TestClosedBacklogReleasesAdmissionForOpenWork` admits three closed candidates ahead of one actionable open issue with one worker slot, checks every closed flag and direct GET, and requires exactly one dispatch in that tick. |
| AC-4 | PASS review. Lookup errors return before clearing; clear errors wrap the real store error and traverse existing synchronous release/rate-limit handling. `TestUncertainBacklogObservationRetainsRetry` covers unknown, missing, inaccessible, transport and rate-limit observations plus later admission. `TestFailedClosureClearRetainsReadableRetry` cancels after a successful HTTP lookup, forcing actual context-first persistence failure; it checks the clear error, readable flagged row, no dispatch, same-session retry and subsequent useful admission. |
| AC-5 | PASS review. Store SQL changes only needs_poll and updated_at. Restart test compares complete retained session fields and separate watermark. `TestClosureRetirementPreservesOwedAbandonedReport` compares the owed run unchanged before reconciliation, then confirms the abandoned report is delivered on the closed issue and acknowledged with the correct watermark. Existing `TestAnAbandonedRunIsReconciledEvenWhenItsIssueWasClosed` remains supporting coverage. |
| AC-6 | PASS review. `TestRetiredBacklogReopensWithoutReplayingConsumedComments` retains session identity and consumed watermark 401 during retirement, then discovers reopening and executes pending 402 with triggers exactly [401,402], watermark 402, and no additional backlog GET. |
| AC-7 | PASS review. Protocol comments describe clean-drain and explicit closure clearing, retry retention, repeated timestamp writes, and released capacity without a per-tick read promise. Existing clean-drain/bootstrap, failed bootstrap, unconsumed-comment and contended-lock tests in backlog_test.go remain intact. Existing backoff/deferral tests in dispatch_test.go and marker round-trip tests in store_test.go provide supporting regression coverage. |

## Architecture and aggregate scope verdict

CLEAN. Daemon policy stays in ordinary materialisation and SQL stays in store with the existing context-first accessor. The change adds no schema, interface, dependency, goroutine or agent execution. In-flight-before-capacity ordering and synchronous skip/error cleanup are intact. State, comment watermarks, reports and continuation/schedule policy retain their owners. Tests use existing HTTP/SQLite/executor fixtures and Quiesce/settle, with no sleeps or invented capacity observations.

Current APP_CONTEXT identifies the materialisation path correctly; CONC-15 and its enforcement references match the frozen story. HEAD scope consists of the intended production/test change, context updates and session documentation. Existing open-listing/closure races, independent report/discovery reads, and non-immediate reopening detection remain expressly outside the guarantee. No production or test edits were made by this reviewer.

## Verification handoff

These are current static-review findings, not a claim that this reviewer reran tests. The orchestrator owns fresh scoped test and aggregate gate execution and must bind their passing results to the same snapshot before publishing verification. Required gates remain make check and go test -race -cover ./.... Forge checkboxes and issue closure are factory-owned and are not proof or disproof of implementation completeness.

Suggested immutable evidence files: the four production/test story files; fixture_test.go, dispatch_test.go, daemon_test.go and internal/store/store_test.go for the evidence referenced above; this review; and the current APP_CONTEXT/APP_CONSTRAINTS documentation if included in story validation scope. Include any additional files directly cited by the orchestrator's acceptance evidence. Transport and mutable administrative status documents must not enter the implementation snapshot.
