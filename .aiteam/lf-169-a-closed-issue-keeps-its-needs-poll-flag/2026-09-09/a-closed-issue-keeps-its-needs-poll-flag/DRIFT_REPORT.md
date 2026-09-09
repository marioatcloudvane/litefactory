# Drift Report — a-closed-issue-keeps-its-needs-poll-flag
# Branch: lf/169-a-closed-issue-keeps-its-needs-poll-flag | Date: 2026-09-09
# Run: 0004 / 242

## Delivered as planned

- US-1/#179, frozen revision `96542e65fbc2199159a9698271747405c9d4c6801335da7f6a094708f3df6024`: explicit closed observations retire ordinary backlog through the existing store accessor; unknown/failed lookups retain retries; clear errors propagate through established admission cleanup.
- T-1.1: implemented in daemon materialisation; retained local state, watermarks, reporting obligations and discovery/reopening contracts.
- T-1.2: corrected daemon/store protocol and admission comments, including the remaining read-budget claim identified and resolved during review.
- T-1.3/T-1.4: six new regression functions cover closure, repeat ticks/restart, capacity, unknown/error/rate-limit observations, real clear failure with a readable flagged row, owed reports and reopening; existing drain/error/lock/deferral tests pass.
- T-1.5: scoped review and re-review CLEAN; no outstanding blockers or advisories.
- T-1.6: per-story verification event published immediately after scoped tests/review, before aggregate gates. Final formatting, vet, pinned lint and all-package race+coverage checks passed; environment adjustments and initial timeouts are recorded in notes/MANIFEST.

## Added scope

None. Session reports and targeted persistent-context updates follow the implementation workflow.

## Dropped or deferred

None from the selected story. Request budgets, schedule closure, forced reopening scans, historical deployment-counter reconciliation and stronger handling of closure after an open listing remain explicitly out of scope. The parent incident update does not override the factory-frozen approved revision.

## Architectural deviations

None. One test specialist owned both regression groups to avoid overlapping fixture writes. The clear-failure regression uses context cancellation after a successful closed HTTP response and reads the retained row with a live context; no SQL outside store, new abstraction or production hook was introduced.

## Verdict

CLEAN — delivered story matches the frozen scope; scoped and aggregate review/tests passed. Temporary gate wrappers changed only execution concurrency, time allowances, cache placement and added coverage; no checks were disabled and repository configuration was unchanged.
