# US-6 — Verify per-issue sync is independent under concurrency

> As the owner of several tasks running at once, I want each task's branch
> sync to happen independently without interfering with the others, so that
> running many issues in parallel is never a reason for one issue's sync to
> corrupt or block another's.

This story proves — it does not build — that litefactory's existing
per-clone locking already gives every task branch an independent, correct
sync under concurrency, with a test standing as the proof.

## Why This Matters

It would be easy to assume that teaching litefactory to merge branches means
teaching it to lock branches too, and that assumption would be wrong here.
The scenario this story addresses — many tasks in flight against the same
repository at once, each syncing its own branch — is already handled by
locking litefactory built for an entirely different reason (protecting one
clone from being torn by concurrent git operations). Building a second,
branch-scoped lock on top of that would not make anything safer; it would
reopen a race window the existing lock exists specifically to close. This
story exists to make that reasoning verifiable rather than merely asserted —
a test that would fail if someone later added the very locking this story
argues against.

## Architecture Notes

- This is a verification story (architect DEV-3), not a construction one.
  Read `plan_brief.md`'s Shared Decisions before starting: no new locking
  primitive, no per-branch keyed registry, no second `CloneGate` acquisition,
  no mutex added to `internal/git`.
- Why the existing model is already correct: measured contention in this
  codebase is per-*clone*, not per-branch — fetch races, worktree-add races,
  and upstream-tracking races are all clone-scoped, and a merge itself moves a
  ref in the clone's shared `refs/` namespace and writes to its shared object
  database, which is clone-scoped contention too. Concurrent issues sync into
  *different* branches; they queue on the one existing `CloneGate` acquisition
  per repo, they do not conflict with each other. The long part of a run — the
  coding agent itself — sits entirely outside the gate, so serializing the
  three sync git commands plus the merge costs almost none of the parallelism
  the gate protects.
- Key test shape: N concurrent turns against one clone, each targeting a
  distinct task branch, each syncing independently, asserted against the
  actual acquisition count — not just "no error occurred," but "exactly one
  `Acquire` per turn, and no cross-branch interference."
- Package placement: test lives alongside `internal/turn/gate_test.go`'s
  existing concurrency assertions, in the same style that already checks
  `prepareClone`'s single-acquisition property.
- Depends on US-2 (there is nothing to verify concurrency for until `syncBase`
  exists).
- NOT in scope: adding any locking, gating, or serialization primitive — if
  this story's test fails, the fix is investigating why the existing model
  doesn't hold, not adding a lock to make it pass.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Concurrency test: N turns, one clone, N branches, independent sync outcomes | Test | US-2 T-2.5 | L |
| T-6.2 | Document why the gate stays keyed on `repo.Path` and not on branch | Doc | T-6.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | Drift measurement and merge occur within a single `CloneGate` acquisition per turn; a test asserts exactly one `Acquire` call per turn, not more. |
| AC-6.2 | N concurrent turns against the same clone, each targeting a different task branch, each complete their own sync independently with no cross-branch interference and without any second gate acquisition appearing anywhere in the call trace. |
