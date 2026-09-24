
## Implement run 0006

Selected all three factory-frozen stories, in dependency order. Workflow 2: engineering only; acceptance remains for explicit Validate. No live forge requests, deployment or daemon state changes are part of implementation.

Assumptions: existing workflow JSON/history can retain lifecycle state without an unnecessary schema migration. Boundary verification uses repository-owned temporary SQLite databases and controlled HTTP fixtures explicitly required by the frozen stories; external TEST_DATABASE_URL/TEST_BASE_URL are unnecessary for those fixtures. Missing docs/story-contract-instruction-manifest.md does not remove supplied frozen-scope or submission obligations.

### Lifecycle engineering checkpoint

Story adopt-child-lifecycle: snapshot run592-lifecycle-1; check lifecycle-scoped-race-1; digest f406c198218a73a56a0ac57518ebfa2c1bf8ddb4d6a29dc6da5efeb5db031711. Request 6cfaf44949cd597eebe8499b19efdab8. Complete preflight clean; matching receipt read: input valid, engineering recorded, acceptance not applicable, remote effect pending. Recovery dependency released by engineering; no acceptance or publication claimed.

### Recovery engineering checkpoint

Story recover-story-effects: snapshot run592-recovery-1; check recovery-scoped-race-1; digest 4c076577165e584618fc146582af70c9d673e0baf224d7f4c12d31b7dc74b8af. Request 9d33043cd1153758573b340ef864f65a. Complete preflight clean; matching receipt read: input valid, engineering recorded, acceptance not applicable, remote effect pending. Boundary tests exercise production daemon admission/recovery/discovery helpers with reopened SQLite and controlled HTTP; complete Tick scheduler assertions remain for reporting story.

Execution accommodation: new-agent creation reached its thread limit before story three; the completed recovery engineer received a new bounded story-three assignment. This reuses capacity, not prior proof or requirements.

### Final-byte scoped checkpoints

Superseded shared-file evidence was explicitly invalidated before recapture. Pinned-lint fix cycle 1 was coordinated; no substantive scope change. Final digest: d348f20f18808e2bf353fedb18f780f8c6acadec693ea4ea77152981c2133297.

- adopt-child-lifecycle: run592-lifecycle-final2 / lifecycle-scoped-final2; request 9cc8506268b3e91536e395e24d7438b1.
- recover-story-effects: run592-recovery-final2 / recovery-scoped-final2; request 0df02ef25cc72f3e1ba5186901a1e090.
- report-and-verify-plan-progress: run592-reporting-final2 / reporting-scoped-final2; request 81d208f3358f9a60839e002b3fc0a573.

All preflights complete and clean; all matching receipts read: input valid, engineering recorded, acceptance not applicable, remote effects pending. Aggregate proof remains separate and pending.

## Aggregate fix cycle 2 — pre-existing runtime packaging drift

The completed full race run passed every package except internal/teamsource: TestPlanPackageStageContractsAndResearchPreservation and TestPlanPackageClosureAndDigest. Both canonical teams/source/plan/plan-story.md (1,055 bytes) and embedded internal/teamsource/plan/plan-story.md (4,579 bytes) matched HEAD before this work, but differed. The embedded-only rewrite lacked seven required contract phrases. Existing plan.go/docs identify teams/source/plan as canonical and internal/teamsource/plan as its explicit runtime mirror.

The narrow repair copied canonical bytes unchanged to internal/teamsource/plan/plan-story.md. No canonical source, test expectations or protected instruction trees changed. Targeted parity/contract race checks passed (2.150s). This additional runtime-copy repair is necessary for the approved aggregate-check requirement, with no new behavior/scope policy designed. Previously recorded engineering was explicitly invalidated for final-byte rebinding.

### Final runtime-copy snapshot and checkpoints

Final digest: 6cf2b6aaed38d385634bee8a3efa06a4f1b7fa90aeebae97b7a03800490a8782 (41 files, including canonical/runtime Plan story pair).

- adopt-child-lifecycle: run592-lifecycle-final3 / lifecycle-scoped-final3; request 49c04e1836fa30091c32ee04755b283d.
- recover-story-effects: run592-recovery-final3 / recovery-scoped-final3; request 74b323bad75a34bfad0f01f86d8c47e7.
- report-and-verify-plan-progress: run592-reporting-final3 / reporting-scoped-final3; request 80a72ff0e3e2908657b0c7b532852f6f.

Complete clean preflights and matching receipts: input valid, engineering recorded, acceptance not applicable, effects pending. The broad post-repair scoped run exceeded its eight-minute suite bound during an actively running three-second SQLite migration setup; it is retained as a timeout, not a pass. Narrow changed-story and package-parity race selection passed: planbrief 2.179s; report 2.734s; teamsource 3.545s; turn 78.374s; daemon 155.981s. Final full make check is pending; no tests removed or weakened.

## Run 0007 aggregate recovery

All three stories are read-only engineering-complete dependencies under the factory-supplied frozen revisions. No story events or implementation edits are authorized or needed. The 41-file manifest exactly matches retained digest `6cf2b6aaed38d385634bee8a3efa06a4f1b7fa90aeebae97b7a03800490a8782`. Existing session reused; missing docs/workflow/session.md does not prevent aggregate execution. Acceptance and save/push remain separate factory/owner operations.

Aggregate preflight requires every final changed worktree path, including the session reports. Final aggregate capture therefore includes those artifacts alongside the unchanged 41 implementation files. Scoped story evidence remains unchanged. Actual preflight/submit/receipt state is reported in the final application response; artifacts are frozen before capture to preserve exact bytes.
