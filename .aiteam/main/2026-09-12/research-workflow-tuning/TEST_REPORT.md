# Verification

- PASS: `go build ./...` (nonfatal cache-write warning).
- PASS after final fixes: `gofmt -l .`, `go vet ./...`, pinned `make lint` (0 issues).
- PASS: engineer's focused source rendering, roster, snapshot, remote/parity and team checks.
- PASS after fixture correction: `go test -race -timeout=8m ./internal/config ./internal/cli`.
- PASS on focused race rerun: `TestRunTimeoutKillsTheWholeSubtree`.
- FAIL: required `make check`. Format, vet and lint passed; the race suite exposed an introduced CLI fixture mismatch (fixed and full package rerun passed), a transient harness fixture failure (rerun passed), and existing teamsource exact-prose parity failures. All other package results passed.

Remaining failures: `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`, `TestCodexSkills_SharedSourceParity`, `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`, `TestCodexSkills_AC003GuardClausesPreserved`. Engineer confirmed historical assertion mismatch predates this work; no unrelated instruction contracts rewritten.

Filesystem approvals were required for protected Codex edits, Go caches and test HTTP listeners. No daemon runtime changes or release publication performed.

## Research clarity implementation — 2026-09-12

Engineer focused harness/turn/teamsource tests passed. Independent unit validation passed six selected test functions, no failures:

- `go test ./internal/turn -run '^(TestResearchQuestionsAcrossProviderContinuation|TestPermissionHonorsModeAndOwnerControl|TestPlanQuestionPermissionCurrentAndPrevious|TestPreviousMayAsk|TestModeMayAsk)$' -short -race -cover -count=1`
- `go test ./internal/teamsource -run '^TestResearchStandaloneContract$' -short -race -cover -count=1`

Coverage includes Research/Plan defaults, other modes, explicit opt-out, quoted/fenced controls, previous triggers, both providers through cold/reply/resume-refusal/fallback, retained incomplete artifacts, standalone closure, native rendering and installed/native parity. Provider execution is stubbed.

`go build ./...` exited 0 with a nonfatal sandbox module stat-cache write warning. Initial sandbox `make check` reached vet but linter package loading failed; approved rerun passed formatting, vet and pinned lint (0 issues). Full race-suite outcome follows below.

Aggregate result: `make check` exited 2. Formatting, vet and pinned lint passed; full race-suite passed turn, daemon, store, Codex, CLI and other packages except harness and teamsource. Two obsolete harness assertion groups introduced by changed permission prose were corrected in `internal/harness/plan_continuation_test.go` and `prompt_test.go`. Focused race rerun passed: `go test -race ./internal/harness -run 'Test(PlanContinuationPromptAcrossPoliciesAndPaths|ColdPromptStillCarriesEverything|ResumedPrompt|RunTimeoutKillsTheWholeSubtree)' -count=1`. The unchanged subprocess-timeout fixture also passed on retry; its aggregate empty-pgid failure cause remains unproven.

Remaining full-gate failures are existing non-Research teamsource exact-handoff/shared-source parity/guard assertions: `TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly`, `TestCodexSkills_SharedSourceParity` (planning-contracts), `TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree`, `TestCodexSkills_AC003GuardClausesPreserved` (reviewer exit-as-acceptance). They were not broadened or disabled. Research replacement contract checks pass. Aggregate suite was not rerun after narrowly scoped assertion fixes. Full CI is not green.
