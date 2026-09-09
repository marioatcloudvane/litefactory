# Code Review — US-2

Feature: a-failed-run-s-uncommitted-work
Reviewed by: go-code-reviewer
Date: 2026-09-09
Status: CLEAN

Selected scope: US-2, issue #181, issue ID 5397107283, revision `d7ac5422628540c42fb4aa41e4dffb8ba5342da0945c0f1229ddfd5001138643`; dependency: US-1. The supplied immutable body and shared plan brief governed review.

## Blocking findings

None in the settled US-2 change.

## Advisory findings

None.

## AC-171-5 review

`applyFailed` recognizes the existing uncommitted-work sentinel with `errors.Is`, including its wrapped path through `failure`. It returns the same failed outcome and original pending proposal, without introducing mutations or starting an agent. The fixed factory wording explains ordinary owner-comment recovery, clean-branch synchronization and fresh confirmation after changed revisions. It makes no claim that the confirmation applied.

The existing confirmed path first validates recorded branch/base revisions inside the preparation gate, then calls guarded `WriteResolvedFiles`. Its dirty check still precedes writes and uses non-refreshing status. Stale confirmation still supersedes; clean confirmed apply and moved-base protections remain unchanged. The daemon forwards the reason into the existing sanitized confirmation-failure renderer, so the guidance reaches the owner without new report plumbing.

`TestDirtyConfirmationRecoveryLifecycle` covers matching revisions, staged and unstaged working edits, untracked retained work, HEAD/raw-index/file preservation, pending proposal identity, zero runner/advisor calls on refusal, ordinary recovery despite committed conflicts, successful saving to new HEAD, stale supersession, refusal to reconfirm the old proposal, and a new pending proposal from a clean preview. Existing `TestApplyConfirmedResolution` cases cover clean apply, moved-base protection, owner/session/decision gates and mutator rollback.

## Validation

Supplied settled result: `go test -race -cover ./internal/turn -run 'TestDirtyConfirmationRecoveryLifecycle|TestApplyConfirmedResolution' -count=1` passed in **24.793s**, with **22.6%** scoped package coverage. Reviewer independently ran `git diff --check` successfully. No tests or production files were modified during review.

## Cross-story and evidence coordination

US-2 consumes US-1 deferral and non-refreshing status; it leaves confirmed application safeguards intact. US-1 evidence was invalidated before the separate US-3 rejected-merge correction. The final shared-code re-review below restores the scoped technical basis for US-1 attempt 2; publish its evidence before US-2 verification. This review covers the settled US-2 code; changes to shared preparation/status/apply behavior after this review require reassessing affected validation. Final aggregate gates remain separate.

## Scoped immutable snapshot files

- `internal/daemon/confirm.go`
- `internal/git/exec.go`
- `internal/git/status.go`
- `internal/git/worktree_apply.go`
- `internal/gittest/gittest.go`
- `internal/report/reply.go`
- `internal/store/conflict_proposals.go`
- `internal/turn/apply.go`
- `internal/turn/apply_test.go`
- `internal/turn/classify_test.go`
- `internal/turn/conflict_e2e_test.go`
- `internal/turn/deferred_dirty_test.go`
- `internal/turn/dirty_confirmation_recovery_test.go`
- `internal/turn/errors.go`
- `internal/turn/fixture_test.go`
- `internal/turn/harvest.go`
- `internal/turn/prepare.go`
- `internal/turn/sync_test.go`
- `internal/turn/syncrefusal_test.go`

Include this review document itself in the snapshot.

## Final shared-code re-review

The US-3 correction to `internal/git/merge.go` is settled. It inspects staged/tracked reserved transport with non-refreshing status and compares reserved branch/base tree entries before any mutating merge; inspection errors retain their causes. Ordinary untracked transport remains allowed. Dirty normal deferral still returns before this merge guard, and confirmation uses its unchanged guarded apply path. No compatibility findings.

Reviewer ran the combined prior US-1 scoped command extended with `TestDirtyConfirmationRecoveryLifecycle|TestApplyConfirmedResolution`, using `-race -cover -count=1 -timeout=10m`: **PASS, 78.534s, 44.2%** scoped turn coverage. The US-3 specialist also reported `go test ./internal/git -run 'TestReservedTransportSynchronization|TestCommitExcludesFactoryTransport' -race -cover -count=1` passing in **38.964s**, **25.5%** coverage. The settled 16-case reserved-path matrix and existing six save-exclusion cases cover the shared correction. Reviewer inspected the final guard/tests and found no issues.

## Verdict

CLEAN — no scoped findings. US-2 tests pass; publish completion only with valid US-1 dependency evidence and a stable scoped snapshot.
