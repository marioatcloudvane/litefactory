# US-5 scoped test results

Frozen story: github:marioatcloudvane/litefactory#165, issue_id 5390504842, revision 648e41fb2a5d88d459d29ac442ca2be66a5333574c47373ef930e64e3da1d627; dependencies US-3, US-4. Tests derive from the selected inline acceptance criteria. Synthetic provider fixtures use future reset timestamps and supported diagnostic forms; they are not additional authentic provider captures. All git, SQLite and filesystem effects are isolated fixtures; HTTP uses the existing httptest forge and provider binaries are shell fakes exercised through the actual adapters.

## Final scoped command

`go test -race -cover ./internal/store ./internal/daemon -run '^TestRecoveryChain|^TestRecoveryLifecycleBothRealCLIRunners$' -timeout 3m`

Exit 0. Store: PASS, 10.343s, 30.2% scoped statement coverage. Daemon: PASS, 14.046s, 35.1% scoped statement coverage. Five top-level tests pass, including four restart fault cases, four semantic terminal cases, and three real-runner lifecycle cases. No race reports.

## Criterion evidence

- AC-5.1: `TestRecoveryChainRestartFaultMatrix` closes/reopens real SQLite and invokes startup reconciliation after pending intent, no-run reservation, allocated abandoned run, and durable fresh quota completion. Source executions are never reallocated; pending report debt survives and reporting preserves semantic interruption. `TestRecoveryLifecycleBothRealCLIRunners` replaces the daemon after a lost source report and confirms replay without rerunning the source.
- AC-5.2: `TestRecoveryChainThreeActualInvocations` verifies reservations/requeues and duplicate completion/report handling do not add allocations, exactly root plus three automatic runs exist, a fourth fresh continuation is refused transactionally, and exhaustion leaves no pending action. The real Claude adapter→turn→daemon repeated-limit case performs three automatic CLI invocations, receives newly classified quota evidence each time, then asserts a visible three-invocation/manual-recovery message and no fourth action or later invocation.
- AC-5.3: Restart and capped-chain tests retain the same original scheduled prerequisite in suspended state while pending/fresh recovery remains viable. Child due times remain absent through restarts and intermediate verified interruptions.
- AC-5.4: `TestRecoveryChainTerminalSettlement` verifies success, failure, cancellation and exit-zero interruption lacking recoverable context. Reporting and restart cannot turn interruption into success. Final success releases from the final run's completion time; failures/cancellation block descendants. `TestRecoveryChainMultipleOriginalPrerequisites` keeps a dependent waiting for both original prerequisite identities, then releases using the later prerequisite completion plus its configured delay.
- AC-5.5: The SQLite fault matrix covers reservation/allocation boundaries, report debt, duplicate completion, repeated limits and multiple prerequisites. The httptest-forge lifecycle covers lost reporting, valid overdue admission and no duplicate invocation, using real adapters and turns with fake CLI subprocesses. Channel-aware daemon Quiesce and subsequent ticks synchronize assertions; no sleep/poll loop is used.
- AC-5.6: Both Claude and Codex pass their actual CLI runner→turn→daemon normal lifecycle: quota interruption persists a future action and explicit conversation, partial files remain uncommitted, failed reporting survives daemon replacement, no early run occurs, the original conversation is explicitly resumed, successful continuation saves work, semantic chain settlement completes, and later ticks allocate nothing. Full repository regression/build/check gates remain orchestrator-owned and pending; this scoped report does not claim them passed.

## Files and snapshot dependencies

New validation files: `internal/store/continuation_settlement_test.go`, `internal/daemon/continuation_lifecycle_test.go`.

Existing helper dependencies include `internal/store/continuations_test.go` (`recoveryIntent`), `internal/store/store_test.go` (real SQLite/session/run fixture), and `internal/daemon/fixture_test.go` (httptest forge, daemon dependencies, injected clock, Quiesce-driven settling). The daemon lifecycle uses production `daemon.NewExecutor`, both real CLI adapters, `turn.Execute`, store migrations and scheduler APIs. The final implementation snapshot should include these implementation/helper dependencies alongside the new validation files according to the orchestrator's scoped manifest.

## Coverage contract

Makefile `make check` enforces formatting, vet, pinned lint and `go test -race ./...`; no coverage threshold or coverage target is defined there. APP_CONSTRAINTS, the available GitHub workflow and implement-orchestrator skill define no numerical minimum. The go-unit-test-engineer skill requires `-race -cover` on scoped runs for visibility; the final command complies. Focused package percentages above are not aggregate repository coverage.
