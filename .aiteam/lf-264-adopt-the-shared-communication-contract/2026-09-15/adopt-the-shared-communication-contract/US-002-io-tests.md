# US-002 persistent I/O test evidence

Frozen scope: US-002 / #299 / issue_id 5463751769 / parent_id 5439211198 / revision `02ab327e5ff1f1b431f024ee7dba52f6b1a55e40879e55a2164b369ceb3a3d6f`; dependency US-001 engineering satisfied. Workflow 2, Implement only.

## Result

13 top-level tests passed under the race detector, 0 failed (52 named passing tests including subtests). The six new persistence tests also pass together without race instrumentation. No production defect remains from these checks. Tests only changed; no acceptance review, forge action, commit, provider invocation or daemon-state mutation.

Environment: Go `go1.27.0 darwin/arm64`, Darwin `25.5.0 arm64`. Executed against uncommitted implementation based on `16634ed33075ea3acfc449bcb6316163bdc0b807`, including US-001 and US-002 implementation changes. This base hash alone does not contain the tested implementation. The orchestrator must capture the complete final worktree with its aggregate evidence.

Test file: `internal/turn/agent_result_persistence_test.go`
Test file SHA256: `0988c6925f2abcefd6abb2965e8ec5b1e412038d7821ab09a281b67331ffaf37`

## Executed commands

- `go test ./internal/turn -run '^TestResultPersistence' -count=1 -timeout=8m` — PASS, 18.420s. Complete output: `US-002-io-plain.log`.
- `go test -race ./internal/turn -run '^(TestResultPersistence|TestResultAggregateWorktree|TestAgentResultService_|TestAgentResultReader_)' -count=1 -timeout=8m -v` — PASS, 81.085s, 12 top-level tests / 51 named tests including subtests. Complete output: `US-002-io-race.log`. This invocation preceded addition of the all-engineered recovery test; the next invocation covers that addition.
- `go test -race ./internal/turn -run '^TestResultPersistenceAllEngineeredRecoveryRetainsScope$' -count=1 -timeout=8m -v` — PASS, 7.280s, one additional test. Complete output: `US-002-io-recovery-race.log`.
- `git diff --check -- internal/turn/agent_result_persistence_test.go` — PASS, no output.
- `gofmt -l internal/turn/agent_result_persistence_test.go` — PASS, no output.

## Coverage derived from frozen criteria

| Check | Criteria | Retained observations |
|---|---|---|
| TestResultPersistenceBaselineAndReceiptIsolation | AC-001, AC-002, AC-003, AC-004 | All five modes: explicit cold baseline, stable ID/revision, SQLite reopen/new-run baseline, unique historical receipt recovery, narrow receipt/replay projection with raw payload/snapshot withheld, unrelated scope refusal, replaced writer refusal, new-ID correction and atomic stale revision with authorized correction projection. |
| TestResultPersistenceProductionLaunchBaselines | AC-001 | Actual runMCPBound/prepareMCP constructor and controlled provider boundary: all five modes, MCP on/off, cold/resume/fallback sequence. Each continued managed launch gets a new authorized run/session. Prompts carry observed full authorized baselines with stable ID and revisions 0/1/2. Validate receives no helper filesystem grant. |
| TestResultPersistenceRetiredIDsAndAmbiguousReceipts | AC-002, AC-003, AC-004 | Deleted collection IDs survive reopen/rebinding and cannot be resurrected. Narrow baselines hide sibling retired IDs. Reused request IDs in different historical runs refuse ambiguous lookup; explicit source-run lookup succeeds. |
| TestResultPersistenceAggregateBrokerAndFinalEvidence | AC-003, AC-004, AC-005 | Actual prepareMCP composition and real Unix socket helper/SQLite/worktree: preflight creates no aggregate, submit retains capture/receipt without result revision or workflow effects, capture alone cannot finalize, read-only reopen replays exact durable capture receipt, changed bytes cannot reuse ID, fresh resolver restores exact submission check/snapshot IDs, wrong generation grants no proof, aggregate-only completion passes preflight/submit/final, sparse comment changes revalidate untouched stale aggregate bytes, final enforcement rejects post-submit worktree changes and conflicted checkpoints. |
| TestResultPersistenceAggregateCaptureRejections | AC-003, AC-005 | Production-composed service rejects factory identity injection, unknown/null/duplicate fields, wrong file hash, missing check coverage, scoped story-check reuse as aggregate, stale worktree bytes. Rejected receipts retain no aggregate. |
| TestResultPersistenceAllEngineeredRecoveryRetainsScope | AC-001, AC-005 | Fresh all-engineered session has zero writable checkpoint stories. Actual production constructor restores the full approved result scope, launch includes exact saved proof references, shared helper captures aggregate, terminal aggregate-only completion succeeds without fabricating a story event. |
| Existing TestResultAggregateWorktree_* | AC-004, AC-005 | Executed actual temporary Go fixture check, immutable aggregate/checkpoint recovery, legacy/separated versions, wrong selected set/revision, missing snapshot/check, failed/unexecuted proof, omitted file/symlink/stale bytes, conflicted checkpoint, Validate capture refusal. |
| Existing TestAgentResultService_* / TestAgentResultReader_* | AC-001–AC-004 | Persisted five-mode continuation, request replay, input/engineering/acceptance/effect separation, frozen scope changes and retained evidence refresh, read-only reopen authority. |

## Bounds and remaining ownership

Temporary migrated SQLite, local Unix sockets and temporary Git worktrees are real I/O. The aggregate fixture actually executes its tiny local Go test; story engineering checkpoints use the repository's existing synthetic retained-check fixtures. These tests establish wiring and rejection behavior, not authentic agent-reported execution. No controlled HTTP call is needed for US-002; external publication recovery belongs to US-003.

No actual Claude/Codex host invocation, same-run native correction, production deployment, live runtime cohort drain or remote publication is established. #274/#296 authentic captures remain unsatisfied. Unit specialist owns pure copy/shape/full-merge tests. Final full-repository `make check` belongs to the orchestrator and was not run here.

Two initial test-fixture mistakes were corrected before the retained passing runs: pending effects were initially compared after deliberately conflicting a checkpoint (the conflict legitimately changes effects); managed continuation initially reused a revoked session attempt (the test now allocates a fresh run/session). Neither required production changes.
