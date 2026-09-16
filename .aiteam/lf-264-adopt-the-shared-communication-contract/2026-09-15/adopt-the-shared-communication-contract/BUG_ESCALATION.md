# Aggregate engineering escalation — run 0007

## Where

Implement aggregate verification, after two permitted fix/retest cycles. No acceptance review occurred. The worktree remains uncommitted for the factory.

## Results and remaining failures

Build, gofmt, vet and pinned golangci-lint v2.13.1 passed. Full CLI and daemon plain suites passed. Most other package suites passed; teamsource failed. The full turn plain suite reported failures and timed out at eight minutes. The make check race target was bounded externally to 480 seconds and did not complete; it is not passing evidence.

| Test / location | Observed result | Required follow-up |
|---|---|---|
| TestCodexMCPLifecycleEnabledDisabledAbsentSetup; TestCodexMCPLifecycleMalformedQuotedValuesSurvive; TestCodexMCPLifecycleServerCollisionRefusesSafely; TestCodexMCPLifecycleConcurrentCancellationIsolation — internal/turn/mcp_codex_lifecycle_test.go | Existing fake runner outcomes lack required current-attempt final output and now fail finalization. | Reconcile fixtures with the adopted final contract, retaining original launch/quoting/collision/cancellation guarantees; do not weaken required final output. |
| TestMCPFamilyCompleteCatalogPipeSocketHTTP — internal/turn/mcp_family_catalog_test.go | Research/Implement/Validate cases report missing family outcomes; Plan/Spike expected catalog omits new activity tools. | Determine intended receipt semantics for read/advisory operations and update coverage or fix production behavior without weakening receipt authority. Update intended activity catalog expectations. |
| TestBothInstructionTrees_SharedSourceParity — internal/teamsource/aggregate_instruction_parity_test.go | Claude workflow paragraphs/order and Codex workflow/review content diverge. | Repair owned Claude paragraphs, then independent Claude-harness Codex handoff. |
| TestBothInstructionTrees_StructuredProtocolHandoffPointsAtOwnTreeOnly | Claude go-code-reviewer lacks exact own-tree handoff. | Restore required restricted handoff in .claude/agents/go-code-reviewer.md. |
| TestResearchStandaloneContract — internal/teamsource/research_contract_test.go | Claude Research differs from neutral native behavior and includes provider dependencies. | Restore standalone/provider-neutral Research contract without widening tools. |
| TestCodexSkills_SharedSourceParity | Codex workflow, review-evidence and four Plan skills remain stale. | Execute frozen US-007 under Claude; exact paths in US-007-harness-handoff.md. |
| Full turn suite | Timed out after 8m while many parallel tests were still queued. | Use bounded partitioned checks on the next attempt; do not claim the interrupted suite passed. |

Exact failing output: aggregate-turn-plain.log and aggregate-other-plain.log. These observations are test results, not a completed code/acceptance review.

## Fix cycles already used

1. Corrected 13 lint findings in production and test code. Scoped tests/vet passed; subsequent pinned lint reported zero issues.
2. Refreshed old shared-instruction fixture pins and byte totals, preserving typed-result equivalence. Five scenarios passed with race; companion documentation consistency passed.

The implement-orchestrator skill explicitly limits aggregate repair to at most two fix/retest cycles. No third cycle was attempted. Current affected engineering checkpoints were invalidated; historical executed captures remain retained. No acceptance was changed.

## Additional prerequisites

US-007 requires the unavailable Claude harness for protected .codex targets. US-009 requires factory-supplied factual cohort/pending-effect evidence and supported authorized upgrade/drain receipts before deleting Plan v1, Implement event-file or Validate v1 input. Retained fixture checks do not establish those facts. A full retirement follow-up and proposed scope revision are prepared, not approved or published.

## Next action

Resume Implement to resolve the exact test failures and refresh final-state evidence; separately select Claude for US-007 and obtain factory runtime retirement evidence or an explicit owner scope decision. Preserve the current partial work. Validate is not ready and is not auto-dispatched.
