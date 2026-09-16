# US-009 executed compatibility checks

**Status: BLOCKED — compatibility regressions pass; retirement prerequisites remain unmet.**

Frozen identity: #306 / US-009; issue ID 5463756074; parent ID 5439211198; revision `ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7`; dependencies US-001 and US-002; workflow 2.

## Environment and implementation state

- Executed 2026-09-15T18:46:26.308978+00:00.
- Go 1.27.0, darwin/arm64; repository temporary SQLite databases and local git/filesystem fixtures; controlled forge/runner test doubles at external boundaries. No live provider or forge calls.
- HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus run-0007 uncommitted changes.
- Complete Go/module/migration file hash inventory: `US-009-test-source-manifest.json`; SHA256 of sorted path + NUL + SHA256 + newline entries: `9e9223f0007940f0737a66fff29fce8a07a4c6a71f82d920f85956f709ebb123`. This inventory was unchanged across both executions. It is a source-state identifier, not a factory submission capture.
- No production code or test files modified; existing tests were sufficient. No external database environment variables required by these self-contained fixtures.

## Executed checks

### US009-compatibility-plain

20 top-level tests passed (42 passing test/subtest records), 0 failed; exit 0.

```sh
go test ./internal/store ./internal/turn -run '^Test(SubmissionMigrationPreservesLegacyVersions|EngineeringMigrationPinsExistingFeaturesThroughRestartAndReplan|ContractUpgradeAtomicGenerationPreservesHistoryAndLocksAdmission|ContractUpgradeSafetyRefusesWithoutChangingLegacyScope|SubmissionRestartRetainsDiagnosticsAndPendingEffects|StoryContractUpgradePreviewAndExplicitApply|StoryContractUpgradeRestartResumesOriginalCohort|StoryContractUpgradeNextTypedPlanRetainsCompletedLegacyDependency|StoryContractUpgradeDispatchKeepsFrozenGenerationContract|StoryProgressResponseLossAndInvalidationSurviveNewService|StoryProgressPendingCloseBlocksReplanUntilDelivered|StoryProgressUnreadInputPreventsPendingClose|EngineeringRestartPreservesProgressWithoutClosure|SubmissionLegacyDecisionRecovery|TypedPlanSubmissionBoundArtifactsAndPendingReadiness|TypedPlanRunnerPublicationAndDurableReceipt|PlanFailureRetainsTransportForRetry|ManagedValidateRetainsFrozenEvidenceOnEveryAdapterPath|ManagedValidateRejectsProcessSuccessWithoutStructuredOutput|ValidationCheckpointReconcilesTerminal)$' -count=1 -timeout=8m -v
```

```text
ok  	github.com/cloudvane/litefactory/internal/store	2.369s
ok  	github.com/cloudvane/litefactory/internal/turn	32.633s
EXIT_CODE: 0
```

Full passing output: `US-009-compatibility-plain.log`.

### US009-compatibility-race

20 top-level tests passed (42 passing test/subtest records), 0 failed; exit 0.

```sh
go test ./internal/store ./internal/turn -run '^Test(SubmissionMigrationPreservesLegacyVersions|EngineeringMigrationPinsExistingFeaturesThroughRestartAndReplan|ContractUpgradeAtomicGenerationPreservesHistoryAndLocksAdmission|ContractUpgradeSafetyRefusesWithoutChangingLegacyScope|SubmissionRestartRetainsDiagnosticsAndPendingEffects|StoryContractUpgradePreviewAndExplicitApply|StoryContractUpgradeRestartResumesOriginalCohort|StoryContractUpgradeNextTypedPlanRetainsCompletedLegacyDependency|StoryContractUpgradeDispatchKeepsFrozenGenerationContract|StoryProgressResponseLossAndInvalidationSurviveNewService|StoryProgressPendingCloseBlocksReplanUntilDelivered|StoryProgressUnreadInputPreventsPendingClose|EngineeringRestartPreservesProgressWithoutClosure|SubmissionLegacyDecisionRecovery|TypedPlanSubmissionBoundArtifactsAndPendingReadiness|TypedPlanRunnerPublicationAndDurableReceipt|PlanFailureRetainsTransportForRetry|ManagedValidateRetainsFrozenEvidenceOnEveryAdapterPath|ManagedValidateRejectsProcessSuccessWithoutStructuredOutput|ValidationCheckpointReconcilesTerminal)$' -count=1 -timeout=8m -v -race
```

```text
ok  	github.com/cloudvane/litefactory/internal/store	19.490s
ok  	github.com/cloudvane/litefactory/internal/turn	75.588s
EXIT_CODE: 0
```

Full passing output: `US-009-compatibility-race.log`.

## Coverage and preservation

- Migration and historical contracts: old database migration, workflow versions 1/2, completed legacy dependency retention, atomic generation history and admission locks.
- Restart/recovery: durable diagnostics and pending effects, explicit upgrade/restart with test doubles, StoryProgress lost-response/invalidation recovery, unread-input and pending-close safeguards, separated engineering without premature closure, legacy submission decision recovery.
- Preserved current routes: typed Plan artifacts/publication/receipts and failed-attempt retry; reference-only frozen Validate context across adapters, missing structured-output refusal, live/terminal assessment reconciliation.
- Retained reader source references: `internal/turn/plan_handoff.go:55` and `:115` prepare/publish rooted Plan file transport; `internal/turn/story_progress.go:72` and `:232` register/ingest ordered event files; `internal/turn/validation.go:91` retains `agent.ParseValidationOutput` legacy harvest path. No deletion performed.

## Unverified obligations

AC-006 regression portion has executed passing evidence. AC-001 factual live cohort/effect inventory and AC-002 required authorized upgrade/drain receipts remain unavailable. AC-003/004/005 removals remain unperformed and blocked. Fixtures cannot establish live drain or authentic #274/#296 host behavior. See `US-009-retirement-disposition.md` and `US-009-retirement-followup.md`; no scope revision is approved and no engineering_complete contribution is justified. Aggregate gates remain with the orchestrator.
