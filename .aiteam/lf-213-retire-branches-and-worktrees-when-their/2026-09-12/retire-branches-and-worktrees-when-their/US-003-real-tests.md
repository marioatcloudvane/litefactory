# US-003 real dependency execution evidence

Frozen revision: e3c76f7481339931f6f0f4815a233c5a31fa2b058e5e364055ef1e59268a8f75. Dependency US-002 was supplied as engineering recorded.

Changed file: internal/daemon/retirement_sweep_real_test.go. No production edits.

Executed commands:

- `go test ./internal/daemon -run '^TestRetirement(SweepReal|ExecutionReal|RealIssueResponses)' -count=1 -v`: passed, 16 top-level tests plus 29 subtests. Exact output: US-003-real-plain.log.
- `go test ./internal/daemon -run '^TestRetirement(SweepReal|ExecutionReal|RealIssueResponses)' -count=1 -race`: passed. Exact output: US-003-real-race.log.
- `go test ./internal/cli -run '^TestTaskCleanReportsTheRetentionSetting$' -count=1 -v`: passed. Exact output: US-003-real-help.log.

Criterion mapping:

- AC-001: TestRetirementSweepRealRestartAndDailyBoundary exercises first pass, one-owner batching, real DB close/reopen, stable sweep identity/cursor and exact 24-hour completion boundary; TestRetirementSweepRealZeroCandidates covers empty passes.
- AC-002: TestRetirementSweepRealFailureFairness exercises HTTP 404 and durable task-lock contention without starving later owners; TestRetirementSweepRealBudgetAndBackoff checks fresh budget and elapsed backoff; TestRetirementSweepRealRateLimitPreservesCursor exercises real HTTP 429 and shared backoff; TestRetirementSweepRealShutdownDuringCloneContention uses actual clone gate contention and channel-controlled cancellation.
- AC-003: restart test counts six confirmed real Git removals and JSON completion summary; zero-candidate summary is checked; cancellation/storage-failure tests retain incomplete state and log incomplete summaries. TestRetirementSweepRealStorageFailure closes real SQLite from the HTTP observation boundary and verifies no checkout removal or completion claim.
- AC-004: existing updated TestTaskCleanReportsTheRetentionSetting passes. Documentation prose review is not claimed by this role.
- AC-005: preceding lifecycle cases execute production store/Git/HTTP paths with temporary real SQLite and bare-origin repositories, synthetic third-party HTTP responses and supplied daemon time. Prior executor real lifecycle/coordination/recovery and closure response tests were rerun because production execution paths changed.

Bounds: temporary local dependencies only, no hosted GitHub availability or production cleanup claim. No acceptance/review performed. Aggregate make check remains orchestrator responsibility. The initial fixture used an uninitialized clone gate and panicked; corrected to existing newCloneGate constructor before passing final plain/race runs. No unresolved production defect found.
