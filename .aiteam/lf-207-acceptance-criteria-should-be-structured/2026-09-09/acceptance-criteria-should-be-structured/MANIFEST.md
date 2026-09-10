# Manifest
# Branch: lf/207-acceptance-criteria-should-be-structured
# Session: 2026-09-09

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(done)*
- [x] **Plan** — `plan_brief.md` + managed GitHub story proposal *(run 0006 completed; factory publication/readiness pending)*
- [ ] **Implement** — code + tests *(feature partial; run 0013 implements US-010; US-011 requires Claude and US-012 is dependency-blocked; factory-completed US-001–US-009 retained; factory save pending)*
- [ ] **Validate** — read-only retained-evidence findings *(not started; no artifact or manifest update from Validate)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-09 — session created; Research requested by run 0001 for issue #207.
- 2026-09-09 — Research completed for run 0001: structured acceptance criteria and factory-owned story-progress envelopes investigated; repository findings, incident provenance, compatibility risks, and Plan decisions recorded in `RESEARCH_BRIEF.md`. No confirmation pause per headless instruction.

- 2026-09-09 — Research completed for run 0002: retained run 0001 findings as carried forward; reverified serial first-error behavior, live ingestion versus missing author feedback, both runner channels and same-run correction semantics. Added preflight/receipt boundaries, aggregated diagnostics, lifecycle risks and provenance limits (two diagnosed rejections; run 14 pending). Updated `RESEARCH_BRIEF.md`; no implementation or tests, confirmation pause, commit or push.

- 2026-09-10 — Research completed for run 0003: reused this session; consolidated all open questions into 7 owner-facing choices and 7 Plan/architecture decisions with explicitly unapproved defaults and tradeoffs. Preserved earlier research as carried-forward evidence, distinguished settled asks from questions, and added current legacy/separated workflow and instruction-ownership constraints. Historical run 14 remains unknown and is not a planning prerequisite. No implementation, tests, operational access, confirmation wait, commit or push.

- 2026-09-10 — Research completed for run 0004: recorded owner decisions, expanded scope to all five story sections, preserved free GitHub editing and explicit CLI-only legacy upgrades, and saved an illustrative typed story/submission/receipt sketch with equivalent evidence alternatives, proposed IDs and reconciliation limits. No production changes, runtime edits, tests, approval wait, commit or push.

- 2026-09-10 — Research completed for run 0005: recorded owner approval of the full structure, evidence Option B, criterion-ID rules and feedback/compatibility direction. Added comment-preferred editing with inline edits retained, concrete bottom-of-story guidance outside parsed content, authenticated owner input, revision conflict detection, and preservation of issue authority, frozen scope and accepted history. All owner review choices are settled; remaining engineering questions belong to Plan. No production changes, tests, operational access, confirmation wait, commit, push or mode transition.

- 2026-09-10 — Plan completed for run 0006 / run database ID 301: requirements engineer captured approved scope in feature-spec.md, architect reviewed owners/lifecycle/transport and decomposed 12 stories in ARCH_REVIEW.md, and product owner authored the complete transient version-1 proposal plus shared plan_brief.md. Saved identity-bound PLAN_CONTEXT.json with no unresolved owner questions; exact current attempt-bound completed outcome supplied. All 12 creates use zero child identities and empty bases; 54 ACs have matching separate evidence-inventory rows. Root validated the current Go proposal/outcome/index parsers; architecture review identified and corrected an atomic GitHub edit-protection overclaim in US-007. Authentic dual-harness helper invocation/correction/receipt evidence remains explicitly unavailable and unsatisfied in US-012 AC-003. Protected-tree stories require independent Codex/Claude handoffs. No implementation, forge operations, broad tests, runtime edits, commit, push or automatic mode transition.

## Notes

- Headless run: never wait for answers or confirmation. Run 0005 approves the structured contract direction, evidence Option B, ID rules and feedback/compatibility, with comments preferred for requesting updates and direct issue edits still supported. The research brief records the approved direction and remaining Plan-owned mechanics. Run 0006 explicitly selects Plan and requests completion from the approved research direction. Plan remains selected after publication; Implement requires a separate owner command. No automatic mode transition or confirmation wait.

## Run 0008 status audit — 2026-09-10

- Run database ID 304; selected Implement/workflow 2 retained. Owner requested actual status following prior exit status 1. No implementation resumption, source fixes, acceptance/review, forge access, new engineering events or mode transition.
- Inspected current source and local/cached repository state; recorded factory-supplied engineering completion for US-001–US-004, saved partial US-005–US-006 work and pending remaining scope. Prior session Implement status was stale; history above is preserved.
- Fresh `go build ./...`, formatting, vet and pinned lint passed; `make check` failed in the race-test phase (internal/turn; observed configured-socket-path-too-long startup diagnostics). See TEST_REPORT.md, notes.md and DRIFT_REPORT.md. Implementation remains partial; no previous-run accomplishments are attributed to this run.

## Run 0009 recovery sketch — 2026-09-10

- Run database ID 305; Implement/workflow 2 retained. Owner requested a one-by-one recovery sketch. Recorded the serial US-005 through US-012 execution checkpoints in notes.md, beginning with diagnosis of the reported path failure and the existing US-005 work.
- Current filesystem inventory confirms inherited submission and typed-Plan source/test files remain present; no completion is inferred from file presence or previous reports. Fresh narrow test reproduction is recorded separately in TEST_REPORT.md.
- The sketch preserves factory-completed read-only US-001–US-004, frozen requirements, incremental snapshot-bound engineering evidence, aggregate/save gates and explicit Claude/external-evidence handoffs. Serial ordering does not narrow the factory-selected eight-story bundle.
- No production changes, new engineering completion events, Plan proposal, review/acceptance decision, commit/push or automatic mode transition. Implementation remains partial.

## Run 0010 / 306 — bounded test-unblocking checkpoint

- Owner explicitly instructed: repair the test blocker, then stop before further stories. Implement/workflow 2 remains selected.
- Reproduced the failure, diagnosed legacy Plan fixtures implicitly selecting typed v2, and changed only their setup to explicitly bind content/submission v1. No production code or assertions changed.
- Fresh focused Plan and full turn race/coverage checks passed; root build passed. Final required aggregate result is retained in TEST_REPORT.md.
- No new story engineering-complete events, acceptance/review, forge access, protected-tree changes, commit/push or automatic mode transition. Feature implementation remains partial; factory owns saving and remote verification.

- First run 0010 aggregate gate hit the default 600-second package timeout despite a separately passing 594.698-second turn suite. A second fixture/infrastructure diagnosis led to explicit bounded `-timeout=20m` in Makefile; no tests/assertions or production operation deadlines were removed. First failure and final rerun are recorded in TEST_REPORT.md.

- Final run 0010 `make check` passed (exit 0; turn race suite 672.931 seconds) and root build passed. The requested test-unblocking checkpoint is complete; Implement as a whole remains partial. Stopped before further story work, with no new engineering-complete or acceptance claims.

## Run 0011 / 307 — US-005 and US-006 engineering completion

- Owner selected only US-005 (#219) and US-006 (#220), using the supplied immutable revisions and persisted workflow 2. Inherited code/tests preserved; completed dependencies were not reimplemented.
- Implemented remaining submission reference/receipt/recovery and typed Plan/frozen-ID/checkbox publication behavior, with unit and offline local-boundary tests. Two initially exposed production regressions were corrected in one fix cycle.
- Scoped `go test -race -timeout=20m ./internal/planbrief ./internal/store ./internal/turn` passed; turn 637.373s. Published atomic consecutive engineering events `.litefactory-story-events/307/000001.json` and `000002.json` immediately afterward, attempt 1, empty acceptance/review, shared 31-file digest `a26a82b05d84c2c9382d7422f7ba4c44dee658713c52c0fe460abd158d37b8b1`.
- `go build ./...` and the one final `make check` passed, exit 0. Fixed test flags preserved throughout. Event snapshots were checked against final worktree bytes and remain current. See TEST_REPORT.md, notes.md and DRIFT_REPORT.md for failures, fixes, tests and scope accounting.
- No acceptance/review, issue closure, forge calls, protected-tree changes, shared context rewrite, commit/push or mode transition. Factory saving and publication remain pending; US-007–US-012 remain outside this selected run. Authentic dual-harness delivery is not inferred from synthetic fixtures.


## Run 0012 / 308 — US-007 through US-009

- Owner selected only US-007 (#221), US-008 (#222), US-009 (#223), persisted workflow 2. Completed dependencies US-001–006 retained.
- US-007 engineering and scoped executed tests passed; atomic event 308/000001.json published with 10-file snapshot digest `f3c51948f891329e72ef7e6756a79b21da3eaccd4fe05386d28c4bd8954bc69d`.
- US-008/009 and aggregate checks remain in progress; feature remains partial. No acceptance/review, forge actions, protected-tree writes, commits/pushes or automatic mode transition.


- Run 0012 checkpoint: US-007 event000001 invalidated by event000002 before necessary US-008 compatibility edits; replacement attempt2 event000003 and US-008 attempt1 event000004 now published. Shared 22-file digest `8e0cec441046f0e2e7b4518e4f9a4376f7346a3018fc6a6d6645446483a952c8`. Full affected race suites and scoped pinned lint passed. US-009 and final aggregate checks remain in progress.


- Run 0012 checkpoint: US-009 engineering complete, event000005, 14-file digest `6daccdf6d077d9e30c0406eaff326b74fbd128d976857265edefbbcf86315cb0`. Full teamsource race suite plus in-memory render/manifest/unchanged-installed-tree checks passed. All selected US-007–009 engineering checkpoints now complete; final build and one make check running. Feature remains partial because US-010–012 are outside this run.


- Run0012 final: selected US-007/008/009 engineering complete; final go build and the one make check passed (format/vet/pinned lint/full race suite). Current events000003–000005 match final worktree bytes; events000001/000002 preserve the earlier checkpoint/invalidation audit. Session TEST_REPORT/notes/DRIFT_REPORT updated. No acceptance/review, forge action, protected-tree write, commit/push or mode transition. Factory saving pending; US-010–012 remain outside selected scope, so overall Implement remains partial.


Run 0013 checkpoint: US-010 engineering complete, event 309/000003.json; 16 Claude manifest targets, snapshot `b1945927ccba057b85a2b5ba8d93f3b73012891cd2139e5d2d691ed26cd21e95`. Scoped assertion/render/whitespace checks and build passed. Final make check running. US-011/012 remain blocked as recorded above; temporary installed-tree parity drift is intentional pending manual Claude handoff.


Run 0013 final: `go build ./...` and the single final `make check` passed, exit 0. Format, vet, pinned golangci-lint v2.13.1 (zero issues) and full race suite passed; turn 730.400s, daemon 391.349s, CLI 259.218s. Linter emitted nonfatal cache-write permission warnings; no permission changes or check retries occurred. Published US-010 event 309/000003.json was verified against all 16 final worktree files. No `.codex` diff exists.

Selected scope remains partial: US-010 engineering complete; US-011 blocked for manual Claude execution of the exact Codex targets in docs/story-contract-instruction-manifest.md; US-012 blocked on US-011 and cannot claim aggregate installed-tree parity. Authentic external evidence for both actual harnesses remains missing/unverified without a waiver. Existing US-001–009 work was preserved. Session artifacts record progress outside the exclusively `.claude/**` US-010 implementation snapshot. No acceptance/review, issue closure, forge actions, commit/push or automatic mode transition. Factory saving remains pending.
