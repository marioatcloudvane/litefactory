# Engineering verification — run 0035

Status: pending implementation. No executed check is claimed by this report yet.

Selected scope: story-001 through story-009 at the factory-supplied frozen revisions.

Scoped tests follow each implementation. Required aggregate gates are make check and make test (race-enabled). Acceptance and code/architecture/plan review are outside this Implement run.

Actual host observations must be reported separately from controlled fixtures. No live Bug workflow, merge, closure, or publication is claimed.

## Story-001 scoped checks

Engineer reported passing existing targeted tests across daemon/store/turn/graphexec/teamsource/harness/report and scoped vet across agent, agentresult, daemon, graphexec, harness, report, store, teamsource, turn.

Unit specialist executed `go test ./internal/agent ./internal/graphexec ./internal/teamsource ./internal/harness ./internal/turn -run '^TestBugWorkflow_' -short -race -cover -count=1`: all five packages passed, ten new test functions. Final test-only cleanup was followed by a passing targeted turn rerun. Coverage percentages are partial package coverage, not criterion-completion claims.

A missing WorkflowSpecs generic-action workflow binding was fixed in internal/turn/research_admission.go. Engineer reran `go test ./internal/turn -run 'Test(BugWorkflow|RegisteredWorkflow|Workflow|ResearchSpecs)'`: passed.

Store/daemon admission recovery and failure-injection verification remains pending. No scoped engineering checkpoint or aggregate check is claimed yet.

Boundary specialist added eight top-level tests in store/workflow_admissions_boundary_test.go and daemon/workflow_boundary{,_internal}_test.go. `go test ./internal/store ./internal/daemon -run '^TestWorkflowBoundary' -count=1` passed (store 0.526s, daemon 0.928s). The same command with `-race` passed (store 2.083s, daemon 2.454s). Covered concurrency/reopen, all prior-evidence sources, ambiguous history, cancellation, failed writes, malformed labels, sticky route, mode-switch refusal, pending publication and denied substitute dispatch/profile injection.

Four focused assertions remain under test authoring: actual worktree cleanup, active Bug graph preservation during mode-switch refusal, resumed graph mismatch, and report-debt clearance. These are not claimed verified yet.

## Story-001 captured scoped result

All remaining boundary assertions passed, including pinned persisted graph mismatch before any handler or attempt. Final combined targeted run: `go test ./internal/agent ./internal/graphexec ./internal/teamsource ./internal/harness ./internal/turn ./internal/store ./internal/daemon -run '^(TestBugWorkflow_|TestWorkflowBoundary|TestBugWorkflowBoundary)' -count=1 -race`. All seven packages passed on captured bytes: 1.638s, 1.331s, 1.764s, 2.097s, 3.080s, 3.912s, 3.437s respectively.

Snapshot: run0035-story001-final. Digest: f9a6e45766f559485c94632396988e5c4a611f3e02dd76cc8e975a1bba9c8392. Check: story001-scoped-race-final. Checkpoint request: 5725b9a838e5e4dacf69ae2fcfd22d07. Complete valid preflight; matching receipt read: input valid, engineering recorded, acceptance not_applicable, effects pending. This is scoped engineering evidence, not acceptance or aggregate completion.

## Story-002 partial verification

Pure contract specialist added internal/buganalysis/analysis_test.go: 45 table cases passed, zero failed, 93.0% statement coverage using `go test ./internal/buganalysis -run '^TestBugAnalysis_' -short -race -cover`. Covers complete/partial structure, evidence and measurable criteria, malformed/duplicate identifiers and digest changes. This is partial verification only; storage, authenticated review, shared result, permissions and full wiring remain pending.

Story-002 storage specialist final current run: ten TestBugBoundary tests pass. Plain targeted store run 0.586s; race run 3.073s. Actual Bug workflow Run/Result bindings now used through ValidateResult plus public result/process APIs. Covers lifetime ledger, immutable analysis/replay, append-only authenticated answers, exact published owner approval including eighth-pass without new invocation, refusal matrices and approval history after supersession. Process/publication observations are controlled inputs, not live agent/MCP or host evidence. Typed downstream section preservation and runtime transport/continuation remain pending.

Story-002 pure shared-result tests passed with race plus existing mode create/update corpus, 49.7% package statement coverage. Twelve updated real-store boundary tests passed including typed downstream reference preservation, 12-way CAS single winner and forbidden agent execution fields (plain 0.751s; race 3.100s).

Three actual in-process AgentResultService boundary tests with nine refusal subtests passed in turn: preflight, submit, receipt lookup, exact replay/corrected identity, strict bindings, no terminal-only or receipt-only effect, blocked-without-analysis retention after public successful process/graph settlement. Final combined race turn 2.523s/store 3.615s after migration0069; plain turn 0.652s/store 0.630s. These use real SQLite/service and controlled process observations. Local broker-socket transport and runtime role permission proof remain pending.

## Final blocked-run verification

18 story-002 boundary top-level tests passed (no failures): `go test -race ./internal/turn ./internal/store ./internal/daemon -run '^TestBug(Result|Analysis)?Boundary' -count=1` (turn 3.429s, store 3.736s, daemon 4.003s). Real local Unix socket capability refusal is established. Controlled process fixtures cover success, resume/cold fallback, failure, cancellation, uncertainty and no-blind-relaunch. Exact pass-eight daemon approval adds no run.

Actual host sandbox probe: `/usr/bin/sandbox-exec` with a disposable synthetic file and read-only profile exited 71 before child launch: `sandbox-exec: sandbox_apply: Operation not permitted`. The unchanged canary is not denied-write evidence. No host enforcement pass, provider invocation or forge effect is claimed. Full scoped policy registration/review-publication journey and settled answers delivered through resumed analyst remain untested.

`go build ./...` passed. First `make check` passed vet then failed lint (63 findings); all production lint findings and one test errors.Is guard corrected in fix cycle 1. Pinned golangci-lint 2.13.1 reports zero issues. Default cache emitted permission warnings; rerun uses writable `/tmp/lf385-golangci-cache`. Final make check is still running its full race suite at this report update.

Final aggregate attempt 2 passed formatting/vet/lint but found four historical migration setup failures because fixtures invoked the current run allocator on old schemas. Test-only historical seed helper preserves all migration assertions; targeted race retest passed 12.673s. The old unbounded aggregate turn package exceeded the gate ceiling and its exact owned session was interrupted with Ctrl-C (exit130), so that aggregate attempt is incomplete. No unrelated/daemon process was touched. Final bounded commands make check TEST_TIMEOUT=8m and make test TEST_TIMEOUT=8m are running. No third fix cycle is planned.

## Final scoped current-byte result

Final ten-package race command selected all TestBug/TestWorkflowBoundary and the four historical migration fixtures, with -count=1. All passed on the final 92-file snapshot. Exact output:

```text
ok  	github.com/cloudvane/litefactory/internal/agent	7.191s
ok  	github.com/cloudvane/litefactory/internal/agentresult	9.236s
ok  	github.com/cloudvane/litefactory/internal/buganalysis	7.943s
ok  	github.com/cloudvane/litefactory/internal/graphexec	8.558s
ok  	github.com/cloudvane/litefactory/internal/teamsource	9.913s
ok  	github.com/cloudvane/litefactory/internal/harness	10.563s
ok  	github.com/cloudvane/litefactory/internal/codex	11.209s
ok  	github.com/cloudvane/litefactory/internal/turn	24.906s
ok  	github.com/cloudvane/litefactory/internal/store	76.446s
ok  	github.com/cloudvane/litefactory/internal/daemon	26.973s
```

Story001 snapshot run0035-story001-final-v3, check story001-scoped-race-final-v3, digest 58a2d56ead478e341bb9509efc4e1322d2c75dbf832b6a44002bd18b337a063c. Receipt8632c9b298ea91ba9e342641e2cb8b13 read: input valid, engineering recorded, acceptance not_applicable, effects pending. Earlier proof was explicitly invalidated before replacement; a preflight correctly refused replacement without invalidation, and was corrected before submission. Partial story002 code in the common snapshot is not a story002 engineering-completion claim.

## Final aggregate result — BLOCKED

Both requested final commands completed with exit 2:
- `GOLANGCI_LINT_CACHE=/tmp/lf385-golangci-cache make check TEST_TIMEOUT=8m`
- `make test TEST_TIMEOUT=8m`

Both commands timed out after eight minutes in the daemon package (`TestRetirementExecutionRealLifecycle`) and turn package (`TestEngineeringRejectsLegacyAcceptanceAndStaleSnapshots`). Both failed `internal/harness/TestRunStreamsEventsAsTheyArrive` because the init event was not on disk while the run was in flight. The separate make test run also failed `internal/mcpadapter/TestMCPServer_SizeBounds/full_contribution_plus_envelope` with a protocol response timeout. These failures are unresolved; their causes have not been established. No aggregate passing evidence is claimed. All owned gate processes have ended.

Formatting, build, vet, pinned lint, the final scoped ten-package race run, and the corrected store migration fixtures passed. Two permitted fix cycles addressed lint findings and historical migration fixture compatibility; final repository failures remain. The earlier unbounded gate was interrupted only through its owned session when it exceeded the gate ceiling, then rerun with the documented timeout.

Story-001 has current-byte scoped engineering recorded, not acceptance or aggregate completion. Story-002 remains partial and blocked: the disposable native sandbox probe failed before child execution with `sandbox_apply: Operation not permitted`, so no real provider denied-write observation was established. Full composed policy/review-publication and resumed settled-answer proof also remains incomplete. Stories 003–009 were not started because their dependency engineering is unfinished. No requirements were waived, no acceptance review performed, and no commit, push, merge, closure or publication is claimed.

## Run 0037 progress

Story-002 engineering completed. Scoped build and vet of internal/store, internal/turn, internal/daemon passed; formatting of six changed files passed. Tests and authentic host permission probes skipped under explicit owner instruction. Checkpoint request 01006a3778a491e5160e7175344c453f: input valid, engineering recorded, acceptance not_applicable, effects pending. Snapshot run0037-story002-engineering-v1.

Story-003 existing TestBug scoped suites passed in seven packages and scoped vet/format passed. New repair-specific verification and concrete source-observation completion remained pending at this checkpoint. Story-004 compile-only and scoped vet passed; concrete Linux bubblewrap backend authored, macOS unsupported and blocked safely. No aggregate gate has run in this continuation yet.

### Run0037 scoped engineering checkpoints through story005

Story003: repair/result/capability unit tests55 subtests and five SQLite/git boundary tests passed with race; source/scope/save implementation completed. Checkpoint e0d8f29c53ac1b1c188045f6da72733d recorded engineering, no acceptance.

Story004: nine recipe/executor unit tests plus six SQLite/git tests passed. Initial checkpoint invalidated when child integration found missing process reconciliation. Added kernel-identity launch barrier, no-signal restart reconciliation, durable cleanup proof and exact check-guard release. Six recovery tests passed with one Linux namespace test skipped on macOS. Restored checkpoint05fb7b7c60872ad21264f5ccc20ad960, snapshot run0037-story004-engineering-v2. Actual Linux host isolation/termination remains unverified; macOS runtime unsupported and blocked before launch.

Story005:59 pure graph/binding cases and10 SQLite/ownership cases passed. Actual BugTestChild.Advance test runs three retained dispatches across reopen, preserves attempts, avoids command relaunch, settles once and yields one parent wakeup; plain and race passed (race1.868s). Checkpoint4a6a8230fde612935df20aa5c40e3d05 engineering recorded. Daemon child dispatch and startup reconciliation implemented; no live daemon/host process proof claimed.

Acceptance remains separate. All checkpoint effect states pending. Story006 implementation underway;007–009 and aggregate gates not completed at this progress checkpoint.

### Stories006–007

Story006 five pure test functions and10 actual SQLite/controlled forge journey cases passed. Tests exposed and engineer fixed nullable-run handling for Bug factory publication; final path executes actual CompleteReportDelivery rather than seeded publication. Final focused race with existing graph/report compatibility passed7.188s. Engineering checkpoint3b846385b1ed0def4a951c84c8f50723 recorded.

Story007 proof matrix11, real SQLite/controlled forge journey10 and commit API4 cases passed race; store5.432s/github1.934s, turn no selected tests. Scoped vet passed. Merge intent, guard, uncertainty, ordered parents/main proof and pre-integration authorization timing covered. Engineering checkpointfa41ee22d0760961abc51c64f806d8df recorded. No live forge mutations or acceptance claimed. Story008 ongoing;009 and aggregate gates remain.


## Run 0039 — stories 008 and 009 verification recovery (2026-09-26)

Both required gates passed against unchanged final implementation bytes. No product code changed.

| Command | Exit | Duration | Evidence |
| --- | --- | --- | --- |
| `make check` | 0 | 495.594 seconds | `run0039/make-check.log.gz`, `run0039/make-check.result.json` |
| `make test` | 0 | 3.213 seconds | `run0039/make-test.log.gz`, `run0039/make-test.result.json` |

Both commands invoked `go test -race -timeout=0 ./...`. Make check passed formatting, vet, pinned lint (0 issues), and all test packages. The separate make test reused Go cache for most packages; graphexec executed again. Cache reuse is recorded as reported, not a second uncached suite. Linter cache write permission warnings did not fail lint.

The 232-file implementation snapshot SHA256 is `d67acde2af6c5d409e3a872acf519ea3a604b6279b3ea536e5c0f2b375767bc9`, identical before/after checks and to the retained final stories 002–007 snapshot. Story 008 AC-001–007 and story 009 AC-001–010 engineering evidence references these full-repository gates; this is not an acceptance assessment or a claim of new targeted test execution.

Story 008 checkpoint: snapshot `run0039-story-008-final`, checks `run0039-story-008-make-check`, `run0039-story-008-make-test`, receipt `ba5f994711ced103fd1648917d33648c`. Story 009 checkpoint: snapshot `run0039-story-009-final`, checks `run0039-story-009-make-check`, `run0039-story-009-make-test`, receipt `a688634045d20f3ef83448539268ed93`. Matching receipt reads report valid input, engineering recorded, acceptance not applicable and remote effects pending. Detailed receipt summaries are in run0039.

The earlier sandbox-test exception remains explicit: no new live provider permission, remote publication, integration or issue-closure proof is claimed. Skipped/waived host evidence remains unverified, never passing. Stories 001–007 remain satisfied dependencies. No acceptance review or mode transition occurred.

Historical receipt recovery: saved story-008 request `f820194b4bbc0b07ae0647bdc10b8806` had a saved rejected outcome; current scoped receipt lookup returned processing-incomplete and did not resolve the later unknown-ID request. That historical uncertainty is preserved. Current frozen authority expressly showed no retained story-008 engineering; this run submitted newly executed evidence with corrected current snapshot content after successful current preflight, not a replay of unknown prior bytes.

Aggregate snapshot `run0039-aggregate-final` will bind all final changed files, including these artifacts, with distinct check IDs `run0039-aggregate-make-check` and `run0039-aggregate-make-test`. The shared application result must reference it independently of capture acknowledgment. Factory revalidation, acceptance and remote effects remain separate.

The aggregate helper rejected a 274-file capacity probe. Thirty historical loose JSON/log artifacts were consolidated without data loss into `HISTORICAL_RUN_EVIDENCE.zip`; its `ARCHIVE_MANIFEST.json` records original names, sizes and SHA256. Each member was compared byte-for-byte before removing the loose copy. Extract members into this session to restore their original paths. Current full logs use verified lossless gzip.
