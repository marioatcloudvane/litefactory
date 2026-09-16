# Engineering test report — run 0014

Workflow version 2; frozen scope US-001 through US-006. This report records engineering execution, not acceptance review or live adoption.

| Story | Executed scoped verification | Evidence | Engineering receipt |
|---|---|---|---|
| US-001 | 148 passing test/subtest records; targeted race tests; 89.8% agentresult coverage; scoped vet/lint | US-001-unit-tests.log | c32c621bb6e429c86ffbfe87e61c6f93 |
| US-002 | Stable-ID update/projection race tests, schema parity, scoped pinned lint, formatting | US-002-unit-tests.log | 4f561c8df0ee2927892af4be45b85f5b |
| US-003 | Real temporary SQLite and service race tests; v2 compatibility regressions; scoped pinned lint, formatting | US-003-final-tests.log; US-003-store-tests.log; US-003-turn-tests.log | f6fc5e2929990702c6bb79cd41ec3170 |
| US-004 | Aggregate capture/store/worktree/recovery race tests with actual executed fixture output; both workflow versions; scoped pinned lint, formatting | US-004-final-tests.log; US-004-tests.log | b4a1edf7a2d71fc3258e7bddcb371758 |
| US-005 | Deterministic progress boundaries/retention/revocation race tests; 90.5% agentresult package coverage; scoped pinned lint, formatting | US-005-unit-tests.log | 3af2917d375d203d407cf04470d137f4 |
| US-006 | Focused source/rendering/all-exit/corpus-equivalence race tests and pinned lint passed; final aggregate gate blocked | US-006-focused-tests.log; aggregate-make-check-final.log | 0ce19b664e702c1fafaa756d19bc14ec (blocked contribution; engineering rejected) |

US-001 through US-005 receipts have valid input and recorded engineering. US-006 has valid input for a blocked contribution and engineering rejected. All have acceptance not applicable and remote effects pending. Full receipt bytes are retained as US-NNN-receipt.json. Each contribution captures exact files and manifest digest; it does not certify provider provenance.

## Aggregate gates

The final uninterrupted `make check` executed and exited 2. Formatting, vet and pinned golangci-lint v2.13.1 passed (0 issues). Every Go package except `internal/teamsource` passed its full race suite, including store, turn (1086.073s) and daemon (666.409s). Separate `go build ./...` passed; `gofmt -l .` returned no paths. No make build, daemon restart or dist writes occurred.

Four pre-existing installed-instruction parity tests failed:

- `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`
- `TestCodexSkills_SharedSourceParity`
- `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`
- `TestCodexSkills_AC003GuardClausesPreserved`

The specialist confirmed their test files and all relevant `.codex`, `.claude` and planning-contract source inputs are unchanged from HEAD `82bd0dea62dc10d286f037126bd7f6b845424b6c` using an empty `git diff --exit-code HEAD -- ...` (exit 0). The tests require literal handoff/source paragraphs absent from installed copies, and do not read the new finalization skill. Frozen US-006 excludes installed-tree changes; `.codex` is also read-only. No tests were weakened and no excluded tree changed. See `US-006-aggregate-blocker.md`.

An earlier 540-second wrapper interrupted the first aggregate invocation (exit 124); its output is retained as `aggregate-make-check.log`, never claimed passing. The final invocation removed that artificial total timeout and completed under the repository's own deadlines. `aggregate-make-check-final.log` is authoritative for the failed aggregate outcome.

The final 140-file source snapshot was rechecked unchanged against manifest digest `bc8f86a96484ca8ab61a8100d4c187ed25309fb6b7b6f63b737dacbd6c7e79da` and retained in `aggregate-final-snapshot.json` with exact selected revisions and failed gate attribution.

## Limits

SQLite tests use isolated temporary databases. Source rendering and byte totals establish offline behavior, not authentic late loading, live delivery, acceptance, publication or billed-token savings. No acceptance/code/architecture/approved-plan review was commissioned.

## Run 0015 — diagnosis only

No checks rerun. Analysis of retained aggregate failures identifies three instruction-text parity causes across four failing tests; see BUG_REPORT.md. Minimum current-test repair affects 18 installed instruction files. Prior failed aggregate evidence remains failed; US-006 remains blocked. US-001–US-005 remain read-only engineering-complete dependencies.


## Run 0018 — engineering complete

The nine Claude role files now contain the tested structured protocol handoff. Only the explicitly authorized Claude instruction repair and session evidence were changed in this run; prior Codex repairs and US-001–US-005 implementation were preserved.

Executed verification:

- Targeted cross-tree/Codex instruction parity with race detection: passed, exit 0.
- Focused finalization/source-rendering/exchange-corpus race tests: passed, exit 0; four top-level tests.
- Byte comparison: passed, exit 0; full 186,859 versus scoped 37,551 UTF-8 bytes for the pinned corpus, not billed-token savings.
- `go build ./...`: passed, exit 0.
- `gofmt -l .`: passed, exit 0, no paths.
- Native `make check`: passed, exit 0; pinned golangci-lint v2.13.1 reported 0 issues and 32 tested packages passed with race detection. Daemon took 636.105s; turn took 1052.262s. Cache hits are retained as reported by Go.

Two earlier aggregate wrappers timed out at 560 seconds and remain incomplete evidence, not failed tests or passes. The final command retained the repository's native timeout; writable temporary lint cache removed the earlier cache-write warnings.

Final snapshot `us006-run18-final`: 158 files, digest `de32b0012fac33c3ba532a83ef8f92259352f7953e8ed89ce4b91df07d463ecc`. All captured hashes were rechecked against the final worktree. Source history and earlier immutable story evidence were not overwritten.

US-006 revision `86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b` passed complete preflight with no diagnostics, then engineering was recorded under request `ca5c340c919238e6a385bad3debf2354`. Receipt dimensions: input valid; engineering recorded; acceptance not_applicable; remote effects pending; Plan not_applicable. Pending remote effects are not publication/save success.

All six stories are engineering complete; US-001–US-005 were read-only satisfied dependencies. No acceptance/code/architecture/approved-plan review, commit, push, daemon restart, dist write, live route adoption, or mode transition was performed. Factory save and remote effects remain factory-owned.

Evidence: `run-0018-claude-repair-report.md`, `run-0018-claude-parity.log`, `run-0018-focused-tests.md`, `run-0018-focused-tests.log`, `run-0018-byte-comparison.log`, `run-0018-build.log`, `run-0018-gofmt.log`, `run-0018-make-check-native.log`, `run-0018-make-check-native.exit`, `run-0018-final-manifest.json`, `run-0018-final-snapshot.json`, `run-0018-US-006-contribution.json`, `run-0018-US-006-preflight.json`, `run-0018-US-006-receipt.json`, `run-0018-receipt-summary.json`.
