# Run 0025 — explanation of retained run 0024 failures

Analysis-only. Inspected retained reports, aggregate output and current source; no new tests, production edits, forge access or runtime-state inspection. Findings below distinguish observed test output from source-based conclusions.

## Remaining failure: old callback-count expectation conflicts with the new fail-closed hold guard

**Affected test:** `TestStoppedResultSQLiteOutageRecoversWithoutRelaunch`, `internal/daemon/stopped_results_db_test.go:19`, assertion at line 70.

**Observed output:** `repair debt was hidden behind failed DB discovery: 0 attempts`.

**Executed evidence:** retained `AGGREGATE_RUN_0024.json`, batch `daemon-8`: normal exit 1, elapsed 41.880 seconds, not a timeout. The test itself failed after 0.75 seconds. Fourteen of 45 batches passed, one failed, 30 were not executed. These are batch counts, not counts of individual tests.

**Exact path:**

1. The test closes SQLite after an ordinary workflow executor has returned an observed stopped result. Its repair callback increments `repairs` before trying `FinishRun` against that closed database. This callback repairs persistence of the stopped workflow result; it is not an owner-approved doctor repair.
2. `Tick` still calls `retryStoppedResults` before normal database discovery (`internal/daemon/tick.go:139`). Discovery ordering is not the immediate cause.
3. `retryStoppedResults` now calls `guardedRecoveryAllowed` (`internal/daemon/stopped_results.go:76`). The wrapper first checks cached retry timing, then calls `doctorHeldSession` (`internal/daemon/report_recovery.go:237`).
4. `doctorHeldSession` calls `DB.DoctorRepairHeldSession`, which queries `doctor_held_session_owners` (`internal/daemon/doctor_repair_control.go:75`, `internal/store/doctor_repairs.go:245`). A closed database prevents proving that the target is free of a doctor repair hold.
5. The guard returns an error. `retryStoppedResults` calls `recoveryFailure` and continues without invoking the callback. `recoveryFailure` retains the recovery row and dirty flag in memory and advances its retry deadline (`internal/daemon/report_recovery.go:190`). Thus the callback counter remains zero instead of the test's expected two.
6. Merely removing that first guard would not fix the counter: `runDoctorGuardedWorker` checks the same durable hold again before calling the worker body (`internal/daemon/doctor_repair_control.go:27`). Both checks protect real write paths.

**What this proves:** the old test expectation that the persistence callback must run twice during a total database outage no longer matches the new hold-safety boundary. The failure message overstates the available evidence: it does not prove the debt was lost or hidden by discovery. The test aborts at line 70, before reopening SQLite and checking eventual completion, one report, watermark advancement and no executor relaunch. Those later guarantees remain unverified in this execution.

**Suggested minimal resolution:** preserve the fail-closed guards. Update the regression to distinguish a scheduled recovery attempt/failed hold check from an invoked persistence callback. During the closed-database interval, require zero callback effects, retained stopped-result ownership and recovery debt, and retry cadence that advances only on due ticks. Assert those through retained recovery records after reopening or focused package-internal tests. Preserve and execute the existing post-reopen checks: original run reported once, source consumed once, executor invoked once. Add/retain explicit held-target coverage showing that recovery stays paused while a real repair hold exists and resumes only after safe release. This is a suggested test-contract correction, not a verified fix or authority to weaken safety. If eventual recovery fails when exercised, fix that concrete implementation defect next.

Do not treat an unreadable hold table as “no hold,” bypass the worker guard, or simply delete the failing assertion without equivalent debt/cadence coverage. If callback invocation during outage must itself remain a requirement, the callback must instead be split into bookkeeping and guarded effect phases; that is a broader change than needed to preserve the user-visible recovery guarantees.

## Earlier aggregate regression: hold lookup bypassed retry cadence — already corrected

**Affected test:** `TestReportRecoverySQLiteOutageRetainsSourceFallbackAndFlushes`, `internal/daemon/report_recovery_db_test.go:156`.

**Observed output:** `outage ineligible ticks retried or alerted`.

The earlier hold query ran before cached next-eligible-time checks. Every tick during a database outage therefore recorded another error, even when no retry was due, causing premature retry/escalation. The saved correction moved hold checks behind cadence eligibility and to guarded production effect wrappers. The original test was unchanged and subsequently passed under race (5.769s); `Doctor|ReportRecovery` passed (28.819s), with zero pinned lint issues. See `IMPLEMENT_RUN_0024_RECOVERY_CADENCE_FIX.md`.

The initial full race run was bounded/interrupted at 538.719s with exit 130. It is not passing aggregate evidence, even though this identified regression subsequently passed scoped reruns.

## Earlier formatting gate — already corrected

Initial `make check` stopped at formatting for `internal/bugrepair/evidence_test.go`. A gofmt-only correction resolved it. Final build, formatting, vet and pinned lint passed. Formatting is a gate failure, not a failed behavioral Go test.

## Other resolved intermediate failures retained in specialist reports

The reports also record a rewind-to-zero SQLite comparison fixed with an integer cast; an in-progress migration0084 syntax error; a presentation-renderer file collision that was restored; fixture deadlines too short under race; Unix-socket path length and macOS `/tmp` symlink-root issues; an unwritable lint cache; and a CLI preflight ordering defect that inspected host status before rejecting disabled provider isolation. Those reports retain later scoped passes. They are distinct from the final outstanding daemon assertion; not every intermediate record identifies a precise test name, so none is invented here.

## How to verify the correction

Suggested commands below have **not** been executed in run 0025:

```sh
go test -race -timeout=8m ./internal/daemon -run '^TestStoppedResultSQLiteOutageRecoversWithoutRelaunch$' -count=1
go test -race -timeout=8m ./internal/daemon -run 'StoppedResult|ReportRecovery|Doctor' -count=1
make check
```

Also rerun the failed and unexecuted aggregate scopes using their exact retained commands, with fresh evidence against the resulting source snapshot; retain appropriate regression coverage for any changed production paths. No aggregate pass is currently established.

## Why run 0024 stopped, and separate operational gaps

The prior implement-orchestrator run consumed its two allowed aggregate fix/retest cycles on formatting and report cadence. That process limit, rather than a proven inability to repair the remaining test, stopped further work. An exit-zero agent process did not satisfy engineering completion because aggregate evidence still failed.

Native provider isolation gates remain disabled pending authentic provider observations. Authentic #406 capture and host lifecycle observations remain unverified. These are separate evidence/enablement gaps, not additional failed Go tests, and fixing the daemon assertion alone does not enable native doctor execution or prove live repair/restart behavior.
