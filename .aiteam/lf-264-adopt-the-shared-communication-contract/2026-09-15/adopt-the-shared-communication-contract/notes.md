# Implement run 0007 notes

Workflow version 2; requirements are the factory-supplied frozen US-001–US-010 revisions. Work proceeds in ascending story order, with independently ready work retained if a harness or compatibility gate blocks a story. No acceptance or code review is commissioned in Implement.

## Assumptions and known gates

- Shared attempt completion and evidence wiring follow existing service owners; no new transport business logic.
- US-007 requires Claude to edit the installed `.codex/` targets; this Codex run cannot fulfill that harness requirement or write that protected tree. Exact targets will be recorded from the US-005 manifest.
- US-009 requires factual runtime cohort and pending-effect evidence before each legacy reader deletion. Synthetic tests cannot establish drain; missing authority or access leaves retirement incomplete.
- Authentic working/Validate observations remain external obligations on #274/#296.
- Scoped checks precede each engineering contribution. Aggregate checks follow available engineering work. Receipts distinguish input, engineering, acceptance and remote effects.

## Progress

- US-001: implementation engineer dispatched.
- US-002–US-010: pending predecessors or ascending sequence.

### US-001 implementation checkpoint

Implementation engineer reports shared helper/terminal finalization independent of MCP availability, preserved process failures and restricted Validate, existing Plan authority, explicit legacy compatibility, whole-batch Validate conflict checking and completion hooks. Focused turn/broker tests and scoped vet passed. New scoped unit and local persistence/runner matrix checks are in progress; no engineering contribution submitted yet.

The concrete SQLite-backed service/runtime types require real local persistence tests for current-attempt finalization and checkpoint reconciliation; pure unit checks cover available no-I/O seams. US-002 must provide authoritative aggregate/checkpoint enrichment; missing evidence currently fails closed.

### US-001 engineering submission

Six new unit cases and 15 selected real-dependency test functions passed; plain and race runs passed. Largest scoped race selection: 336.310s. Snapshot digest: 5c79581c6ea54c7c26eb2ce017508d11e94151a054006c035359782f9b775804. Factory request 7e9e9404aae8b4b71af9df6e5dfdc1a1: input valid; engineering recorded; acceptance not_applicable; remote effects pending. Retained US-001-contribution.json, US-001-preflight.json and US-001-receipt.json. Pending remote effects are not success.

US-002 implementation dispatched after recorded US-001 engineering. Its authoritative aggregate/evidence enrichment is still required before adoption can complete.

### US-002 implementation checkpoint

Engineer reports complete scoped launch baselines, historical receipt recovery, stale-update diagnostics, authoritative checkpoint resolution and atomic aggregate capture/receipt wiring. Existing focused checks and scoped vet passed; new unit and local persistence tests are in progress.

Implementation assumption: the existing AggregateCapture type and RetainAgentResultAggregate owner receive a narrow shared-helper capture adapter because no production ingress previously existed. Scope remains factory-bound; scoped checks are never relabeled aggregate, and no acceptance authority is added. Current-worktree matching remains required. This is necessary AC-005 wiring, not a scope waiver or a new business-result schema.

### US-002 engineering submission

16 unit cases and 13 top-level persistence/race tests passed. Factory request 0a7c4ef7a16f41ea96967a72858d7e2b: input valid; engineering recorded; acceptance not_applicable; remote effects pending. Exact immutable capture, preflight and receipt are retained in US-002-contribution.json, US-002-preflight.json and US-002-receipt.json. US-003 now proceeds after both required engineering dependencies were recorded.

### US-003 engineering submission

Four unit functions / 32 cases, 14 publication-runtime groups / 44 cases, and three daemon recovery cases passed, including scoped race execution. Factory request 5108dcc7434c98eb291137c44f159cc0: input valid; engineering recorded; acceptance not_applicable; remote effects pending. Captures/preflight/receipt retained under US-003. US-004 activity lifecycle implementation is now dispatched.

Owner-control implementation assumption: `/mcp-create prepare <CreationEnvelope JSON>` connects the existing authenticated owner control to the existing retained proposal presenter. Preparation is separate from exact hash/relationship approval, and grants remain next-eligible-Research-attempt scoped. No actual forge operation was performed by this run.

### US-004 engineering submission

11 unit functions and nine runtime/persistence tests passed, including scoped race checks. Factory request eb5f91b010cbde08b8c7922859b7209d: input valid; engineering recorded; acceptance not_applicable; remote effects pending. Bounded cleanup, terminal/capability lifecycle and durable publication preservation verified. A dedicated daemon rate-limit-backoff expiry end-to-end case was not added; existing daemon recovery and runtime cleanup cases passed. US-005 shared-source/docs/auxiliary work dispatched.

### US-005 engineering submission

12 focused source/render/auxiliary tests passed with race checking; neither installed tree was edited. Factory request 157e13d27af90eaf0ee765c9cfd00421: input valid; engineering recorded; acceptance not_applicable; remote effects pending. Exact snapshots/preflight/receipt retained under US-005. US-006 now updates only manifest-selected Claude installed copies under Codex, including its explicitly assigned scoped owned-tree assertions.

### US-006 engineering and US-007 handoff

All 14 owned Claude targets were updated and eight source/ownership assertions plus scoped Claude temporary-render/manifest race checks passed. US-006 request b0e7af6d36c2b05b42f243f3a974566e: input valid; engineering recorded; acceptance not_applicable; effects pending.

US-007 is blocked under Codex: the frozen story explicitly requires Claude, and `.codex/**` is protected read-only. Workflow skill `.codex/skills/workflow/SKILL.md`, section Which tree you may edit, says: "This tree — `.codex/**` — is the one you must not write, and a story that requires changing it is not yours to finish." This matches frozen scope and actual sandbox constraints. Exact 12-target manual Claude handoff is in US-006-engineering.md and the current manifest. No nested provider launch or permission retry is authorized. Request 4cbb33e6a3cbe5117dd286ffc63616a5 recorded the blocked contribution: input valid; engineering rejected; acceptance not_applicable; effects pending. Independent US-008 follows in order.

### US-008 implementation and uncertain submission

Unreachable Markdown discovery and exclusive renderer/parser/second-commit wiring were removed after caller ownership proof; live shared helpers remain. Fourteen pure tests and thirteen real git/SQLite/HTTP tests passed, plain/race. Preflight valid and complete. Submit request 13f85e6dcebecba4385b9c88c1a971c1 lost its reply; one receipt lookup says processing did not complete. Exact bytes/request and response retained under US-008. Engineering is implemented/tested but NOT claimed factory-recorded. US-009 is independently ready from US-001/002 and is inspecting the separate legacy compatibility gates.

### US-008 reply recovery and US-009 blocker

An identical-byte US-008 retry recovered the same request ID 13f85e6dcebecba4385b9c88c1a971c1 with input valid, engineering recorded, acceptance not_applicable, remote effects pending. No new correlation or duplicate submission was invented.

US-009 retains all three legacy readers. Factory request f3ca29e211c73348b4cf790247c8e4c1 recorded blocked status (input valid, engineering rejected, acceptance not_applicable, effects pending). Runtime cohort/drain evidence is unavailable from authorized local interfaces; supported upgrade preview would read the forge, reserved to the factory. Disposition and full follow-up/scope-revision draft are retained. Existing local compatibility regressions are being executed; they cannot establish live drain.

## Final run status

No current engineering-complete claim remains. US-001–006 and US-008 retain implemented work and historical scoped passes, but current checkpoints are invalidated pending final-state repair/verification. US-007/009/010 are blocked. Aggregate build/format/vet/pinned lint pass; CLI/daemon plain suites pass; native lifecycle/family catalog and installed instruction tests fail; bounded full turn/race gates did not complete. Two permitted aggregate fix cycles are exhausted. Exact failures and next steps are in BUG_ESCALATION.md, and final source metadata in FINAL_IMPLEMENTATION_STATE.json. All observed latest invalidated/blocked inputs are valid, engineering rejected, acceptance not_applicable, effects pending. No receipts were interpreted as acceptance or publication success.


## Implement run 0015 — one-off inventory script

The current owner instruction replaces the proposed product inventory command with a session-local script. This is US-009 prerequisite execution, not a new product feature or an authorization to migrate runtime state. Only the supplied US-009 revision ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7 is selected; US-001/002 remain satisfied read-only dependencies.

The script uses existing read-only store accessors and was run against the actual runtime database. There are 186 canonical session owners and 89 selected pending/unsuccessful records. Legacy cohorts with unfinished stories are #73 (4), #169 (1), and #207 (1); their implementing state prevents supported upgrade admission. The runtime database lacks two publication-operation ledgers, so the report explicitly remains incomplete and all removal gates retain readers. No migrations, forge operations, daemon-state rewrites, reader deletions, or product API/CLI changes were performed.

The script is an interval inventory, not a proof that the daemon has stopped admitting legacy work. Absence of fixture consumers does not prove runtime drain. See US-009-script-guide.md and US-009-live-inventory-run-0015.json.


Run 0015 aggregate gate: make check exceeded its 480-second outer bound (recorded exit 124). Vet and pinned golangci-lint v2.13.1 passed; full race test suite did not complete. No aggregate pass is claimed. See run-0015-make-check.log and run-0015-make-check.exit. The current script-only scope provides no evidence justifying unrelated product repairs.

## Implement run 0017 — selected operator scripts

The owner selected only task #230 and #259. Treat these as task targets within US-009, not new story identities; ignore other open tasks for these scripts. Delivered session-local fixed-target entrypoints upgrade-230.sh and upgrade-259.sh with upgrade_pending.py. Default preview and explicit guarded apply use the existing factory CLI; no live preview/apply was executed here. Existing planning/active-scope refusals remain intact. No product functionality, runtime state edits, direct forge calls, or legacy removal were introduced. See US-009-scripts-230-259.md for exact commands.
