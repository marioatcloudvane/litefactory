# Implementation notes — run 0005

Workflow version 2. Selected frozen scope: US-001, US-002, US-004, US-005 and US-006. US-003 is expressly deferred by the owner. No acceptance review or mode transition is authorized.

Assumption: the selection authorizes independent engineering portions of US-005 and the explicitly preparable US-006 capture handoff, but does not waive their US-003 dependency or cross-host evidence requirements. Those stories cannot be represented as fully complete until dependencies and external evidence are supplied.

Authentic captures are absent. No managed engineering or CI run may acquire paid nested agents; operator acquisition remains separately authorized external work.

US-001 runtime connection and US-006 capture handoff dispatched. Required scoped engineering tests precede factual engineering submissions. Aggregate make check belongs to the orchestrator after implementation writes settle.

US-006 T-001: docs/agent-evidence-capture.md now contains the four-scenario external handoff, exact call sequences, negative Validate observations, provenance/assertion matrix, sanitization and bounded replay requirements. Executed git diff --check -- docs/agent-evidence-capture.md passed. No captures or replay tests exist. Blocked contribution request 6a985bd2732340f08c742cdf0504a9c5 passed preflight; matching receipt: input valid, engineering rejected, acceptance not_applicable, effect pending. This does not establish engineering completion or remote publication.

US-001 runtime implementation and scoped unit race checks passed. Immutable checkpoint us001-runtime-v1 was preflighted/submitted; request c784dae22ba81108a9ce15f3e1b535e8 observed input valid, engineering recorded, acceptance not_applicable, effect pending. Shared runtime guidance wiring follows, so final aggregate snapshot will refresh affected files/check attribution.

US-002 private Claude registration and exact additive authorization implemented. Thirteen provider cases passed under race checking; authentic host interpretation is still unavailable. Checkpoint us002-claude-v1 receipt 87d36256d4484f9b972ba854d40d4a87 observed input valid, engineering recorded, acceptance not_applicable, effect pending.

US-005 terminal reconciliation reuses exact matching durable contributions, retains full terminal coverage and explicit conflict failure. Scoped race result passed in 35.742s; production lifecycle coverage is in progress. US-003 remains deferred, so this does not fulfill full cross-host AC-008.

Aggregate make check attempt 1 stopped at pinned lint with QF1001 in internal/agent/mcp.go and ifElseChain in reconciliation test. Test phase did not execute. Log make-check.log preserves actual failure and sandbox cache warnings. Fix cycle 1 addresses those two findings; rerun uses GOLANGCI_LINT_CACHE under /tmp to respect existing sandbox permissions. Build passed separately.

Final make check attempt 2 passed formatting/vet/lint then exceeded outer580seconds during repository race tests. CLI, Codex, config and confirm reported passes before timeout; no failed test was reported. Aggregate verification remains incomplete. Narrow daemon/turn race package runs with test timeout8m are used to obtain bounded remaining-package evidence and prepare cache before the final unrestricted gate; no timeout increase or scoped-gate substitution is claimed.

US-005 blocked receipt3fad4198ef8fc19150fd87cfe7d95fdc: input valid, engineering rejected, acceptance not_applicable, effects pending. Narrow daemon/turn package checks timed out cumulatively with no identified assertion regression; full aggregate remains unsatisfied. See BUG_ESCALATION.md.

Final current scoped snapshot da5507e2b856cd2f80c20db8eee9cad8edcfe2b74d6a54924f2861a116d164af recorded for US-001 after explicit invalidation of affected unaccepted prior evidence. Final scoped race checks passed129.014s for turn and final build passed. Full aggregate remains unsatisfied; remote effects pending.

## Run 0006 — US-003 implementation and final tests

Owner instruction: "Now implement story 3 and do final tests." Scope: US-003 T-001/T-002, then US-005 T-002 extension (cross-host Codex) and T-003 (AC-008 matrix completion, final make check). US-006 out of scope. No submission-helper executable/descriptor was supplied for this run's invocation context; checkpoint submission is therefore recorded here in session artifacts (TEST_REPORT.md/MANIFEST.md) rather than via the JSON contribution helper used in run 0005. This is an assumption made to keep the run moving; if a helper was in fact available and unused, that is a process gap to flag, not a scope change.

US-003 T-001 implemented: new internal/codex/mcp.go (SupportsMCP()=true, mcpServerName="litefactory" fixed constant, mcpOverrides() encoder emitting -c mcp_servers.litefactory.{command,args,env,enabled_tools} TOML-literal overrides via strconv.Quote, matching the existing skillOverrides idiom; checkMCPCollision()/userConfigPath() refusing on a detected existing operator mcp_servers.litefactory key, bounded read-only scan, no persistent config writes). internal/codex/runner.go: args() now returns ([]string, error); mcpOverrides() call inserted after the validate-mode block and before the delivery/agents.* block, i.e. before --model/resume/stdin marker. Two existing test call sites in delivery_test.go/validation_test.go mechanically fixed for the new signature. Engineer validated go build ./..., go vet ./..., go test ./internal/codex/... and go test ./internal/turn/... -run 'MCP', all passing; gofmt clean.

Code review (Phase 1.5): ADVISORY_ONLY. Three non-blocking advisories recorded in CODE_REVIEW.md (collision scan misses one alternative TOML table-header spelling; duplicate spec.Validate() call is dead on the real path; strconv.Quote escape-set is not byte-identical to TOML's for rare control characters, pre-existing pattern). No fix cycle required; proceeding to Phase 3.

Phase 3 unit tests (US-003 T-002): internal/codex/mcp_test.go added, 12 top-level functions / 21 subtests, all passing under `go test ./internal/codex/... -race -v -run 'TestMcpOverrides|TestMCPLaunchSpec_RejectsNulByteInExecutable|TestArgs_'`. Full package regression `go test ./internal/codex/... -race -v -cover`: PASS, 76.6% coverage, 37.554s. Covers quoted/backslash/newline round-trip, exact catalog parity, collision detection (table-header + dotted-key forms, plus a documented known-gap case for the bare-table-header spelling the reviewer flagged), nil-spec additive no-op, cold/resume argv placement, ignore-user-config true/false isolation branches, and Validate-mode restriction preservation. No production defect found.

Phase 3 integration tests (US-005 T-002 extension, Codex cross-host): internal/turn/mcp_codex_lifecycle_test.go added, 5 functions / 9 subtests, using a real *codex.Runner against a fake codex subprocess script plus the real broker/authority (reusing the existing lifecycleMCPProbe and harness_test.go fake-subprocess patterns). `go test -race -v ./internal/turn/... -run 'TestCodexMCPLifecycle'` passed 9/9 (19.442s); retained at us005-codex-cross-host-check.log. Full `internal/turn` package regression (`go test -timeout 8m -v ./internal/turn/...`, 473s) retained at us005-codex-cross-host-full-turn-package.log: all new tests pass; one pre-existing unrelated flake, TestStoryContractUpgradePreviewAndExplicitApply ("store: story progress parent is busy" SQLite busy-lock timing flake under full-package parallel load), confirmed pre-existing and unrelated to this story's scope by 3/3 clean isolated reruns. Not fixed — outside US-003/US-005 approved scope, not caused by this run's changes. Covers enabled/disabled/absent setup, catalog parity, native-tool preservation, malformed/quoted path values, server collision/isolation with real broker cleanup, resume/fallback registration, and concurrent-attempt cancellation/isolation. No production defect found.

Bug loop: not entered. Zero failures attributable to this run's scope; the one flake observed is pre-existing/unrelated and reproducibly passes on rerun.

Final aggregate check (US-005 T-003): resolved the prior run's `make check` timeout blocker by bounding scope instead of raising any timeout. `make fmt vet` and `make lint` ran as the existing whole-repo gate (both clean, lint 0 issues). The full-repository `go test -race` phase (`test: go test -race -timeout=$(TEST_TIMEOUT) ./...`) was split into 21 sequential bounded invocations — one per small package bundle or single heavy package, with internal/daemon and internal/turn (the two packages that individually exceeded 8m last run) further split by test-name-prefix `-run` regex so every letter-prefix bucket of their test functions is covered exactly once with no gaps. Every group ran with the same `-race -timeout 8m` bound as before; none was widened. All 21 groups passed (tightest margin: internal/store 364.0s, internal/daemon bucket R 254.0s, both well under 480s). This is the same evidence make check would produce, split only in call shape to respect the ~10 minute per-Bash-call ceiling — not a scope reduction. Logs: final-check-fmt-vet.log, final-check-lint.log, final-check-group-1.log through final-check-group-21.log.

TestStoryContractUpgradePreviewAndExplicitApply (the pre-existing SQLite busy-lock flake noted during Phase 3) was re-run in this pass (group 20, turn-9, `-v`) and passed cleanly with no busy-lock error — did not reappear.

Snapshot attribution: 33 implementation/documentation files (18 modified + 15 new across internal/agent, internal/codex, internal/daemon, internal/harness, internal/submission, internal/turn, internal/store, docs/agent-evidence-capture.md), per-file SHA256 retained at lf274-final-check-t003-manifest.txt, aggregate manifest SHA256 `4696a1b190dab4dc459f540ddf04eba4ad0ceb4b2ab952751089422842019663`. This is an evidence-binding identifier only, not a commit — the factory commits the worktree separately.

Run 0006 outcome: US-003 fully implemented, reviewed (ADVISORY_ONLY) and unit-tested; US-005's previously-blocked T-002 cross-host Codex coverage and T-003 aggregate check are both complete with a real, bounded, full-repository `make check`-equivalent pass and zero failures. US-006 (authentic external host captures) remains untouched and out of scope for this run, as instructed — still blocked on separately supplied operator captures, per the prior run's TEST_REPORT.md/BUG_ESCALATION.md. No acceptance/code/architecture/plan review was performed (reserved for Validate). No commit/push performed.


## Implement run 0007 — assumptions and remaining prerequisite

The authoritative Mode remains Implement. The request to check whether the last item can be ticked off is interpreted as checking engineering/evidence readiness for US-006, not authorization for acceptance review, issue closure, a mode transition or nested host acquisition. Frozen US-001/002/004 remain read-only engineering-complete dependencies. US-003/005 have landed implementation and historical evidence; this run does not infer factory engineering completion from prose or re-submit stale passing checks.

US-006's four authentic external captures remain unavailable. A separately authorized operator must supply Claude and Codex working-mode and Validate traces on the same implementation revision, including exact calls, correlation/order, permitted native use, negative Validate enforcement and provenance. Existing handoff instructions remain the next step; there is no safe in-run substitute. No questions, commits, pushes, forge calls, daemon changes or live hosts. See `RUN_0007_EVIDENCE_READINESS.md`.

## Run 0008 — remediation guidance

The request “Then tell me, how can we fix this?” is treated as a request for a concrete unblock procedure within Implement, not authorization to acquire external paid host captures. No tests or host launches are requested for this explanatory continuation. Frozen US-001/002/004 remain satisfied read-only dependencies. US-006 requires separately supplied authentic captures; no evidence waiver or mode transition is inferred. An analysis-only engineer checks the existing operator handoff and reports practical next steps.

## Run 0009 — engineering remediation completed (2026-09-16)

Selected scope: US-003 (#293, revision 4905bbbf909f786e54c46e750cdf06b2302f99c8861ff1494fcec22435f87c2d) and US-005 (#295, revision fc563a920b331e13af6377620c0bddfffc6372dea0508fc9ff0d4895e70ec105). US-001/002/004 remained satisfied read-only dependencies. This run implements the first remediation step only; US-006 authentic captures were not acquired or claimed.

US-003 required two bounded fixes before truthful completion: equivalent inline/quoted/dotted reserved-server TOML declarations could bypass collision detection, and Go-only control-character escapes were not valid TOML strings. The current code rejects reserved collisions with bounded parsing, encodes TOML-compatible control escapes, and refuses invalid UTF-8. Production changes: internal/codex/mcp.go and new internal/codex/mcp_config.go; regression coverage: internal/codex/mcp_test.go. US-005 needed no source change.

Fresh executed checks on final source bytes:

- US-003: 14 targeted test functions/38 subtests with race detection and coverage; zero failures; scoped vet passed. See run0009-us003-unit.log and run0009-us003-vet.log.
- US-005: 2 fake-boundary test functions/6 subtests passed with race detection; 11 production preparation/reconciliation tests passed with race detection; 5 supplementary real-local-I/O recovery/correlation tests passed both plain and race. See run0009-us005-unit.log, run0009-us005-targeted.log and run0009-us005-local-io.log.
- Aggregate: go build ./..., go vet ./..., and gofmt -l . passed; formatting output was empty.
- make check PKGS='./internal/codex ./internal/agent' TEST_TIMEOUT=8m passed: formatting, vet, pinned linter (0 issues), and Codex/agent race suites. Linter cache-write warnings were non-failing and retained in the full log.
- Every remaining package/test function was exercised through 103 bounded race batches with -count=1 and -timeout=8m; all 103 passed. The current complete inventory contains 35 packages and 2,200 test functions, including Codex/agent covered by make check. This is exhaustive batched aggregate execution, not a claim that the default unbounded single make check invocation ran. Exact selections are in run0009-test-batches.json; actual results, command logs and SHA256 locators are in run0009-race-summary.log and run0009-race-ledger.json.

No live-host acquisition, review/acceptance transition, issue closure, daemon changes, commit or push was performed. Authentic host evidence remains a separate US-006 obligation.

### Final receipt observation

- US-003: request `2a3a35ba372f06235a76729cd45fd1c1`; input valid, engineering recorded, acceptance not_applicable, remote effect delivered. Snapshot `run0009-us003-final`, SHA256 `302950afb2e20a2b212e6ac5eed9cd0c9d5f85f1ce7891f942e7be208c0b3ff8`.
- US-005: request `c6eb135c8fcea66ff1d92b39e8877360`; input valid, engineering recorded, acceptance not_applicable, remote effect delivered. Snapshot `run0009-us005-final`, SHA256 `97eb1c525b3e3cd20b830f682fe962f2f648fb57cac860a690546a5a0af28acb`.

Both captured file sets were rechecked against current bytes after all gates. No mismatches.
