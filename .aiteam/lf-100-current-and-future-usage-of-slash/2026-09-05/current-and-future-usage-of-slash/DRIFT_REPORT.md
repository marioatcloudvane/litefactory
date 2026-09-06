# Drift Report — current-and-future-usage-of-slash
# Branch: lf/100-current-and-future-usage-of-slash | Date: 2026-09-05

## Delivered as planned

- T-100.1: retired slash commands now take the ordinary unknown-command path;
  `/schedule` has a factory-only command boundary and strict pure parser for
  direct, absolute, dependency, proposal, acceptance, and rejection forms.
- T-100.2: embedded SQLite migrations and store accessors persist schedules,
  dependencies, proposals, replay keys, reservation, terminal state, and the
  pending-schedule count.
- T-100.3: authorized schedule controls persist before an immediate structured
  reply and source-comment consumption, allowing later issue comments through.
- T-100.4: normal daemon ticks dispatch eligible work under existing capacity,
  propagate dependency outcomes, recover orphaned reservations, and log
  `scheduled` beside existing pool metrics.
- T-100.5: bounded structured proposal adapters for configured Claude/Codex
  agents are wired into the factory; proposals require owner/issue-scoped
  ID-based acceptance and cannot directly schedule or dispatch work.
- T-100.6: parser, store, daemon, report, proposal, replay, and harness tests
  were expanded or repaired. `go test ./... -race` passed across 23 packages.

## Added scope

- Added a Darwin test-only preflight that skips process-tree fixture assertions
  only when the sandbox explicitly denies process-list access. This was needed
  to make unrelated harness fixtures reliable in the current execution
  environment; production process discovery and termination were unchanged.
- Added private durable scheduled-run trigger identities and abandoned
  reservation recovery to resolve code-review findings required for safe
  multi-item accepted proposals and crash recovery.

## Dropped or deferred

- External integration tests were skipped because `TEST_BASE_URL` and
  `TEST_DATABASE_URL` were unset. Durable persistence is covered by local real
  SQLite tests; no external target was invented.
- `.aiteam/APP_CONTEXT.md` and `.aiteam/APP_CONSTRAINTS.md` were not updated:
  the required context-updater specialist failed to start due to its usage
  limit. This is a documentation handoff only; it does not affect compiled
  behavior or test results.

## Architectural deviations

- None in product architecture. The proposal capability was implemented as a
  separate bounded, tool-free/read-only adapter rather than reusing ordinary
  workflow execution, as required by review.

## Verdict

CLEAN — delivered matches the plan, with the documented external-integration
skip and deferred context-document refresh.
