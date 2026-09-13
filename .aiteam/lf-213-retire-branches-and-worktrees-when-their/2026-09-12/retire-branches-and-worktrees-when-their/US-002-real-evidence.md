# US-002 executed real dependency evidence

Frozen story US-002, issue 256 (5433203226), revision 74f54d211ddd90390ea775cb7a7729a273c749aa6834059558afacb38ddefed9. Dependency US-001 is read-only satisfied engineering. This report is engineering execution, not acceptance or hosted delivery.

Files owned:
- internal/daemon/retirement_execution_real_test.go
- internal/git/retirement_guard_internal_test.go

Fixtures use migrated temporary SQLite, temporary real Git clones/worktrees and bare origins, and an in-process HTTP server through the production GitHub client. No production resources or external forge mutations. Synthetic HTTP closure does not establish hosted availability.

Criterion mapping:
- AC-001: lifecycle open/reopened-final/unavailable-final, owner lock, active session; coordination parent lock and blocked clone cancellation with canonical lock observed first; shared child rejection and active child blocks parent. HTTP assertions reject conditional requests and require exact individual issue URL.
- AC-002: lifecycle clean deletion, ignored/untracked/unpushed content, missing upstream, stale/absent registration; real Git safety mutation probes dirty/locked/detached/advanced/base. Clone dirty checkout/index/HEAD snapshots remain unchanged.
- AC-003: exact-head local race wrapper advances ref immediately before actual update-ref compare-delete; remote pre-push advances bare-origin ref after advertisement and actual receive-pack rejects expected old head. Actual Git version/rejection output retained. Remote-only cleanup exercised.
- AC-004: SQLite intent-write failure prevents effect, outcome-write failure preserves unknown intent and original path, missing confirmation retains uncertainty, confirmed retry clears exact original path. Direct API test rejects unknown outcome path clearing and protects replacement ownership from old intent.
- AC-005: confirmed-absent retry after lost confirmation; stale registered checkout removal; old pending intent cannot delete recreated worktree this pass; branch/session identity survives full/partial cleanup.
- AC-006: externally closed merged feature, not-planned abandonment, open merged issue, absent upstream, shared/active owners, local-only files/commits, full/partial cleanup, reopening via local branch, retained remote tip or fresh base. This treats closure reasons identically as required.
- AC-007: executor cases exercise logged results, but focused structured severity/field assertions are assigned to the unit-test engineer; this report does not duplicate those assertions.
- AC-008: dirty clone snapshots during real local/remote branch and worktree operations; protected base mutation refusal. Explicit capability-contract source changes and surface tests are engineer/orchestrator owned.

Plain command: `go test ./internal/daemon ./internal/git -run '^TestRetirement(ExecutionReal|ExactHeadRaces)' -count=1 -v`
Result: passed, 9 top-level tests, 0 failures. Exact output: US-002-real-plain.log.
Additional safety-probe plain and complete focused race checks will be recorded below after completion.

Additional plain command: `go test ./internal/git -run '^TestRetirementGuardRevalidatesSafety$' -count=1 -v`
Result: passed, 1 top-level test / 5 safety subcases, 0 failures. Output: US-002-real-safety-plain.log.

Race command: `go test -race ./internal/daemon ./internal/git -run '^TestRetirement(ExecutionReal|ExactHeadRaces|GuardRevalidatesSafety)' -count=1 -v`
Result: passed, 10 top-level tests / 31 leaf cases, 0 failures, no race detector findings. Exact output: US-002-real-race.log. All new test files are gofmt-clean. Aggregate make check remains orchestrator-owned.
