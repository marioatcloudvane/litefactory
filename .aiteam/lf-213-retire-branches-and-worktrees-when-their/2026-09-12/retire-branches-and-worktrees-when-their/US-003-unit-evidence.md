# US-003 unit engineering evidence

Frozen revision: e3c76f7481339931f6f0f4815a233c5a31fa2b058e5e364055ef1e59268a8f75

Executed:

```text
go test ./internal/daemon ./internal/cli -run 'TestRetirementBudgetIssues_|TestRetirementHelp_|TestRetirementIssueClosed_Identity|TestEvaluateRetirement_ClosureAndEvidence' -short -race -cover
ok  github.com/cloudvane/litefactory/internal/daemon 2.340s coverage: 3.2% of statements
ok  github.com/cloudvane/litefactory/internal/cli 1.886s coverage: 5.5% of statements
exit 0
```

Five selected test functions passed, zero failed. Three new functions contain eight named cases; two existing functions were rerun following checked-type-assertion lint fixes.

- AC-002/AC-005 (partial): TestRetirementBudgetIssues_BackoffBoundary uses a fake issue reader and supplied time to show worker-armed backoff blocks closure evidence, then allows observation at and after the exact boundary.
- AC-002 (partial): TestRetirementBudgetIssues_PreservesFailures shows canceled/deadline/forge errors remain distinguishable from closed issue evidence and caller context is forwarded.
- AC-004 (partial): TestRetirementHelp_ExplainsAutomaticAndPreviewBehavior executes in-memory help routing for active daily retention, completed-run/creation fallback, advisory preview, ownership and schedule preservation.
- AC-001/AC-003 and remaining AC-005 require persisted batch state/log outcomes. Concrete *store.Store prevents fake-only lifecycle checks; real SQLite coverage is delegated to the real-dependency specialist. No new production seam was introduced.

Modified tests:
- internal/daemon/retirement_sweep_unit_test.go (new)
- internal/cli/retirement_help_unit_test.go (new)
- internal/cli/cmd_task_test.go (updated obsolete manual-clean retention message assertion; real-dependency test not executed by this unit role)
- internal/daemon/retirement_unit_test.go (two checked type assertions; relevant functions rerun)
- internal/daemon/retirement_real_test.go (GET constant changed to http.MethodGet; real test execution left to real-dependency specialist)

These tests establish in-memory behavior only, not hosted operation or production retirement. Aggregate make check remains the orchestrator responsibility.
