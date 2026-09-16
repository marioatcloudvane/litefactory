# US-004 unit test evidence

Frozen story: #301 / issue 5463752986, parent 5439211198, revision `74d45a210089e9cf742806b410b67be17201d2dbfbfceca40f7899b61a7dde2b`, dependencies US-001 and US-003, workflow 2. Executed base HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus current uncommitted implementation and the test files below. This is agent-reported local execution, not acceptance or authentic host evidence.

## Changes

- `internal/submission/family_tools_test.go`: all-five-mode discovery/call matrix expects both advisory activity tools for Plan and Spike while keeping unrelated families restricted, including creation gate and evidence permissions.
- `internal/submission/family_catalog_test.go`: incremental installation matrix allows only the installed activity tool for Plan/Spike and preserves effect labels and denial of uninstalled families.
- `internal/turn/activity_lifecycle_unit_test.go`: missing factory time rejects read/terminal/clear atomically; permission material is versioned, distinguishes task/run/attempt, and remains stable across display cleanup. Inert store handles are used only for constructors and permission inspection; no store method or external I/O is exercised.

Reused existing `activity_unit_test.go` checks for exact retention boundary, immediate clearing, non-renewal, no reopening, defensive copies, concurrent copied views and empty state. Test-plan disposition: AC-001 pure profiles/material passed; AC-002 process-local lifecycle passed with production revocation assigned to I/O specialist; AC-003 advisory state isolation passed with durable retention assigned to I/O specialist; AC-004 deterministic lifecycle boundary passed with runtime scheduling/capacity assigned to I/O specialist. No TodoWrite tool is available; this records the per-criterion disposition.

## Final executed check

```
go test ./internal/submission ./internal/turn -run 'TestFamily(Profile|Catalog)_|TestActivityService_' -short -race -cover -count=1 -timeout=4m
ok  github.com/cloudvane/litefactory/internal/submission 1.469s coverage: 10.8% of statements
ok  github.com/cloudvane/litefactory/internal/turn 1.883s coverage: 0.4% of statements
```

Exit 0. Eleven selected top-level test functions passed, zero failed. Coverage percentages are package-wide for this narrow selection, not story or complete-package coverage.

```
gofmt -l internal/turn/activity_lifecycle_unit_test.go internal/submission/family_tools_test.go internal/submission/family_catalog_test.go
```

Exit 0, no output.

An initial run failed to compile because a drafted lifecycle test duplicated an existing function name; redundant drafted tests were removed in favor of the existing lifecycle suite. The next run passed (submission 10.8%, turn 0.3%); adding the permission-material test justified the final rerun above. No outstanding failure remains.

## Explicit I/O handoff and limits

Activity update/get, attempt guards and runtime composition use concrete SQLite store dependencies. Production catalog/digest/call agreement, repeated scoped reads, runner success/failure/cancellation/interruption, replacement/expiry, runtime capacity/shutdown and preservation of durable receipts/pending publication require the separately assigned I/O specialist. These unit checks do not prove those scenarios. No production files, installed instructions, forge records, runtime state, dist outputs or commits were changed; no full-package suite, repository suite, make check or provider invocation was executed here.
