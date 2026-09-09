# Code Review — US-1

Feature: a-failed-run-s-uncommitted-work
Reviewed by: go-code-reviewer
Date: 2026-09-09
Status: CLEAN

Selected scope: US-1, GitHub issue #180, issue ID 5397106908, revision `86c27e70342988d857c60140bc617dc57b10e088b8451cdea11e53aeca356e30`; dependencies: none. The supplied immutable body and shared plan brief governed review.

## Blocking findings

None.

## Advisory findings

None.

## Acceptance review

- **AC-171-1:** Dirty observation occurs under the existing preparation gate, after explicit policy decisions and before merge preview. No staging, parking, cleanup, reset or task-branch update occurs in deferral. `--no-optional-locks` prevents Git status from refreshing raw index metadata. The real-git lifecycle test checks retained HEAD, raw index, staged and unstaged content on one file, executable mode and untracked bytes at runner entry; a separate stat-refresh fixture targets metadata mutation.
- **AC-171-2:** Success-only harvest is unchanged. Publication still precedes saving, and its error blocks saving. The lifecycle covers repeated failure, timeout and abandonment without task commits, later successful saving, and a subsequent clean synchronization. Existing save/publication regressions supply composed policy coverage; US-3 adds the dedicated end-to-end Plan publication-failure retry test. The fixture now establishes a real successful no-edit conversation before failed work, because Codex persists resume identity only after success; no path or preservation assertion was weakened.
- **AC-171-3:** Turn, store and report use `deferred_dirty` consistently. Store accepts the outcome, preserves divergence and rejects a sync SHA for it. Shared bounded prompt context reaches both adapters and is retained by the fallback value copy. Adapter stdin tests cover cold, resumed and fallback paths; the lifecycle exercises actual fallback reconstruction. Owner disclosure states that the base was not merged and does not repeat the existing failed-run worktree location or claim earlier work succeeded.
- **AC-171-4:** Up-to-date, disabled and distance-cap decisions precede dirty observation. Clean conflict preview and guarded merge behavior remain unchanged. Status inspection errors retain the underlying `git.CommandError` and prevent launch. Admission, agent-switch, managed approval and automatic-continuation checks remain independently wired; existing scoped regressions cover these guards.

## Scoped validation

The test specialist reported the settled race-and-coverage scoped turn suite passing in **76.187s**, with **39.1%** package coverage (local output `/tmp/lf171-us1-final-tests.log`). Adapter and index tests passed for harness (**3.395s**, 45.3%), codex (**3.680s**, 28.4%) and git (**4.457s**, 17.8%). Store round-trip tests passed in **10.974s**, 12.3%. These percentages are scoped package coverage, not a full-suite claim. Reviewer independently ran `git diff --check` successfully.

Two test-fixture corrections were reviewed: the no-repeat assertion respects the existing failed-run location lead, and a successful conversation setup makes Codex resume/fallback reachable. Neither required a production change. Final aggregate checks remain the orchestrator's separate gate.

Exact settled turn command:

```sh
go test -race -cover ./internal/turn -run 'TestExecuteDeferredDirtyRetryLifecycle|TestSyncBaseDirtyPolicyPrecedence|TestSyncBaseStatusErrorRetainsCause|TestReporterDisclosesDeferredDirty|TestExecuteStatusInspectionError|TestManagedPublicationRecoversDurableCreateAndAttach|TestSaveOnlyCommitsASucceededRun|TestSaveDoesNotCommitAbandonedIsUnchanged|TestExecuteDoesNotCommitAnAbandonedRun|TestFilterClassify|TestFilterAdmit|TestExecuteRefusesAnAgentSwitch|TestExecuteRefusesASyncOnAConflictingBase|TestSyncBaseBlockedOnConflictLeavesBranchUntouched|TestAutomaticCancellationWinsBeforeAllocation|TestApprovedRevisionSurvivesResumeAndFallback' -count=1 -timeout=5m
```

## Cross-story summary

The deferral contract is ready for US-2 and US-3. Dirty normal recovery does not weaken confirmed-resolution revision checks or low-level dirty guards. Existing transport filtering remains unchanged; collisions and Plan-specific recovery belong to US-3. Preparation keeps caller context and clone gating; no workers, dependencies, SQL outside store, forge access or credential changes were introduced.

## Scoped immutable snapshot files

The following verified paths cover the implementation, validation fixtures and immediate unchanged consumers used in this review. Include this review document itself as well when publishing evidence.

- `internal/codex/deferred_sync_test.go`
- `internal/codex/runner.go`
- `internal/codex/runner_test.go`
- `internal/git/commit.go`
- `internal/git/exec.go`
- `internal/git/merge.go`
- `internal/git/status.go`
- `internal/git/status_preservation_test.go`
- `internal/git/worktree_test.go`
- `internal/gittest/gittest.go`
- `internal/harness/deferred_sync_test.go`
- `internal/harness/prompt.go`
- `internal/harness/prompt_test.go`
- `internal/harness/runner.go`
- `internal/harness/runner_test.go`
- `internal/report/run.go`
- `internal/store/deferred_sync_test.go`
- `internal/store/runs.go`
- `internal/store/store_test.go`
- `internal/turn/approved_attempts_test.go`
- `internal/turn/classify.go`
- `internal/turn/classify_test.go`
- `internal/turn/continuation_admission_test.go`
- `internal/turn/continuation_dispatch.go`
- `internal/turn/deferred_dirty_test.go`
- `internal/turn/errors.go`
- `internal/turn/execute.go`
- `internal/turn/execute_test.go`
- `internal/turn/fixture_test.go`
- `internal/turn/harvest.go`
- `internal/turn/harvest_test.go`
- `internal/turn/managed_scope.go`
- `internal/turn/plan_handoff.go`
- `internal/turn/plan_publication.go`
- `internal/turn/plan_publication_test.go`
- `internal/turn/prepare.go`
- `internal/turn/report.go`
- `internal/turn/report_test.go`
- `internal/turn/story_progress_test.go`
- `internal/turn/sync_test.go`
- `internal/turn/syncrefusal_test.go`

## Attempt 2 — final shared-code re-review

Attempt 1 was invalidated before the shared merge implementation changed to prevent raw-index mutation on rejected reserved-transport merges. This renewed review binds the final implementation and rerun.

The US-3 correction to `internal/git/merge.go` is settled. It inspects staged/tracked reserved transport with non-refreshing status and compares reserved branch/base tree entries before any mutating merge; inspection errors retain their causes. Ordinary untracked transport remains allowed. Dirty normal deferral still returns before this merge guard, and confirmation uses its unchanged guarded apply path. No compatibility findings.

Reviewer ran the combined prior US-1 scoped command extended with `TestDirtyConfirmationRecoveryLifecycle|TestApplyConfirmedResolution`, using `-race -cover -count=1 -timeout=10m`: **PASS, 78.534s, 44.2%** scoped turn coverage. The US-3 specialist also reported `go test ./internal/git -run 'TestReservedTransportSynchronization|TestCommitExcludesFactoryTransport' -race -cover -count=1` passing in **38.964s**, **25.5%** coverage. The settled 16-case reserved-path matrix and existing six save-exclusion cases cover the shared correction. Reviewer inspected the final guard/tests and found no issues.

## Verdict

CLEAN — no findings. US-1 has passing scoped tests and review; the orchestrator may publish its per-story evidence before dependents begin. Final aggregate gates remain required.
