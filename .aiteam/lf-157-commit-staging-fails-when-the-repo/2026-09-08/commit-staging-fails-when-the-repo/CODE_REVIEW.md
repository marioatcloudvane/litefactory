# Code Review
# Feature: commit-staging-fails-when-the-repo
# Reviewed by: go-code-reviewer
# Date: 2026-09-08
# Status: CLEAN

## Scope

US-1, issue #158, issue ID 5390210480, frozen revision `7cc69b218735eac9a403581328344501089d914415b22bb10baf96bbdd552e62`, dependencies: none. Reviewed the selected inline story against `internal/git/commit.go` and, as explicitly assigned for acceptance coverage, `internal/git/worktree_test.go`. This is static review; no tests were run by the reviewer.

## BLOCKING findings

None outstanding.

### CR-B-001: Required transport entry-state coverage — RESOLVED

**File:** `internal/git/worktree_test.go:406`
**Story:** US-1 / T-157.2 / AC-157-5
**Category:** Acceptance coverage
**Original finding:** The public operation lacked already-staged transport and transport-only no-op fixtures.
**Resolution:** Re-reviewed the stable `TestCommitExcludesFactoryTransport` changes. The new already-staged case stages ordinary and transport files and invokes `Worktree.Commit` without an intervening fixture reset. Both staged and unstaged transport-only cases require successful no-op result fields and unchanged actual HEAD. All cases inspect actual tree/index exclusion and preserve transport bytes. The isolated reset now also checks bytes immediately afterward. No new findings.

## ADVISORY findings

None.

## Acceptance assessment

- AC-157-1: Production diff is exactly the removal of the two staging exclusions. Both resets, ordering, contexts/timeouts, error propagation and public contracts remain unchanged.
- AC-157-2: Existing ignored nonempty fixture verifies a new ordinary commit, actual tree/index exclusion and byte preservation.
- AC-157-3: Existing ignored empty fixture verifies a new ordinary commit, actual tree/index exclusion and retained empty directory.
- AC-157-4: Unignored fixture explicitly proves both files stage, reset leaves only the ordinary file staged and immediately preserves bytes, and the public commit preserves bytes while excluding transport from the tree/index.
- AC-157-5: Ordinary additions/modifications/deletions and provenance retain existing coverage. Direct already-staged transport and staged/unstaged transport-only no-op coverage now resolves CR-B-001. The orchestrator reports full focused `internal/git` race/coverage tests passed with 83.9% statement coverage. Aggregate checks and fresh negative reproduction are still running at review time and are not claimed passed here; exact binary version and scratch commands belong to validation evidence.

## Cross-story summary

One atomic story with no sibling boundaries. No production issues found in idiom, error handling, concurrency, resource management, security, performance or package placement. The existing post-stage reset remains the exclusion mechanism; the change does not suppress failures or alter ignore rules.

## Verdict

CLEAN — CR-B-001 resolved, no outstanding findings. Production review remains clean. Complete pending validation gates before story verification.
