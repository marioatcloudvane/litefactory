# US-3 — The report says plainly nothing was committed, and where the work is

> As a repository owner, I want the report of a failed run to say plainly
> that nothing was committed and where the work is, so that I can tell a
> failed run from an empty one without opening the log.

Changes the report renderer in `internal/report/run.go` so a non-success run's
report names the worktree holding its uncommitted work, instead of reading
like a successful run that simply had nothing to change.

## Why This Matters

Once litefactory stops committing failed work (US-1, US-2), the report needs
to say so clearly — otherwise "no commit" looks identical whether a run failed
loudly or succeeded and genuinely had nothing to change, and telling the two
apart means digging through logs. This story makes the report itself carry
that distinction: it states plainly that the run did not succeed and exactly
where its unfinished work still lives, so it can be picked up or inspected
without guesswork.

## Architecture Notes

- Ships after US-1 and US-2 (plan_brief Workflow) — this story describes a
  state that only exists once the commit gate is actually widened; shipping it
  first would make the report claim "nothing was committed" while a commit
  still exists.
- Change the "none — this run committed nothing" arm in
  `internal/report/run.go` to name the worktree path for a non-success run.
- Extend `describeRunOutcome`'s Failed / NoResult arms (and confirm the
  TimedOut arm) so each states the work is still in the worktree, following
  the existing `withFailure` wording. The TimedOut arm already partially says
  this and needs the path added.
- Render the worktree path through the package's existing sanitiser/span
  helpers so it cannot break the comment's markdown.
- Read-only change against `turn.Result` — no change is needed to what `turn`
  hands over to `report`; the field this needs already exists on the result
  type.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Change the "none — this run committed nothing" arm in `internal/report/run.go` to name the worktree path for a non-success run | logic | — | S |
| T-3.2 | Extend `describeRunOutcome`'s Failed / NoResult arms (and confirm TimedOut) so each says the work is still in the worktree, following the existing wording pattern | logic | T-3.1 | M |
| T-3.3 | Rendering tests per outcome: the failed report names the worktree exactly once, does not claim a commit, and the succeeded report is unchanged | test | T-3.1, T-3.2 | M |
| T-3.4 | Test that the worktree path is rendered through the package's existing sanitiser/span helpers and cannot break the comment's markdown | test | T-3.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | The report for a run that did not succeed names the worktree path holding the uncommitted work, and does not describe the run as having committed nothing in the same words used for a successful run that changed nothing. |
