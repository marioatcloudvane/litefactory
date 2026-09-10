# Engineering test report — task-branch reconciliation

Run 0006, workflow 2. These are agent-reported executed engineering checks; acceptance and factory save are separate.

## US-001 — #234

Frozen revision `b9d05084584db4b5d68eac608a029bc31b89c6b81f944aceabe535ead5952075`.

Final focused race/coverage check passed: 32 cases, zero failures. Full exact command, output and exit status retained at `/private/tmp/lf209-us001-tests-final-fixed.log` for immutable factory capture. Packages: internal/turn, internal/store, internal/report. Focused coverage: turn 15.4%, store 7.3%, report 14.6%.

- AC-001: FF/merge/equal/ahead matrix, preserved parents, reused/recreated/resumed worktrees and task-before-base-before-runner ordering.
- AC-002: absent and remote-only task branches, narrow fetch coverage, unavailable origin refusing stale cached state.
- AC-003: dirty/conflict/unrelated/reserved/index-lock preservation, mandatory runner refusal regardless of permissive base policy, normal dirty recovery when task state is equal.
- AC-004: independent phase/ref/commit observations, store round trips, historical base-only migration, prelaunch reporting without run allocation.
- AC-005: managed Validate freshness observation preserves retained snapshot and acceptance.

Existing broader Git/store/report suites passed. Earlier broad turn run exposed base-refusal wording regression; production now preserves the legacy base opening and appends phase detail. The final focused run includes that regression test and passed. Earlier failures are retained, not represented as passing checks.

Environment: Go 1.27.0 darwin/arm64; Git 2.50.1 (Apple Git-155). Deterministic local Git/bare-origin and SQLite fixtures with fake runners; no live external services or nested agents.

## US-002 — #235

Frozen revision `71359f25e7e30424c1af3d99f9bb8cf9f1730472646522507c5a0cc5ce220023`.

All final scoped checks passed with `-race -cover -count=1`. Exact commands, outputs, exit codes, check IDs and criterion mappings are retained at `/private/tmp/lf209-us002-tests-evidence.json` and its referenced logs for factory capture.

- Turn task/conflict/apply suite: 119.659s, 9.3% scoped package coverage.
- Additional task deletion freshness: 28.737s, 5.7%.
- Refined exact task application while remaining base work is deferred: 14.697s, 7.4%.
- Store proposal history/binding/replay suite: 9.482s, 6.8%.
- Git region/raw-byte/staging/restoration suite: 56.089s, 59.0%.

AC-006 covers task-labelled exact-input previews, redaction, advisor fallback and no mutation before owner approval. AC-007 covers both-target HEAD/base/task changes, absent→present/present→absent, historical unbound proposals and persistence/replay isolation. AC-008 covers exact approved bytes/parents, deferred base work, numeric owner/session/expiry/consumption, dirty/tampered/excluded preservation, raw-byte restoration and retained commit evidence after publication failure.

An initial invalid store test fixture was corrected; original failing logs remain retained. No production defect was found by the dedicated new criterion tests.

## Aggregate result

Final `make check` passed (exit 0): formatting, `go vet ./...`, pinned golangci-lint v2.13.1 with zero issues, and `go test -race -timeout=20m ./...`. All 31 packages with tests passed; turn took 1001.811s. Exact output `/private/tmp/lf209-aggregate-make-check-fixed.log`. The cache used `/private/tmp/lf209-golangci-cache` because the default linter cache is outside sandbox write permissions.

Final `go build ./...` passed (exit 0), output `/private/tmp/lf209-aggregate-build-fixed.log`.

The first aggregate attempt stopped on two gocritic style findings (harvest if/else chain and prepare single-case switch). Responsible engineers made equivalent control-flow style changes; the final aggregate above passes on those exact files. Earlier failed output is retained in `/private/tmp/lf209-aggregate-make-check.log`.

All three final engineering submissions are recorded against the exact final 33-file source/test manifest SHA256 `304fcfff0479400e63200c001f473e043286b177860cecd9695887fae17c5b2d`. Each story retains its own immutable snapshot/check attribution using the actual shared aggregate execution outputs:

| Story | Snapshot | Check IDs | Request |
|---|---|---|---|
| US-001 | `lf209-final-328` | `lf209-aggregate-make-check`, `lf209-aggregate-build` | `e6830b243f4749c6fbf048a1f6f676a1` |
| US-002 | `lf209-final-328-us-002` | `lf209-aggregate-make-check-us-002`, `lf209-aggregate-build-us-002` | `fce3a4fca0dd1968a090dfbf5a654e00` |
| US-003 | `lf209-final-328-us-003` | `lf209-aggregate-make-check-us-003`, `lf209-aggregate-build-us-003` | `b49ac590b0dd87e024cdc6901fd0ffad` |

All final receipts: input valid, engineering recorded, acceptance not applicable, remote effects pending. Earlier engineering was explicitly invalidated before refresh because dependent edits/final style fixes affected its captured files; historical acceptance was untouched. The aggregate commands were executed once on the final files, not rerun for each receipt. Factory save and acceptance remain separate; no publication success is inferred from these checks.

## US-003 — #236

Frozen revision `066aad90a779c5d8177dcef931bcddb2210f590e553cf3e474d87f3dfb6adddf`.

Fourteen final cases passed, zero failed: `go test ./internal/turn ./internal/report -run '^TestTaskPublication' -race -cover -count=1 -v`. Exact output `/private/tmp/lf209-us003-tests-final.log`; criterion map `/private/tmp/lf209-us003-tests-evidence.json`. Focused coverage turn 11.5%, report 22.1%.

AC-009 covers preparation-only/prior-retained publication and no-op/equal/FF behavior. AC-010 covers failed-run local preservation/recovery and truthful rejected publication. AC-011 covers deterministic actual second-writer movement during execution and during push, identical-tip completion without changing checked tree, generic push rejection versus verified movement and report propagation.

Initial fixture failures (duplicate trigger identities, disabled base synchronization, inherited Git hook environment) were corrected; original failed logs remain retained. Dedicated criterion tests required no production fixes.
