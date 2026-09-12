# Test report — Implement run 0005

Fresh execution against merged HEAD 7d037854a135602e6200c178ecb1d18fc12db2a1. Existing implementation commit 57dbe11 was independently confirmed as an ancestor. No production/test code changed. Prior-run passing claims are not substituted for these executions.

| Check | Actual result | Evidence |
|---|---|---|
| go build ./... | PASS, exit 0 | run0005-build.log |
| US-001 focused race tests: evaluator, HTTP responses, real preservation, CLI preview, owner inventory | PASS: daemon 39.016s, CLI 1.776s, store 4.005s | run0005-us001-check.log, run0005-us001.md |
| US-001 structured logging unit race tests | PASS: 2 top-level tests, 0 failures; reused for US-002 AC-007 | run0005-us001-unit.log/.md, run0005-us002-unit.md |
| US-001 real Git guard fixtures | PASS: five safety subcases plain and race; 19.287s / 21.361s | run0005-us001-real-plain.log, run0005-us001-real-race.log, run0005-us001-real.md |
| US-002 lifecycle and exact-head/capability race selections | PASS: 12 top-level tests, 21 subcases, zero failures; daemon 118.213s, Git 9.512s | run0005-us002-real.log/.md |
| US-003 budget/help unit race selection | PASS: 3 test functions, 8 leaf cases, 0 failures | run0005-us003-unit.log/.md |
| US-003 real sweep lifecycle race selection | PASS: daemon 44.001s | run0005-us003-real.log/.md |

Only targeted -run selections were executed per story. No whole-package reruns were performed per story. Real dependencies are temporary Git/SQLite and controlled HTTP fixtures; no hosted availability or production deletion is claimed. Installed Git 2.50.1 (Apple Git-155) and actual expected-head rejection output are retained in US-002 evidence. The final real-sweep selection was executed by the orchestrator after the agent thread limit refused another specialist.

## Aggregate gate

One `make check` invocation: run0005-make-check.log. Formatting, vet and pinned lint passed; lint reports `0 issues`. Lint also emitted cache-write warnings because its default cache location is outside writable roots, without failing lint. The full race stage includes passing daemon (722.212s), CLI (339.753s), store (298.621s), Git (144.630s) and other package results. Final result: exit 2; only internal/teamsource failed. The turn package passed in 1113.534s. No subtree-timeout failure occurred. The single aggregate gate completed without being killed or restarted. Under the explicit owner exception the observed parity failures do not block engineering; the raw make check result remains FAILED, not PASS. See run0005-aggregate-result.json.

The owner explicitly waives only pre-existing internal/teamsource instruction-parity failures and TestRunTimeoutKillsTheWholeSubtree under parallel load. Those remain recorded failures if observed, not passing checks. Observed final result: four top-level teamsource parity tests and 23 failing subtest lines. No retirement scoped failure was observed. No attempt was made to fix protected instructions or chase the waived tests.

## Publication state

US-001 input valid, engineering recorded, acceptance not applicable, effects pending when observed. US-002/003 fresh scoped engineering is ready but factory evidence recording is blocked by recovery of the first US-002 request, which reused a run-global immutable check ID. Corrected payloads pass preflight. See run0005-submission-blocker.md for exact request correlation and safe recovery. Pending is not success. No acceptance review, factory save or remote-effect success is claimed.
