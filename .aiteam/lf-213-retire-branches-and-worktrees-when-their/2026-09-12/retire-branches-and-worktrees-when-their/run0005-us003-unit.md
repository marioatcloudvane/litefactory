# Run 0005 — US-003 targeted unit evidence

Story: US-003, issue #257, issue ID 5433203535, revision `e3c76f7481339931f6f0f4815a233c5a31fa2b058e5e364055ef1e59268a8f75`; dependency US-002. These are fresh executed engineering checks, not acceptance findings.

Command executed:

```sh
go test -race -count=1 -timeout=8m ./internal/daemon ./internal/cli -run '^(TestRetirementBudgetIssues_.*|TestRetirementHelp_ExplainsAutomaticAndPreviewBehavior)$'
```

Result: exit 0; both selected package runs passed. Selection contains 3 top-level test functions and 8 leaf cases; 0 failures. Output retained in `run0005-us003-unit.log`. No source or test changes made.

| Criterion | Fresh unit evidence | Bound |
|---|---|---|
| AC-001 | Not exercised by this unit selection | Scheduling and persistence require separate lifecycle checks. |
| AC-002 | `TestRetirementBudgetIssues_BackoffBoundary`: before, at, and after hold boundary; `TestRetirementBudgetIssues_PreservesFailures`: cancellation, deadline and forge error identity plus context propagation | Fake issue reader; no authentic hosted availability claim. |
| AC-003 | Not exercised by this unit selection | Durable summaries and incomplete-pass behavior require separate lifecycle checks. |
| AC-004 | `TestRetirementHelp_ExplainsAutomaticAndPreviewBehavior`: clean help describes daily retention and no-run fallback; retire help describes advisory checks and unchanged ownership/schedule | CLI help assertions only; operator documentation and manual execution behavior are outside this selection. |
| AC-005 | Backoff/cancellation subset above | Full lifecycle selection and aggregate gate remain parent-owned. |

Coverage percentage was not collected: the orchestrator assigned the exact command above, without `-cover`; no duplicate run was added merely to gather a percentage. No whole-package suites, production resource operations, or forge mutations were performed.
