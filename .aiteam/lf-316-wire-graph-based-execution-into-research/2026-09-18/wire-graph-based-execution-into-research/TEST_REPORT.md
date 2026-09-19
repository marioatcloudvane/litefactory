# Engineering test report — run 0014

Overall: scoped checks passed; aggregate `make check` incomplete. No acceptance is claimed.

| Scope | Executed check | Result |
| --- | --- | --- |
| #315 prerequisite | Scoped race tests, build, vet, formatting and exact retained-byte comparison | Passed; FOUNDATION_INTEGRATION.md |
| #335 unit | Catalog capture/reference/default/closure tests and permitted DOT scope | Eight top-level tests and 21 table subcases passed; UNIT_TEST_research-catalog.md |
| #338 unit | Factory graph identity and strict Research node decoding | Five test functions and 53 subcases passed |
| #335/#338 real boundaries | Selected native resources, real artifact confinement, migrated SQLite and fake runner | Three tests and 19 subcases passed, plain and race-enabled |
| Fresh scoped retest | Catalog, node request/decode and filesystem/store tests after lint-only edits | All passed; LINT_FIX_REPORT.md |
| Initial `make check` | Format, vet, pinned lint | Stopped at 15 lint findings before aggregate tests; corrected in fix cycle 1 |
| Corrected `make check` | Format, vet, pinned lint, repository race suite | Format/vet/lint passed, 0 lint issues. Race suite unfinished at roughly ten-minute ceiling; owned session interrupted, exit 130 |
| `go build ./...` | Entire module build | Passed; exit 0 |
| `gofmt -l .` | Entire worktree formatting | Passed; no output |
| Authentic host evidence | #338 AC-407 and later rollout observations | Not performed; separately authorized external operator capture required |

The corrected command was `GOLANGCI_LINT_CACHE=/private/tmp/lf316-golangci-cache make check`. The repository target invoked `go test -race -timeout=0 ./...`. At interruption, successful package output was retained through cli/codex/config/confirm; remaining package results were not available. No failed assertion was reported, but unfinished execution is not a pass. AGGREGATE_CHECK_OUTPUT.txt preserves actual output and termination cause. The whole suite was not restarted.

Executed boundary command:
`go test -race ./internal/turn -run 'TestResearch(NativeBundleFilesystem|ArtifactFilesystemConfinement|AgentRealStorePrivateRetention)$' -count=1 -v`
Initial direct run: PASS, turn package 20.687s. A fresh affected retest after lint fixes also passed.

Executed unit commands:
`go test ./internal/teamsource ./internal/graphexec -run 'TestCaptureResearch|TestGraphShippedDOTScope' -short -race -cover`
`go test ./internal/agent ./internal/agentresult -run '^(TestRequest_ResearchNodeIdentity|TestDecodeResearchNode_)' -race -cover -count=1`

No offline check substitutes for actual-host observations. No passing aggregate proof or downstream-story completion is captured.


## Implement run 0015 — dependency gate (2026-09-19)

No new implementation or test commands executed. The supplied frozen state retains #335 as engineering complete and #338 as blocked, with no retained engineering snapshot/checks for #338. Its offline implementation and prior scoped tests do not establish the missing authentic Claude/Codex helper, same-run correction and authoritative receipt observations required by AC-407. This attempt supplies neither observations nor external capture authority.

All nine requested stories depend directly or transitively on #338. Their engineering dispatch gates therefore remain closed. No aggregate gate was rerun: the last selected story has not landed. Prior passing scoped evidence and the unfinished run 0014 aggregate gate remain historical facts, not newly executed proof. Acceptance and rollout remain unverified.

## Run 0017 — #338

Strict node decoder and request identity unit tests passed with race instrumentation. The unit specialist added malformed input cases. Real SQLite, filesystem confinement, atomic retention, stale-attempt protection and zero-publication boundary tests passed in normal and race runs (4 top-level tests / 20 subtests). Scoped engineering snapshot `research-node-results-524-local-1`, checks `node-unit-524` and `node-boundary-524`, was recorded. Authentic AC-407 observations remain unverified; these are offline fixtures. Matching receipt: `5124dfd7fad850eb0c5d85cdc9e694cd`; input valid, engineering recorded, acceptance not_applicable, effects pending.

## Run 0017 — #336 and #339

#336 main-role cold/resume/fallback and retained delivery identity: six test functions passed with race instrumentation; receipt `0524455b02e074c7a14f5558471035c9`. #339 typed loop/if/default/unsafe-branch unit checks passed with race instrumentation, plus three real SQLite tests covering four retained-routing subcases, cold reopen, failed/successful continuation limits and instance isolation; receipt `95df646ed7dc3d99332e25f3d20f632c`. Both checkpoints: input valid, engineering recorded, acceptance not_applicable, effects pending. No authentic host observation is claimed.

## Run 0017 — #337, #340 and #345

#337 four scoped fake-process/delivery test functions passed with race instrumentation, including instruction encoding and invalid source rejection. Snapshot research-codex-main-524-local-1; receipt `1b334fd9be437b95bcd569271c446e22`.

#340 nine declaration unit cases and six real HTTP/SQLite boundary cases passed, including lost acknowledgment, correction identity, report-only recovery and original failure preservation. Snapshot research-comments-524-local-1; receipt `df9156471f7d4cdfc9aa656ca6500264`.

#345 three pure projection/CSV test functions plus seven boundary/presentation tests passed with race instrumentation. Actual invocation identities remain distinct through compatible resume, refusal/cold fallback and different nodes; unknown counters remain unknown. Snapshot research-usage-524-local-1; receipt `a1d90396759801524007cc7b6e3dd988`.

All three receipts: input valid, engineering recorded, acceptance not_applicable, effects pending. Authentic host evidence remains unverified. Final shared-file capture refresh and aggregate checks remain pending.

## Run 0017 — #341

Owner-reply early-validation/migration tests passed with race instrumentation (two test functions,13 admission cases), plus two real HTTP/SQLite turn/daemon tests with eight rejection subcases. These establish pending-publication refusal, durable wait, authenticated correlation, claim/allocation rollback and restart, bounded saved context and unrelated due-work selection. Snapshot research-owner-resume-524-local-1; receipt `833cd70a243fb70aef1d02da70a5f2a2`; input valid, engineering recorded, acceptance not_applicable, effects pending. Actual assembled scheduler/provider resume awaits final workflow story.

Recovery engineer reported passing targeted checks and vet; one additional background retirement/continuation command did not provide a retrievable terminal result and is not counted as passing. Dedicated recovery boundary checks are pending.

## Aggregate regression corrections

First make check stopped at57 pinned-lint findings; one correction cycle fixed them and six-package scoped Research tests passed. Second run passed vet/lint but full race tests exposed strict config import-purity and nil injected-clock startup regressions. The second correction cycle moved YAML decoding into config.go without weakening purity and added clock fallback while retaining graph reconciliation. Targeted race purity/legacy startup and Research startup/owner claim tests passed. All affected unaccepted captures were invalidated and replaced. Full final aggregate execution remains pending; no interrupted or failing gate is counted as passing.


Run0017 aggregate follow-up: prior full race attempts completed with strict config YAML-placement, nil/extra-clock, closure error-context, and obsolete no-production-graph-caller failures. These were corrected; narrowed clock/closure/mode/store and structural/default/mode-isolation race checks pass. Final delivered make check is running, output /private/tmp/lf316-make-check-delivered.log. Final go build ./... passed after all fixes. No passing aggregate claim yet.


## Run0017 final aggregate PASS

`GOLANGCI_LINT_CACHE=/private/tmp/lf316-lint-cache make check` exited0 against the final delivered source/test snapshot. Formatting and vet passed; pinned golangci-lint2.13.1 reported0 issues; `go test -race -timeout=0 ./...` passed every package, including CLI347.963s, daemon902.391s, store522.444s and turn2307.954s. Exact output: RUN17_AGGREGATE_PASS.txt. Final `go build ./...` passed after all source fixes. Previous failed attempts above remain historical, not current failures. No authentic-host evidence or acceptance decision is inferred.
