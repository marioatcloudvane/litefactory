# Implement run 0006 test report

Only root-executed commands from this run supply evidence. Previous run execution claims and outputs are not reused. No production or test source edits were required. US-001 is a supplied satisfied read-only dependency and was not resubmitted.

## Fresh executed checks

- `go build ./...`: exit 0, silent compiler output. `run0006-executed/build-result.json` records the actual command, timestamps and exit.
- US-002 focused `-race -count=1 -timeout=8m` checks: exit 0; daemon 112.089s, git 30.609s. Selection covers execution/retry/storage failure, canonical coordination, logging, exact-head races, clone safeguards and worktree recreation. Exact executed command and actual output: `run0006-executed/us002-tests-result.json` and `us002-tests.log`.
- US-003 focused `-race -count=1 -timeout=8m` checks: exit 0; daemon 47.331s, cli 1.883s. Selection covers daily/restart lifecycle, budgets/backoff/shutdown/storage failure and operator help. Exact command and output: `run0006-executed/us003-tests-result.json` and `us003-tests.log`.
- Root executed `git --version`: git version 2.50.1 (Apple Git-155). Feature fixtures exercise real temporary Git and SQLite, with controlled forge responses; they do not claim hosted availability or production deletion.

## Single aggregate gate: FAILED, owner exception applied

Root executed one `make check`, which completed with raw exit 2 after 1234.648813 seconds. Formatting, vet and pinned golangci-lint v2.13.1 succeeded; lint reported zero issues. Every tested package except `internal/teamsource` passed. Relevant aggregate timings: daemon 782.708s, git 179.365s, store 325.780s, turn 1145.872s and cli 351.264s.

Only these four aggregate top-level teamsource tests failed:

- TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly
- TestCodexSkills_SharedSourceParity
- TestCodexSkills_StructuredProtocolHandoffScopedToCodexTree
- TestCodexSkills_AC003GuardClausesPreserved

These fall within the owner's explicit pre-existing instruction-parity exception. `TestRunTimeoutKillsTheWholeSubtree` did not fail; its separate owner exception was not needed. The aggregate remains FAILED with an applied owner exception, never a fabricated pass. Exact raw output and exit metadata are preserved in `run0006-executed/make-check.log` and `make-check-result.json`.

## Factory engineering evidence

Root independently verified story-prefixed unique check references, exact equality to fresh logs, decoded snapshot bytes against current files and the sorted manifest digest before each contribution. Both preflights returned valid and complete with no diagnostics.

| Story | Request | Engineering | Remote effects |
|---|---|---|---|
| US-002 | a6a476067d4bf4b591430752b8c1c94d | recorded | delivered |
| US-003 | 895d43cf20f5ca7cd1b4078372efcb07 | recorded | delivered |

Check references are `us002-build-r6`, `us002-scoped-r6`, `us003-build-r6`, and `us003-scoped-r6`. The failed aggregate is documented here and is not included as a passing capture check. Actual preflight, submission and receipt outputs are under `run0006-executed`; the final US-003 observation is `us003-receipt-final.json`.

Engineering execution is complete under the precise owner exception. Acceptance was not assessed; no code/architecture/plan review or factory save/commit/push is claimed. Root's final checks passed: gofmt -l . returned exit 0 with empty output; both decoded submitted snapshots and SHA256s match final current files; all four check references are unique and story-prefixed. Final git status showed only session reports and run0006 evidence artifacts changed, with zero source/test edits.
