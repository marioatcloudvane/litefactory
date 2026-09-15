# Engineering test report — run 0005

Frozen scope: US-001, US-002, US-004, US-005, US-006. US-003 deferred. No acceptance assessment or real provider acquisition was performed.

## Scoped executed evidence

| Scope | Result | Retained output |
| --- | --- | --- |
| US-001 registration/catalog and disabled runtime | Five functions / 19 leaf cases passed with race checking | us001-unit-check.log |
| US-002 Claude private config, exact authorization, native limits, cold/resume/fallback, errors/cancellation | 13 cases passed with race checking | us002-unit-check.log |
| US-004 conditional cards, mode/payload/snapshot semantics | Six tests plus 17 subtests passed with race checking | us004-guidance-check.log |
| US-004 valid real-service create/update baseline examples and US-005 lifecycle isolation | Five top-level tests / ten subtests passed plain and race; final race 23.564s | us005-lifecycle-final-race-check.log; us005-lifecycle-plain-check.log |
| US-005 live/terminal reconciliation and recovery | Scoped race command passed, 35.742s | reconciliation-check.log |
| US-006 capture handoff whitespace | git diff --check -- docs/agent-evidence-capture.md passed | Engineer report, no output |

Initial test compilation/fixture errors were corrected by specialists and their logs retained; they are not passing evidence. No production behavior bug was found by scoped specialists.

## Guidance measurements

Shared wording is 160 words / 1124 UTF-8 bytes. Per-mode and combined counts are retained in us004-guidance-metrics.md and us004-application-guidance-sizes.log, including payload-contract boundaries. Full-card renders meet editorial targets; conditional smaller catalogs intentionally omit irrelevant cards. Actual provider-rendered token measurements, billed token counts and measured savings are unavailable.

## Aggregate gates

First make check stopped at two pinned lint findings (QF1001 character predicate and ifElseChain test control flow); test phase did not execute. make-check.log preserves failure and sandbox lint-cache warnings. Fix cycle 1 corrected both without behavior change; targeted regression checks passed. Final rerun uses writable temporary GOLANGCI_LINT_CACHE, without changing sandbox policy.

Final build passed: build-final.log. Final make check formatting/vet/lint passed with zero lint issues; repository race-test phase is still running. Final outcome and immutable snapshot attribution will be appended below.

## Missing obligations

US-005 remains partial: Codex integration and dependent cross-host coverage are deferred with US-003. US-006 remains blocked: all four authentic working/Validate captures, provenance integration and historical replay are absent. Local fake-host tests do not establish real host interpretation, effective real-host Validate enforcement or before-exit receipt observation.

## Final aggregate outcome

NOT PASSED: make-check-final.log records timeout124 after580seconds. Narrow daemon-final-check.log and turn-final-check.log record timeout-driven test failures at480.721s/480.773s. The active tests had only recently started; diagnosis found cumulative suite duration, not a demonstrated regression. No complete aggregate pass is claimed. See BUG_ESCALATION.md.

Final scoped MCP/reconciliation evidence is refreshed separately in final-scoped-check.log and does not satisfy this missing aggregate gate.

## Final immutable scoped evidence

Final scoped race run passed: agent1.750s, harness4.396s, turn129.014s; output final-scoped-check.log. Final build passed. Snapshot `lf274-final-engineering-v2` retains27 final implementation/documentation files; manifest SHA256 `da5507e2b856cd2f80c20db8eee9cad8edcfe2b74d6a54924f2861a116d164af`. Capture: lf274-final-engineering-v2.json. Check IDs: lf274-final-scoped, lf274-build-final. These are scoped/build checks, not a complete make check.

US-001's affected unaccepted checkpoint was invalidated before refresh (request67feeeeef0bdaced2d591ef7a6600ece), then current scoped engineering was recorded with request87a3156d2a79e3f3fce751645246f565 (input valid, engineering recorded, acceptance not_applicable, effect pending). US-002 and US-004 remain bound to their unchanged per-story snapshots and original passing checks. No acceptance or publication success is claimed.

## Run 0006 — US-003 implementation and final tests

Frozen scope this run: US-003 (T-001/T-002), plus US-005's previously-blocked T-002 extension and T-003 aggregate gate. US-006 explicitly out of scope per owner instruction. No acceptance assessment performed. No submission-helper executable/descriptor was available in this run's invocation context, so checkpoints are recorded as session evidence here rather than via the JSON contribution helper (see notes.md for the explicit assumption).

### US-003 scoped executed evidence

| Scope | Result | Retained output |
| --- | --- | --- |
| T-001 production encoding (internal/codex/mcp.go, runner.go) | go build ./..., go vet ./..., go test ./internal/codex/... and go test ./internal/turn/... -run 'MCP' all passed; gofmt clean | reported inline by engineer, not separately logged |
| Phase 1.5 code review | ADVISORY_ONLY, 0 blocking findings | CODE_REVIEW.md |
| T-002 unit tests (internal/codex/mcp_test.go) | 12 functions / 21 subtests passed with race checking; full-package regression 76.6% coverage, 37.554s | reported inline by engineer; commands recorded in notes.md |

### US-005 scoped executed evidence (this run's extension)

| Scope | Result | Retained output |
| --- | --- | --- |
| T-002 extension: Codex cross-host lifecycle (internal/turn/mcp_codex_lifecycle_test.go) | 5 functions / 9 subtests passed with race checking, 19.442s | us005-codex-cross-host-check.log |
| Full internal/turn regression during T-002 extension | All new tests pass; one pre-existing unrelated SQLite busy-lock flake (TestStoryContractUpgradePreviewAndExplicitApply), confirmed pre-existing/unrelated via 3/3 clean isolated reruns | us005-codex-cross-host-full-turn-package.log |
| T-003 aggregate gate: fmt/vet/lint | Both clean, 0 lint issues | final-check-fmt-vet.log, final-check-lint.log |
| T-003 aggregate gate: full-repository `go test -race` | 21 bounded sequential groups (package bundles + daemon/turn split by test-name-prefix `-run` regex so every test function is covered exactly once), each within `-timeout 8m`; all 21 groups passed, tightest margin 364.0s (internal/store) | final-check-group-1.log through final-check-group-21.log |
| T-003 flake recheck | TestStoryContractUpgradePreviewAndExplicitApply re-run in group 20 with -v; passed cleanly, did not reappear | final-check-group-20.log |

### AC-008 coverage matrix — cross-host Codex additions this run

Enabled/disabled/absent setup, malformed/quoted values, catalog parity, native-tool preservation, server collision/isolation, concurrent attempts, cancellation, cleanup, resume/fallback: all covered for the Codex path by internal/codex/mcp_test.go (unit level) and internal/turn/mcp_codex_lifecycle_test.go (production-seam lifecycle level, real temp SQLite + real broker + fake Codex subprocess). Completion reconciliation coverage (AC-007) was already delivered intact in run 0005 and re-verified passing as part of the full internal/turn regression this run.

### Final aggregate outcome (this run)

PASSED, bounded. `make check`'s check contract (fmt + vet + lint + full-repository `-race` test) was executed in full, with identical rigor to an unbounded single invocation, split across 21 sequential Bash calls to respect the ~10-minute per-call ceiling that caused the prior run's timeouts (BUG_ESCALATION.md). No timeout was raised above the existing 8m bound; no `-race` was combined with a whole-repository invocation in a single call; no package or test function was skipped. Zero real test failures were found. Snapshot manifest SHA256 `4696a1b190dab4dc459f540ddf04eba4ad0ceb4b2ab952751089422842019663` over 33 implementation/documentation files (18 modified + 15 new), retained at lf274-final-check-t003-manifest.txt, binds this evidence to the worktree state as verified — not a commit.

### Missing obligations (unchanged, explicitly out of scope this run)

US-006 remains blocked: all four authentic working/Validate captures, provenance integration and historical replay are absent, per prior run's assessment. This run did not touch US-006 or attempt to satisfy it, per owner instruction.
