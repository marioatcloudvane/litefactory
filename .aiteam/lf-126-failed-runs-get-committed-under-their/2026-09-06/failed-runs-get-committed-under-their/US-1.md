# US-1 — A non-success run commits nothing

> As a repository owner, I want litefactory to make no commit for a run that
> did not succeed, so that a branch never carries a commit whose message is a
> tool's error text and whose content nobody reviewed as finished work.

Widens the commit gate in `internal/turn/harvest.go` so only a run that
actually succeeded ever reaches the commit-and-push step.

## Why This Matters

Today, when a coding run fails partway through, litefactory still writes a
commit to the branch — using the run's own raw error text as the commit
message, as if a crash were finished, reviewed work. Anyone reading the
branch's history sees commits that look like completed features but are
actually failure reports. This story stops the commit from happening at all
when a run doesn't succeed, so branch history only ever reflects real
successes. It's deliberately conservative: the failed run's actual work isn't
thrown away — it stays on disk and the next attempt on the same task picks it
up cleanly — so nothing here changes what a human sees for a run that
succeeds.

## Architecture Notes

- Widen `save`'s gate in `internal/turn/harvest.go` from
  `Outcome == OutcomeAbandoned` to `!runResult.Succeeded()`, covering Failed,
  TimedOut, Abandoned, and NoResult in one gate. Uses the already-existing,
  nil-safe `agent.RunResult.Succeeded()` — no new predicate is needed for this
  story (see plan_brief Shared Decisions on the two `Succeeded()` predicates).
- `commitAndPush` itself is unchanged and remains the only site that creates a
  commit or a push; it keeps passing `runResult.ResultText` as the commit
  summary, now reachable only on success.
- This gate sits inside the `context.WithoutCancel` bookkeeping region that
  must survive cancellation. The new early-return arm is the same shape as the
  existing abandonment arm and must not skip `finish()` or the deferred meta
  write.
- `turn.Result.Succeeded()` (`internal/turn/result.go:236`) is unrelated and
  must not be touched — see plan_brief Ring-fenced.
- Rewrite `save`'s doc comment and `internal/turn/doc.go` where either
  currently argues the opposite of the new behaviour.
- Platform: must ship together with US-2, never alone — see plan_brief
  Workflow. Landing this story by itself relocates the bug into
  `fileStoryIssues`'s `git add --all` write-back rather than fixing it.
- Risk to flag, not fix here: a `TimedOut` run that produced substantial work
  previously got partial credit via a commit; after this story it does not.
  That's the intended behaviour (plan_brief Shared Decisions), but it makes
  US-3's report wording load-bearing for exactly this case — a human most
  needs to be told where the work is when a long-running attempt times out.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Widen `save`'s gate in `internal/turn/harvest.go` to skip `commitAndPush` for any non-success outcome | logic | — | S |
| T-1.2 | Rewrite `save`'s doc comment and `internal/turn/doc.go` where they describe commit-on-failure | logic | T-1.1 | XS |
| T-1.3 | Table-driven unit tests over all five outcomes: only `Succeeded` reaches `commitAndPush`; the other four leave the worktree uncommitted and log the worktree path | test | T-1.1 | M |
| T-1.4 | Non-regression pin: a failed run leaves `Result.CommitSHA` empty, `Changed`/`Committed`/`Pushed` false, `Result.Err` nil, and `Result.Succeeded()` false | test | T-1.1 | S |
| T-1.5 | Test that `finish` writes no `commit_sha` and `store.FinishRun` leaves the column untouched for a failed run | test | T-1.1 | S |
| T-1.6 | Run `turn/gate_test.go` unmodified and confirm the three `CloneGate.Acquire` call sites are unchanged | test | T-1.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | A turn whose run ends `OutcomeFailed` makes no commit: the branch SHA is unchanged, `git log` shows no new commit, and no commit message anywhere contains the run's result text. |
| AC-1.2 | The same holds for `OutcomeTimedOut` and `OutcomeNoResult`; the existing abandoned-run behaviour is unchanged. |
| AC-1.3 | A turn whose run ends `OutcomeSucceeded` still commits and pushes exactly as before, with `runResult.ResultText` as the summary. |
| AC-1.4 | After a failed run, the worktree still contains the run's changes (a file the run wrote is present and shows as modified/untracked in `git status`). |
| AC-1.5 | After a failed run, the run's row has an empty `commit_sha`, and `SetRunCommit` was not called. |
| AC-1.6 | After a failed run, `Result.Changed`, `Committed`, `Pushed`, `CommitSHA`, `SHAAfter`, and `Changes` are all at their zero values, and `Result.Err` is nil (the run failed; the turn did not). |
