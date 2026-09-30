# Aggregate fix cycle 2: report-recovery outage cadence

Observed aggregate failure: `TestReportRecoverySQLiteOutageRetainsSourceFallbackAndFlushes`, assertion "outage ineligible ticks retried or alerted". The aggregate race run was interrupted at its bound and is not passing evidence.

Root cause: the doctor repair-hold database query had been inserted at the beginning of the generic `recoveryAllowed` helper, ahead of the cached next-eligible deadline. During a SQLite outage, each otherwise ineligible tick returned another database error, and `reportAttempt` recorded another failure/escalation. It also prevented the generic outage callback from reaching its existing fallback behavior.

Fix: retain generic cadence/debt behavior in `recoveryAllowed` / `reportAttempt`, and use explicit guarded wrappers at every production reporting/recovery effect callsite. Cached ineligible work is skipped before a hold lookup. Due work checks the durable repair hold before effects; a database error fails closed and records one due failure. An active repair hold returns pending without falsely resolving or incrementing report debt. All production references to the unguarded helpers are now confined to the wrappers and cadence implementation itself.

Production files changed in this fix:

- internal/daemon/report_recovery.go
- internal/daemon/stopped_results.go
- internal/daemon/continuation_cancellations.go
- internal/daemon/continuation_reports.go
- internal/daemon/creation_presentation.go
- internal/daemon/mcp_creation.go

New tests: `internal/daemon/report_recovery_doctor_test.go` verifies that a closed database prevents the external effect while preserving cached retry cadence, and that an actual persisted repair hold blocks reporting without changing debt. The preexisting failing test was not altered.

Executed checks:

```
go test -race -timeout=8m ./internal/daemon -run TestReportRecoverySQLiteOutageRetainsSourceFallbackAndFlushes -count=1
ok github.com/cloudvane/litefactory/internal/daemon 5.769s

go test -race -timeout=8m ./internal/daemon -run 'Doctor|ReportRecovery' -count=1
ok github.com/cloudvane/litefactory/internal/daemon 28.819s

GOLANGCI_LINT_CACHE=/private/tmp/litefactory-doctor-lint /Users/mariomh/.local/bin/golangci-lint run ./internal/daemon
0 issues.
```

No full suite or make check was run by this specialist. No external provider, forge, runtime-state or lifecycle effects were performed. The orchestrator owns the final bounded aggregate rerun and retained completion evidence.
