# US-2 — The sub-issue phase skips a non-success run

> As a repository owner, I want the sub-issue filing phase to skip a run that
> did not succeed, so that it does not sweep that run's uncommitted work into
> a commit of its own and does not file issues whose Detail links point at an
> unpushed SHA.

Widens the guard in `internal/turn/subissues.go` so `fileStoryIssues` never
runs its `git add --all` write-back commit for a non-success run.

## Why This Matters

There's a second, easy-to-miss place where litefactory writes a commit: the
step that files sub-issues also stages and commits whatever is sitting in the
worktree at that point. If US-1 stops the main commit step from firing on a
failed run but this step doesn't change too, the failed run's half-finished
work would still end up committed anyway — just under a different message,
with no record connecting it to the run that produced it. This story closes
that second door, so the first story's fix actually holds instead of just
relocating the same problem one step over.

## Architecture Notes

- Widen the `fileStoryIssues` guard in `internal/turn/subissues.go` from
  `runResult == nil || Outcome == OutcomeAbandoned` to
  `runResult == nil || !runResult.Succeeded()`, preserving the existing
  log-and-early-return doctrine: no mutation of `result`, no returned error.
- Update the guard's comment to state the real reason: `git add --all` would
  otherwise sweep the failed run's uncommitted work into a commit no runs row
  names.
- Keep the `runResult == nil` check even though it becomes logically redundant
  under the wider guard — it's clearer to read and deleting it is a needless
  diff.
- Log level: match `save`'s existing abandonment log (Info/Warn), not Error —
  "the run failed, so no sub-issues" is a normal outcome, not something to
  alarm on.
- Must ship in the same unit as US-1, never separately — see plan_brief
  Workflow and Shared Decisions. T-2.4 below is the regression test that
  proves both landed together.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Widen the `fileStoryIssues` guard in `internal/turn/subissues.go` to `runResult == nil \|\| !runResult.Succeeded()`, preserving the log-and-early-return doctrine | logic | — | S |
| T-2.2 | Update the guard's comment to state the real reason: `git add --all` would otherwise sweep the failed run's uncommitted work into an unattributed commit | logic | T-2.1 | XS |
| T-2.3 | Tests: for each non-success outcome the story-index write-back does not run, `result` is returned byte-identical, and no error is returned | test | T-2.1 | M |
| T-2.4 | Regression test for the relocation bug: a failed run with a dirty worktree produces **no** commit from either `save` or `fileStoryIssues` | test | US-1's T-1.1, T-2.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | `fileStoryIssues` returns early for a failed, timed-out, or no-result run, files no sub-issues, and makes no write-back commit. |
| AC-2.2 | A run following a failed run in the same worktree commits both its own changes and the predecessor's leftovers when it succeeds. |
