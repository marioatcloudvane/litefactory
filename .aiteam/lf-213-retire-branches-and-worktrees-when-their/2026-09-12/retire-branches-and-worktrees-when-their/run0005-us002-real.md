# Run 0005 US-002 real-dependency execution

Story: US-002 / #256 / issue ID 5433203226. Frozen revision: `74f54d211ddd90390ea775cb7a7729a273c749aa6834059558afacb38ddefed9`. Dependency: US-001. Existing implementation and tests reused; no production or test sources changed by this worker.

## Executed checks

- `git --version`: exit 0, git version 2.50.1 (Apple Git-155).
- `go test -race -count=1 -timeout=8m ./internal/daemon -run '^TestRetirementExecutionReal' -v`: exit 0, package 114.978s, wall 118.213s. Eight top-level tests passed, zero failed.
- `go test -race -count=1 -timeout=8m ./internal/git -run '^(TestRetirementExactHeadRaces|TestCloneMethodSetIsTheAllowlist|TestCloneCannotMutateTheRegisteredClone|TestRetirementCapabilitySignatures)$' -v`: exit 0, package 8.044s, wall 9.512s. Four top-level tests passed, zero failed.

Total: 12 top-level tests passed, 0 failed, with 21 nested subcases passed. Full actual output: [run0005-us002-real.log](run0005-us002-real.log).

## Criterion-attributed observations

| Criterion | Executed evidence |
|---|---|
| AC-001 | Lifecycle final reopen/unavailable and active/owner lock cases; real parent/clone coordination and cancellation; shared-child case. HTTP fixture asserts no conditional closure requests. |
| AC-002 | Real lifecycle removes worktree then branches, conserves ignored/untracked files and unpushed commits, and checks registered clone unchanged. Additional guard-specific cases are outside this worker selection. |
| AC-003 | Local compare-and-delete and remote receive-pack race probes rejected concurrently advanced tips. Exact rejection output retained. Remote-only lifecycle case passed without clone checkout changes. |
| AC-004 | Real intent/outcome storage failures prohibit success claims, preserve uncertain intent and ownership; uncertain retry case passed. |
| AC-005 | Missing/stale registration and stale intent lifecycle cases; uncertain retry, recreated checkout, intent path isolation cases passed. |
| AC-006 | External closure, abandonment, open merged issue, missing upstream, active/shared child and local-only preservation lifecycle cases; surviving remote branch reopening and recreated checkout cases passed. |
| AC-007 | This real-dependency selection establishes action outcomes; log-level/field assertions belong to the separate assigned logging unit check, not this report. |
| AC-008 | Clone allowlist, clone mutation exclusion, retirement signature checks and actual exact-head race probes passed; these are executed engineering tests, not a Validate review. |

## Bounds

Fixtures use temporary bare Git origins/worktrees, migrated SQLite and local httptest servers. No production deletion or live forge requests were performed. Scratch race probes establish installed Git rejection semantics, not hosted GitHub availability. No new build tags, external database/service requirements, or nested agents. Existing tests were executed once with race detection as requested; no whole-package reruns or fresh plain duplicates. Aggregate gate and engineering submission remain orchestrator-owned.
