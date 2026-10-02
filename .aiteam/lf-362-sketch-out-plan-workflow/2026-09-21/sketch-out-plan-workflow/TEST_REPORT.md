# Implement engineering test report — run 0007

Status: all three selected stories implemented; scoped checks and final aggregate checks passed. Acceptance/code/architecture/plan review was not performed.

## Final aggregate

`GOLANGCI_LINT_CACHE=/tmp/lf362-lint-cache make check` exited 0. Formatting, vet, pinned golangci-lint v2.13.1 (zero issues), and the complete repository race suite passed. Actual output is retained in `MAKE_CHECK.log`. The final run used valid Go test-cache results for unchanged packages; newly executed heavy packages included CLI 304.183s, daemon 751.123s, store 497.387s and turn 1938.354s.

`go build ./...` exited 0 with no output. `gofmt -l .` exited 0 and reported no files. No dist build or daemon restart occurred.

Aggregate snapshot reference: `run-0007-aggregate-final-1`; check IDs: `aggregate-make-check-final`, `aggregate-build-final`, `aggregate-format-final`. The factory capture includes all final changed paths, including these artifacts. Input acknowledgment, engineering, acceptance, remote effects and task-branch save remain separate states.

## Final scoped evidence

Actual final activity, wiring and compatibility output is retained in `SCOPED_CHECKS.log`.

- Activity final-byte selection: planbrief 0.638s, report 0.390s, graphexec 0.769s, turn 2.763s, store 1.210s, github 1.098s; all passed. Includes draft/stage/render/schema, migrated SQLite recovery, controlled HTTP mutations, ordinal persistence and closed inventories. Snapshot `run-0007-activity-final-3`, check `activity-final-scoped-3`, request `377704221a01d1ff7deb5b73854beffb`.
- Package: six test functions / twenty-two subcases passed with race and coverage (teamsource 1.910s, 13.3%). Covers the twelve-resource closure, digests and exact source/embedded consistency, invalid resources/controls, bounded approval gates, both in-memory native harnesses, neutral guidance applicability/reuse and Research preservation. Snapshot `run-0007-package-final-1`, check `package-contract-race`, request `37ae4d398885c362b8f289c8308d72d6`.
- Wiring final-byte selection: planbrief 0.339s, graphexec 0.480s, store 3.579s, turn 51.480s, daemon 2.071s; all passed. Covers independent progress, exact waits/replies/upstream bindings, invalidation, owner edits, retirement, restart, partial publication and failed-save recovery using real temporary SQLite/HTTP/git fixtures.
- All previously failing legacy targets and immutable route-admission regressions passed with race: turn 124.543s, store 3.620s. Exact closed caller inventory passed 2.821s. Legacy fixtures explicitly retain legacy admission; new graph fixtures and production graph defaults are unchanged.
- Wiring snapshot `run-0007-wiring-final-1`, checks `wiring-final-scoped-1` and `wiring-legacy-race-1`, request `59a2b581e78f0d2af5791ae044eec9f2`.

Final activity and wiring source snapshot digest: `d022af95a60f6949c3ae90492b3ed9841a2fb75c9026317be475a6062229adc8` (86 implementation/documentation/test paths). Package digest: `afdd77696a4dfcc3db737463efd5e4807c6601eb546246a844373612a57856d2`. Story preflights were valid and complete; matching receipts recorded engineering, left acceptance not applicable and reported remote effects pending when observed. No remote completion is inferred.

## Corrected failures and retained history

Scoped tests found and resolved renderer metadata assumptions, hidden identity edits, missing graph managed-feature initialization, repeated presentation-edit correlation, unchecked upstream references, child/parent cursor leakage, locked-child input loss and interleaved retirement correlation. Explicit failing regressions were retained and rerun. Activity snapshots were invalidated before affected later changes; the final reference above supersedes intermediate engineering references without altering historical acceptance.

Aggregate fix cycle one resolved lint findings; actual diagnostics are retained in `MAKE_CHECK_lint_diagnostics.log`. Cycle two corrected closed graph/caller inventories and explicitly admitted supported legacy fixtures; the complete earlier failed race output is retained in `MAKE_CHECK_attempt2.log`. The final `MAKE_CHECK.log` is passing. The required exact visible heading remains unchanged, with a documented local spelling exception and independent golden fixtures.

All tests are offline fixtures. They do not establish authentic hosted mutations or Claude/Codex provider observations. Commit/push and task-branch save are owned by the factory and are not claimed here.
