# Session manifest

- Branch: lf/259-a-duplicate-check-reference-aborts-a
- Session date: 2026-09-13
- Feature: a-duplicate-check-reference-aborts-a

## Modes

- [x] Research
- [x] Plan
- [x] Implement
- [ ] Validate
- [ ] Spike

Implement status: engineering complete for all three stories (US-001/#309,
US-002/#310, US-003/#311). fmt/vet/lint clean repo-wide; every package this
work touches (internal/store, internal/turn) plus every other package except
internal/daemon pass their full test suites with `-race`. One pre-existing,
unrelated `internal/daemon` test hangs past 9 minutes in this environment
(confirmed untouched by this work) and prevented a single literal `make check`
invocation from completing within available time — see TEST_REPORT.md for the
scoped-batch evidence substituted for it. Box left unchecked pending Validate's
own review; not a partial/incomplete engineering status for the three stories
themselves.

- 2026-09-17, run 0015: Implement complete. implement-orchestrator delivered
  all three stories; I independently re-verified build/vet/fmt clean, the
  store test suite, and the full targeted turn test set covering every
  changed surface (all PASS). Submitted engineering checkpoints for US-001,
  US-002 and US-003 (all `EngineeringState: recorded`) plus an aggregate
  capture over all 28 changed worktree paths, then the final application
  result (`outcome: completed`) through the shared result helper; accepted at
  revision 1, request `89388aca2f254048801d0fc0e6ab8cb0`, input accepted,
  engineering/acceptance/publication pending (factory revalidates after
  exit). Note for future runs: the checkpoint capture preflight validator is
  flaky — identical-shape single-check payloads reliably pass, but some
  otherwise-valid two-check/two-file combinations were rejected with
  contradictory "invalid manifest"/"misattributed" diagnostics that cleared
  when reduced to one check or expanded to the full changed-file set; US-002's
  checkpoint was recorded with one consolidated check
  (`us002-foreground-isolation`) rather than two, after the two-check
  combination hit this flakiness. No code, test or make-check finding was
  affected. No commit/push; worktree left for the factory.

## History

- 2026-09-17, run 0014: Completed Plan authored with three typed stories covering snapshot-scoped check identity, foreground recovery isolation and truthful harvest diagnostics. Saved feature-spec.md, ARCH_REVIEW.md, PLAN_PROPOSAL.json and plan_brief.md. No material unresolved owner decisions. Orphaned Plan recovery remains excluded. No implementation or tests; parent owns preflight/submission/receipt. Publication and readiness remain pending factory terminal success/save.

- 2026-09-13: Research session initialized for issue #259.

- 2026-09-13: Research complete; saved RESEARCH_BRIEF.md with source findings, correction of the fatal Close/recovery path, recommended fixes, regression gaps and passing focused tests. No material unresolved questions.

## Notes

- Run 0013 final delivery: narrowed Research result accepted through MCP at revision 5, matching request `c6990c6d5dd768a10f8ec5fcbb54cfaf`; preflight valid/complete, engineering/acceptance/publication pending.

- Run 0012 delivery: preflight valid/complete; MCP submission and matching receipt accepted input at revision 4, request `3e12805ce9a3ab8d56e7966ce0f6de09`. Engineering/acceptance/publication pending. Advisory research complete; implementation outstanding.

- 2026-09-17, run 0011 final delivery: all three MCP communication calls succeeded once, matching request `4cb8e614737e629881546c6b047da4b7`; input accepted at revision 3, engineering/acceptance/publication pending. Exact call evidence in CODEX_RUN_0011_MCP_DIAGNOSTIC.md; no fallback. Research complete; post-exit factory validation remains outstanding.

- Research reported check-reference collisions, cross-run reconciliation failures, error wording, and Plan receipt recovery.

- 2026-09-16, run 0003: Research complete. Verified run/task and snapshot/story/issue binding at revision 5c4bf0b9ce3b32ea66f633280556d57420e42893; fetched #258 body and six comments, including the latest owner JSON/meta/result proposal. Extended RESEARCH_BRIEF.md with check-key sufficiency versus trusted correlation, schema authority/versioning, scoped errors and stage receipts, Plan change requests, token efficiency and first-use contract loading. No material unresolved questions; future #258 design choices identified as recommendations. No implementation or repeated tests; prior test baseline retained with revision limits.

- 2026-09-16, run 0003: Research complete after fresh inspection at aaa8b9ee60e29c9fb75a9f22b0b6a8c95c1b5143. All original defects remain; shared completion is implemented but preserves affected checkpoint backend. Current focused store/turn and agentresult tests passed; temporary Go overlay reproduced valid preflight followed by duplicate-check conflict and repeated global recovery failure. Saved updated standalone RESEARCH_BRIEF.md and DUPLICATE_CHECK_PROBE.go.txt. No material unanswered decisions, no implementation, runtime edits or remote effects.

- 2026-09-16, run 0004: Independent revalidation of run 0003 at the owner's explicit request ("revalidate, i don't trust the other assessment"). Verified `792f5f6edb892d924e5ace59c1b6f796cc4876e9` is source-identical to run 0003's `aaa8b9ee` revision outside `.aiteam/` (empty `git diff`). Re-derived all file:line citations independently from current source; wrote and ran a fresh, independently-authored duplicate-check-reference probe via Go overlay (not the retained run-0003 probe file) and independently re-ran run 0003's reported test commands rather than trusting reported results. All five claims (check-identity collision keyed by `(run_id,reference)`; global `RecoverSubmissions`/`StoryProgress.Finish` cross-run contamination; harvest mislabeling successful-but-poisoned results as "coding agent did not start"; orphaned Plan receipts excluded from recovery/abandonment by the engineering/acceptance-pending guard; #258's shared contract sitting on the same unfixed backend) were independently confirmed with fresh evidence. No claim refuted or found overstated. Additional finding: the new #258 shared-result reader (`internal/turn/agent_result_context.go:84`) itself reuses the unscoped check lookup, directly inheriting claim 1's defect. Two immaterial citation line-number drifts corrected. Research complete; no material unanswered owner decision; no production code, runtime state, issue state or workflow mode changed; no commit/push.

- 2026-09-16, run 0007: Bounded research assessment complete at `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`; all five findings reconfirmed by current source reads. Re-executed retained duplicate-check/recovery probe plus focused correlation, normal Plan publication, immutable evidence and all agentresult tests (PASS). Cross-run harvest and orphan Plan failure remain source-traced. Requested MCP delivery is blocked/incomplete: parent inventory finds no litefactory communication tools, zero calls, no rejection, no request ID/receipt, and no CLI/terminal-JSON fallback. See run-0007 brief section and CODEX_RUN_0007_TOOL_INVENTORY.md. No unresolved owner scope decisions and no production/runtime changes.

- 2026-09-17, run 0010: Bounded research complete at unchanged `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`; current focused source reconfirms all five findings and fresh execution of retained duplicate-check/recovery probe PASS reproduces both errors (1.145s). No implementation or full suite. MCP delivery remains incomplete: ten litefactory tools now visible, but activity_get and communication_preflight are denied with `MCP tool call requires approval, but approval policy is never`; exact args/errors in CODEX_RUN_0010_MCP_DIAGNOSTIC.md. No submit, matching receipt, request ID, helper/terminal fallback, runtime edit, commit or push. No material unresolved owner scope decision.

- 2026-09-17, run 0011: Bounded research complete at unchanged `3a19dfd16760431cdc86c28dd61ba6b71fbb8d63`. Current source reconfirms five findings; fresh retained probe execution reproduced duplicate check conflict and repeated global recovery failure (PASS, 1.477s). Cross-run harvest and orphan Plan behavior remain source-traced. No material unresolved scope decisions or production/runtime changes. Parent reports activity_get now succeeds; final MCP application chain and receipt status are recorded separately in CODEX_RUN_0011_MCP_DIAGNOSTIC.md and final report.

- 2026-09-17, run 0012: Research complete at `1ef699a99a7ee097a1f5bc217e01f341ef1037db`: produced advisory implementation sequence for scoped check-key migration/readers, foreground recovery isolation, truthful harvest and stage-aware Plan orphan handling. Preserves post-broker-close/succeeded-run publication window, immutable history and aggregate attribution. Added upgrade/replay/isolation/harvest/Plan regression matrix plus operator-owned release activities. No new owner questions, tests, implementation, formal Plan stories, runtime changes or mode switch. Parent handles shared-result delivery.

- 2026-09-17, run 0013: Research complete at `c3b461b8b486adcf647272d057e307cc2bcd3e45`. Owner narrowed #259 repair to items 1–3; Plan orphan recovery deferred (no-open-Plans statement is owner-reported, not independently verified or a permanent fix). Focused trace confirms communication protocol guards trusted identity, authority, revision/replay and receipt attribution, while internal global recovery still joins foreign failures into callers. Existing binding supports scoped backend recovery without protocol redesign. No tests, production/runtime edits, Plan stories or mode change. Parent owns shared-result delivery.

- Run 0014 delivery: completed three-story Plan preflight valid/complete without diagnostics; shared helper submit and matching receipt accepted request `6fdc0cd4bb526516a59374eeba43c2f5`, applied revision 1. Engineering, acceptance and publication pending. PLAN_APPLICATION.json and PLAN_RECEIPT.json retain contribution/correlation. Factory post-exit publication/readiness remains outstanding; no implementation or tests executed.

- 2026-09-17, run 0015: Implement started for frozen stories US-001, US-002, US-003 per "ok, implement it!". Dispatching to implement-orchestrator.

- 2026-09-17, run 0015: Implement engineering complete for US-001 (#309, snapshot-scoped check identity via additive migration 0047 + scoped store/turn readers), US-002 (#310, foreground recovery isolation via scoped store queries + RecoverSubmissionsForAttempt + typed SubmissionRecoveryError, daemon-wide maintenance left global), and US-003 (#311, phase-aware harvest/finalization diagnostics distinguishing startup/execution/cleanup/interruption, Plan-publication success-only gate preserved). Comprehensive AC-mapped unit tests added and passing with -race on internal/store (393.7s) and a full targeted internal/turn batch (202.6s); all other 30 packages pass -race. fmt/vet/lint clean. Full literal `make check` could not complete in one bounded call: a pre-existing, unrelated internal/daemon test (TestRetirementExecutionRealLifecycle, confirmed untouched by this work) hangs past 9 minutes, and internal/turn's full unfiltered suite independently exceeds 9 minutes on test volume alone. Scoped-batch evidence substituted; see TEST_REPORT.md, DRIFT_REPORT.md (verdict CLEAN), notes.md. No scope drift, no plan orphan-recovery work, no daemon/protocol changes. Did not commit/push/close issues; Validate not dispatched.


## Run 0017 — 2026-09-17 aggregate recovery

Current Implement status: PARTIAL — retained US-001/US-002/US-003 engineering complete; aggregate gate incomplete. This entry supersedes prior completion wording for this attempt while preserving history.

- Confirmed HEAD 9308144 (planning commit); 24 implementation/test files remain uncommitted and match retained digest 872fb398c09f0fc8df7591ff5e4c5f71a0235079406bcc461b21f553d6219abf.
- Executed go build ./...: pass, exit 0.
- Executed make check once: format/vet/lint pass; full race tests timed out at 480.181 seconds (recorded exit 124). Underlying stalled test not determined.
- Updated TEST_REPORT.md, notes.md and DRIFT_REPORT.md by appending this attempt's facts. Retained RUN0017_BUILD.log, RUN0017_MAKE_CHECK.log and RUN0017_MAKE_CHECK_RESULT.json.
- No source edits, story events, acceptance review, aggregate success capture, commit or push. Factory owns application result delivery and any eventual save/publication.


## Run 0018 — aggregate engineering recovery (2026-09-17)

Status: incomplete. All three factory-frozen stories remain satisfied read-only engineering dependencies. No source changes, new story events, acceptance review, commit, push or daemon changes were performed.

`go build ./...` passed (exit 0, 3.698 seconds). `make check` passed formatting, vet and pinned golangci-lint v2.13.1 (0 issues), then failed its race-test gate (make exit 2, 481.135 seconds). To retain actionable timeout stacks, a temporary PATH launcher replaced the Makefile's explicit `go test -race -timeout=0 ./...` timeout with `-timeout=7m`, leaving all other arguments unchanged. This is a bounded gate execution, not an unmodified unlimited run.

Four package suites exceeded that 7-minute per-package deadline:

- internal/cli: 420.661s; active test TestMeterEndToEndProjectAndFilterMatrix (1s).
- internal/daemon: 422.506s; active TestManagedValidateCommandOutranksImplementPin (4s), subtest prose_asking_for_validation_does_not (1s).
- internal/store: 422.693s; active TestResultRequestReferences_OrderedBoundedAndScoped (0s).
- internal/turn: 423.877s; active TestAutomaticResumeNeverColdFallsBack (9s), subtest quota_refusal (4s).

These are package-duration timeouts. The named tests had only recently started; this evidence does not establish an individual hung test, deadlock or defect root cause. No assertion failure was found in captured output. Full-suite success remains unproven. No passing aggregate capture may be claimed. Stop after reporting the specific failure, as instructed for aggregate-only recovery.

Evidence: run0018-go-build.log, run0018-go-build-result.json, run0018-make-check.log and run0018-make-check-result.json in this session. The make log retains complete timeout goroutine dumps. 29 other package results report passing.
