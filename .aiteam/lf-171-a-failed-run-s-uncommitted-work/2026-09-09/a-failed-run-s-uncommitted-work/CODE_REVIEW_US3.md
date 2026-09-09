# Code Review — US-3

Feature: a-failed-run-s-uncommitted-work
Reviewed by: go-code-reviewer
Date: 2026-09-09
Status: CLEAN

Selected scope: US-3, issue #182, issue ID 5397107715, revision `c1117d45419f43caed4ce8c6f05839e1fff8e102047213be09b9173c4d7f9c9f`; dependency: US-1. Review used the factory-supplied complete body and shared plan brief.

## Blocking findings

None.

## Advisory findings

None.

## Acceptance review

- **AC-171-6:** The real Execute fixture distinguishes failed planning from successful agent execution followed by an injected forge publication outage. Both preserve ordinary plan content, proposal bytes and task HEAD until the next eligible recovery runner enters; that successful recovery saves ordinary work without transport. A proposal-only fixture proves normal synchronization rather than dirty deferral and preserves its bytes. Existing commit-exclusion cases exercise staged, untracked, ignored and transport-only situations. Success-only harvest, rooted proposal access and factory-owned publication remain unchanged.
- **AC-171-7:** The recorded Git 2.50.1 probe explains the actual rejected-merge raw-index mutation counterexample, including why merely disabling optional merge locks was insufficient. The correction stays inside `MergeBase`: a read-only reserved-path status check refuses staged/tracked modifications, and a tree-to-tree reserved-path diff refuses branch/base collisions before invoking mutating merge. Both checks wrap inspection errors. Usual untracked proposal/event files remain allowed, and identical legacy tree entries are not migrated. The 16-case real-git matrix covers both reserved paths across staged, tracked and incoming collisions, checks retained bytes plus raw index and HEAD on refusal, and verifies transport exclusion during subsequent successful saving. No case accepts an incoming collision as successful synchronization.

## Validation

Settled specialist results:

- `go test ./internal/git -run 'TestReservedTransportSynchronization|TestCommitExcludesFactoryTransport' -race -cover -count=1`: PASS, **38.964s**, **25.5%** scoped package coverage; 16 collision/preservation cases plus six existing save-exclusion cases.
- `go test ./internal/turn -run 'TestPlanFailureRetainsTransportForRetry|TestProposalOnlyStateSynchronizesNormally' -race -cover -count=1`: PASS, **14.136s**, **24.1%** scoped package coverage.

Reviewer inspected the settled production change, new tests, reused fixtures and probe, and ran `git diff --check` successfully. The combined US-1/US-2 race/coverage regression command also passed against this guard (**78.534s**, **44.2%**), supporting compatibility. No production or test edits were made by the reviewer.

## Cross-story summary

US-1 dirty deferral still precedes preview/merge, and US-2 confirmed application retains its own revision/dirty guards. This narrow reserved-path safety guard creates explicit failures for unsafe transport states rather than silently overwriting or claiming failed work. No transport deletion, broad ignore, migration, extra retry mechanism, live daemon changes or real forge access was introduced.

## Scoped immutable snapshot files

- `.aiteam/lf-171-a-failed-run-s-uncommitted-work/2026-09-09/a-failed-run-s-uncommitted-work/GIT_TRANSPORT_PROBE.md`
- `internal/git/commit.go`
- `internal/git/exec.go`
- `internal/git/merge.go`
- `internal/git/merge_test.go`
- `internal/git/reserved_sync_test.go`
- `internal/git/status.go`
- `internal/git/worktree_test.go`
- `internal/gittest/gittest.go`
- `internal/turn/errors.go`
- `internal/turn/execute.go`
- `internal/turn/fixture_test.go`
- `internal/turn/harvest.go`
- `internal/turn/managed_lifecycle_test.go`
- `internal/turn/plan_handoff.go`
- `internal/turn/plan_publication.go`
- `internal/turn/plan_retry_transport_test.go`
- `internal/turn/prepare.go`
- `internal/turn/story_progress_test.go`
- `internal/turn/sync_test.go`

Include this review document itself in the snapshot.

## Verdict

CLEAN — all scoped acceptance, review and test obligations pass. Publish after valid US-1 dependency evidence; final aggregate gates remain separate.
