# Engineering test report — run 0007

Status: partial; aggregate verification in progress. No acceptance review was performed.

## Scoped executed checks

| Story | Retained executed evidence |
|---|---|
| US-001 | 6 new unit cases and 15 selected persistence/runner functions passed, including race. US-001-UNIT-TESTS.md, US-001-io.md and logs. |
| US-002 | 16 unit cases and 13 top-level persistence/race functions passed. US-002-unit-tests.md and US-002-io-tests.md. |
| US-003 | 32 unit cases, 44 publication/runtime cases, and 3 daemon intake/recovery tests passed, including race. US-003 unit/I/O/daemon reports. |
| US-004 | 11 unit functions and 9 runtime/persistence tests passed with race. US-004-unit-tests.md and US-004-io-tests.md. |
| US-005 | 12 focused source/render/auxiliary tests passed with race; exchange fixture was later refreshed and all 5 scenarios passed. US-005-unit-tests.md, aggregate-fix2-tests.md. |
| US-006 | Eight source/ownership assertions and scoped Claude temporary-render/manifest race checks passed. US-006-engineering.md. |
| US-007 | Not executed: requires unavailable Claude harness and protected Codex writes. |
| US-008 | 14 pure tests and 13 real git/SQLite/HTTP tests passed plain/race. US-008-unit-tests.md and US-008-io-tests.md. |
| US-009 | 20 retained compatibility tests passed plain/race; no live-cohort/drain evidence and no reader deletion. US-009-compatibility-tests.md. |
| US-010 | Incomplete: installed Codex parity and gated retirement dependencies unmet. |

Counts above describe each scoped selection and can overlap existing regressions; they are not a summed unique repository test count.

## Aggregate execution and fixes

- Initial make check stopped at 13 lint issues. aggregate-make-check.log.
- Fix cycle 1 resolved production/test style findings; scoped tests and vet passed. aggregate-fix1-engineering.md.
- make check retest 1 passed vet and pinned golangci-lint v2.13.1 with zero issues, then found outdated exchange fixture pins. Its unbounded race target was terminated at the orchestrator's 480-second gate bound; this is incomplete, not passing evidence. aggregate-make-check-retest1.log.
- Fix cycle 2 refreshed test-owned fixture hashes/totals without weakening equivalence assertions; five scenarios passed with race. Companion byte-cost documentation consistency assertions passed. aggregate-fix2-tests.md and aggregate-fix2-docs.md.
- go build ./... passed. gofmt -l . returned no files. aggregate-build-format.log.
- Bounded full-package plain checks are running separately for turn, daemon, CLI and remaining packages. No aggregate pass is claimed until results are retained.

## Evidence limits

Offline synthetic/local dependencies establish repository behavior only. Authentic working/Validate host observations remain unsatisfied external #274/#296 obligations. Fixture compatibility cannot establish live runtime drain. No dist build, runtime-state edit, forge call, provider launch, acceptance decision or commit occurred. Remote-effect pending receipts are not success. Current snapshots changed during aggregate fixes; affected engineering checkpoints were invalidated pending final-state verification, preserving historical executed evidence.

## Final aggregate outcomes

- CLI full plain suite: passed, 68.144s.
- Daemon full plain suite: passed, 265.359s.
- Other packages: all reported packages passed except teamsource instruction-contract checks. Agent-result package passed after fixture refresh. Exact package list/output: aggregate-other-packages.txt, aggregate-other-plain.log.
- Turn full plain suite: failed native lifecycle/family catalog checks, then timed out after 8m (481.080s); many parallel tests had not run. Exact output: aggregate-turn-plain.log.
- make check: NOT PASSED. Initial lint defects were fixed; retest vet/lint passed, but race execution reported the stale fixture and was interrupted at 480 seconds. The fixture is now corrected and targeted race verification passed; remaining aggregate failures and timeout prevent success.
- Two fix cycles exhausted; BUG_ESCALATION.md records exact next work. No later test results are inferred.

Final implementation state digest: `25986981b6304e1bd99c25a4ade586bf7f3164bf359ad24768bcda2ee97d1db8`; base `16634ed33075ea3acfc449bcb6316163bdc0b807` plus uncommitted changes. Environment: go version go1.27.0 darwin/arm64. Final source hashes: FINAL_IMPLEMENTATION_STATE.json.

All seven previously engineering-recorded changed stories (US-001 through US-006 and US-008) were invalidated pending final-state repair/verification; their earlier passing snapshots remain historical. US-007/009/010 are blocked. Input receipts are valid; engineering latest states are rejected for invalidated/blocked contributions; acceptance is not_applicable and remote effects remain pending as observed. No all-feature engineering completion is claimed.


## Implement run 0015 — script verification

The script compiled and ran read-only against the live database, exiting 2 because two runtime ledgers are absent (go run reports its wrapper exit 1). The JSON retains partial findings and explicit errors; this is evidence of incompletion, not a passing drain check.

Four offline SQLite fixture tests passed in plain and race runs, with 64.9% script coverage: missing DB without creation, unmigrated DB rejection, empty inventory with retained gates, and legacy identity/uncertain-delivery preservation without report contents. See US-009-script-test-report.md for exact commands and output. Pure unit tests were not applicable to the concrete SQLite interface; no artificial product abstraction was added. gofmt -l . passed with no output.

US-009 blocked contribution passed preflight and was submitted with request ff41e9a8c04bd7ccba92df44dbcf0922, receipt 163. Receipt lookup: input valid, engineering rejected, acceptance not_applicable, remote effect pending. No successful story completion is claimed.


Run 0015 aggregate gate: make check exceeded its 480-second outer bound (recorded exit 124). Vet and pinned golangci-lint v2.13.1 passed; full race test suite did not complete. No aggregate pass is claimed. See run-0015-make-check.log and run-0015-make-check.exit. The current script-only scope provides no evidence justifying unrelated product repairs.

## Implement run 0017 — script verification

Base HEAD: 16634ed33075ea3acfc449bcb6316163bdc0b807 with prior uncommitted work preserved. Exact delivered file hashes: run-0017-script-snapshot.json. The script test specialist executed 10 Python unittest cases: 10 passed, 0 failed (US-009-run-0017-script-tests.txt). Coverage includes both fixed target wrappers, stale/mismatched evidence and changed binary rejection, preview/apply guards, factory refusals, apply failure/retry, retained observations, private permissions, and paths with spaces. All CLI behavior was mocked with temporary directories; no live forge or migration proof. Shell syntax, Python syntax and help checks passed. gofmt -l . exited zero with no files (run-0017-gofmt.log). Aggregate gate result follows separately.

Aggregate run 0017: make check passed formatting, go vet and pinned golangci-lint v2.13.1 (0 issues), then its full race suite exceeded the 360-second bound and was terminated (process return -15). Therefore make check did not pass; no aggregate engineering completion is claimed. Logs: run-0017-make-check.log and run-0017-make-check.exit. No retry or unrelated code fixes were attempted for this narrowed scripting deliverable.
