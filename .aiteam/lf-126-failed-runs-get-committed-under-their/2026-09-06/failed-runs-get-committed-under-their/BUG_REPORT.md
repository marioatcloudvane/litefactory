# Bug Report

## Bug 1: Worktree path named twice in a non-success report

### Root cause
`describeRunOutcome` (`internal/report/run.go`) and `metadataList`
(`internal/report/run.go`) each independently append a "the work is still in
the worktree at `<path>`" mention for the same non-success run, so any report
whose `Run.WorktreePath` is set — which is every `DispositionRan` result in
production, per `internal/turn/execute.go:511` — names the path twice.

Both additions landed in the same commit (`15efc68`, "You've hit your session
limit · resets 3:40pm"), which refactored `withFailure`'s pre-existing,
single worktree mention into the shared helpers `worktreeNote`/`inWorktreeAt`
(`internal/report/run.go:411-434`) and then, in the same diff, called those
same helpers from two more places that had never named the worktree before:
`describeRunOutcome`'s `Failed`/`TimedOut`/`NoResult` arms (the callout), and
a brand-new default arm in `metadataList`'s Commit-line switch (T-3.3/AC-3.1).
Neither addition was aware of the other — the callout's mentions and the
Commit line's mention were both written to satisfy "the report names the
worktree path" on their own, so together they say it twice.

None of the pre-existing golden fixtures catch this because every one of
them (`outcome_failed.golden`, `outcome_timed_out.golden`,
`outcome_no_result.golden`, `run_never_started.golden`, `run_failed.golden`,
`outcome_abandoned.golden`) has an empty `Run.WorktreePath`, under which
`inWorktreeAt`/`worktreeNote` both render `""` at both call sites, making the
duplication invisible. `TestNonSuccessReportNamesTheWorktree` is the first
test that sets `WorktreePath` on a `Failed`/`TimedOut`/`NoResult` run and
therefore the first to observe the count going to 2.

The `Run.Failure != nil` path through `withFailure` (used by
`run_push_failed.golden`, the "not completed"/"did not run" callouts) is
*not* implicated by this test — `failureRun()` leaves `Failure` nil, so only
`describeRunOutcome`'s own arms and `metadataList`'s default arm run. (When
`Failure != nil` and `CommitSHA` is also empty, the same duplication pattern
can in principle recur through `withFailure`'s own `worktreeNote` call at
line 407 colliding with `metadataList`'s default arm; that combination has no
covering test today and is not one of this report's three failing subtests,
so it is noted here as a related risk rather than folded into this bug.)

### Evidence
Call sites that emit a worktree-path mention today, in the order they appear
in the rendered comment body (callout renders before the metadata list, per
`fixedSection`, `internal/report/run.go:512-526`):

1. `describeRunOutcome`, `internal/report/run.go`:
   - `OutcomeFailed` arm, line 468: `lead += worktreeNote(run.WorktreePath)`
     → appends `"\n\nWhatever the run produced is still in the worktree at `<path>`."`
   - `OutcomeTimedOut` arm, line 476: `lead += ". The work it had done up to that point is still in the worktree" + inWorktreeAt(run.WorktreePath) + ". Raise the timeout or narrow the task, then comment again."`
   - `OutcomeNoResult` arm, line 490: `lead += worktreeNote(run.WorktreePath)`
     → same text as the `Failed` arm.

2. `metadataList`, `internal/report/run.go:576-582` — the Commit-line
   `default` arm (reached whenever `run.CommitSHA == ""`, `run.Branch != ""`,
   and `!view.succeed`, i.e. every non-success outcome with nothing
   committed):
   ```go
   add("Commit", "none — this run did not succeed; if it produced anything, it is still in the worktree"+
       inWorktreeAt(run.WorktreePath))
   ```

Rendered example (`Failed`, `WorktreePath` set — matches
`TestNonSuccessReportNamesTheWorktree/failed`):
```
> [!CAUTION]
> Run 3 failed: `claude` exited with status 1.
>
> Whatever the run produced is still in the worktree at `/Users/mario/dev/.lf-worktrees/acme-api/task-123`.

- **Branch:** `lf/123-add-dry-run-to-task-clean`
- **Commit:** none — this run did not succeed; if it produced anything, it is still in the worktree at `/Users/mario/dev/.lf-worktrees/acme-api/task-123`
```
The path appears once in the callout (via `worktreeNote`, line 468) and once
more in the Commit line (via `inWorktreeAt`, line 582) — `strings.Count(body,
testWorktreePath)` is 2, and the test at `report_test.go:1273` wants exactly
1. The same shape reproduces for `TimedOut` (line 476) and `NoResult` (line
490).

### Recommended fix
Keep **`metadataList`'s Commit-line default arm** (`internal/report/run.go:576-582`)
as the single canonical site that names the worktree path for a non-success
run with nothing committed, and stop `describeRunOutcome`'s `Failed`,
`TimedOut`, and `NoResult` arms from naming it a second time in the callout.

Why this site and not the callout:
- It is exactly what AC-3.1 is about: the same line both states the path
  *and* uses different wording from the success case's "this run committed
  nothing" — one line does both jobs the acceptance criterion asks for, so
  keeping it is sufficient on its own without help from the callout.
- It already fires uniformly for every non-success outcome through the
  shared `!view.succeed` branch (`Failed`, `TimedOut`, `NoResult`,
  `Abandoned`, `OutcomeUnknown`/nil-result, "outcome unrecognised") — one
  code path, one wording, no per-arm duplication logic to keep in sync.
- It is the site every existing golden fixture already anchors on
  (`outcome_failed.golden:9`, `outcome_timed_out.golden:9`,
  `outcome_no_result.golden:9`, `outcome_abandoned.golden:9`,
  `run_never_started.golden:9`, `run_failed.golden:9` all assert the exact
  string `"none — this run did not succeed; if it produced anything, it is
  still in the worktree"`). Removing the callout's mentions leaves every one
  of those goldens byte-for-byte unchanged (they all have `WorktreePath ==
  ""`, so the callout's `worktreeNote`/`inWorktreeAt` calls were already
  rendering `""` there); keeping the Commit-line arm and only deleting the
  callout's three `+= worktreeNote(...)`/`+ inWorktreeAt(...)` lines is the
  smallest diff that fixes the duplication.

Concretely:
- `describeRunOutcome`, `OutcomeFailed` arm (line 468): delete
  `lead += worktreeNote(run.WorktreePath)`. The callout goes back to just
  `"Run %d failed: `%s` exited with status %d."` (or the signal-exit
  variant) — no worktree clause at all; the Commit line below already says
  where the work is.
- `describeRunOutcome`, `OutcomeTimedOut` arm (line 476): drop
  `inWorktreeAt(run.WorktreePath)` from the sentence, reverting it to the
  pre-`15efc68` wording: `". The work it had done up to that point is still
  in the worktree. Raise the timeout or narrow the task, then comment
  again."` — this still says "no commit happened, the work is still around"
  without repeating the path, which is what the Commit line now owns.
- `describeRunOutcome`, `OutcomeNoResult` arm (line 490): delete
  `lead += worktreeNote(run.WorktreePath)`, same as `Failed`.
- Leave `worktreeNote`, `inWorktreeAt`, and `withFailure`'s own call to
  `worktreeNote` (line 407) untouched — `withFailure` is the pre-existing,
  legitimate use (it is the *only* mention in the `run.Failure != nil` +
  `CommitSHA != ""` case, e.g. `run_push_failed.golden`, where
  `metadataList` takes the `CommitSHA != ""` branch instead of the default
  arm and never mentions the path itself). Only the two new call sites added
  in `15efc68` — the three `describeRunOutcome` arms — are redundant with
  `metadataList`'s default arm and should be reverted.
- `worktreeNote`'s doc comment (`internal/report/run.go:416-418`, "…
  `describeRunOutcome`'s own non-success arms say the same thing, in the
  same words, about a run that failed on its own") describes the
  duplication as intentional; it should be updated once the arms stop
  calling it, since after the fix `worktreeNote` is only called from
  `withFailure`.

### Affected tests / goldens
- `TestNonSuccessReportNamesTheWorktree` (`internal/report/report_test.go:1255`),
  subtests `failed`, `timed_out`, `no_result` (lines 1259-1283) — these are
  the tests this bug makes fail; the fix above makes `strings.Count(body,
  testWorktreePath)` equal 1 for all three.
- The same test's "succeeded report unaffected" (lines 1285-1292) and
  "no-change report keeps its own wording" (lines 1294-1306) assertions
  already pass and are untouched by this fix — neither exercises
  `describeRunOutcome`'s `Failed`/`TimedOut`/`NoResult` arms.
- Golden fixtures `outcome_failed.golden`, `outcome_timed_out.golden`,
  `outcome_no_result.golden`, `outcome_abandoned.golden`,
  `run_never_started.golden`, `run_failed.golden` — all have
  `WorktreePath == ""` today, so reverting the three callout arms produces
  no diff against any of them (their callout text for `Failed`/`NoResult`
  already omits a worktree clause, and `TimedOut`'s golden text at line 6
  already reads "...still in the worktree. Raise the timeout..." with no
  path, matching the reverted wording exactly).
- `run_push_failed.golden` — unaffected; it exercises `withFailure` with
  `CommitSHA != ""`, which this fix does not touch.
- `TestWorktreePathIsRenderedThroughTheSanitiserSpan`
  (`internal/report/report_test.go:1314`) — uses `failureRun()` (an
  `OutcomeFailed` run) with a hostile `WorktreePath` and asserts the path is
  quoted through `codeSpan`/flattened. After the fix, that assertion is
  satisfied by the Commit line's `inWorktreeAt` call alone (the callout no
  longer renders the path), so this test keeps passing; worth a quick manual
  check that it does not also assert a specific count of the sanitised
  path's presence in the callout specifically.

## Bug 2: Worktree path named twice when a failure produced no commit at all

### Root cause
`outcomeView.withFailure` (`internal/report/run.go:386-409`) calls
`v.lead += worktreeNote(run.WorktreePath)` on line 407 unconditionally, after
all three of its switch branches (`run.Result == nil`, `case v.succeed`, and
`default`), regardless of `run.CommitSHA`. Bug 1's fix removed the redundant
mentions from `describeRunOutcome`'s own arms but left this call alone on the
stated grounds that it is "the legitimate single-mention case" — true only
when `run.CommitSHA != ""` (`run_push_failed.golden`), because in that case
`metadataList` takes one of its `case run.CommitSHA != ""` branches (line
567/569) and never itself mentions the worktree.

But when `run.Failure != nil` and `run.CommitSHA == ""`, `metadataList`'s
Commit-line `default` arm (line 575-581, reached whenever `run.Branch != ""`
and `!view.succeed`) *also* fires and appends
`inWorktreeAt(run.WorktreePath)` — and it always can, because every one of
`withFailure`'s three branches leaves `view.succeed == false`:
- `case run.Result == nil` (line 390-395): builds a brand-new `outcomeView`
  literal with `succeed` left at its zero value, `false`.
- `case v.succeed` (line 396-402): also builds a brand-new literal with
  `succeed` left unset, `false` — this is the branch that turns a run that
  *did* succeed (`describeRunOutcome` returned `succeed: true`) into "not
  completed", specifically so a report with a failure wrapped around a good
  run never claims success.
- `default` (line 403-404): only appends to `v.lead`, but `v.succeed` was
  already `false` coming in — every `describeRunOutcome` outcome other than
  `OutcomeSucceeded` returns `succeed` at its zero value, and this branch is
  only reached when `v.succeed` was already `false` (the `case v.succeed`
  branch above catches the `true` case first).

So `view.succeed` is `false` after `withFailure` runs, in all three branches,
every time. Combined with `run.CommitSHA == ""`, that is exactly
`metadataList`'s trigger condition (`run.Branch != "" && !view.succeed`,
line 566-582) for its own worktree mention. The two call sites — line 407 in
`withFailure` and line 580-581 in `metadataList` — both fire whenever
`run.Failure != nil && run.CommitSHA == "" && run.Branch != "" &&
run.WorktreePath != ""`, and the path is named twice. This is the same
duplication shape as Bug 1, on the `Run.Failure != nil` path instead of the
`Run.Failure == nil` path, exactly as flagged as an unfixed related risk in
Bug 1's write-up.

Traced against the three new subtests, confirming the test engineer's
finding:
- `turn_never_reached_the_agent` — `run.Result == nil`, `run.Failure != nil`,
  `Branch` set, `CommitSHA` unset. `describeRunOutcome` returns `{word:
  "outcome unknown", succeed: false}` (its own `res == nil` early return,
  line 449-455). `withFailure` takes the `run.Result == nil` branch,
  producing a fresh `succeed: false` view, then unconditionally appends
  `worktreeNote(run.WorktreePath)`. `metadataList` separately hits its
  `default` arm (`CommitSHA == ""`, `Branch != ""`, `!succeed`) and appends
  `inWorktreeAt(run.WorktreePath)` too. Two mentions.
- `run_succeeded_but_the_commit_itself_failed,_not_merely_the_push` —
  `successRun()` with `CommitSHA`/`SHAAfter` cleared and `Pushed = false`,
  `Failure` set. `describeRunOutcome` returns `{word: "succeeded", succeed:
  true}` for `OutcomeSucceeded` (line 461-462). `withFailure` takes `case
  v.succeed`, replacing the view with a fresh `succeed: false` one ("not
  completed"), then unconditionally appends `worktreeNote`. `metadataList`'s
  `default` arm fires on the same `CommitSHA == ""`/`Branch != ""`/`!succeed`
  combination. Two mentions.
- `run_failed_and_cleanup_afterwards_also_failed` — `failureRun()`
  (`OutcomeFailed`, `CommitSHA` never set) with `Failure` added.
  `describeRunOutcome` returns `{word: "failed", succeed: false}` (line
  464-469). `withFailure` falls to `default` (`v.succeed` was already
  `false`), appends its own "...could not complete the turn either: ..."
  text, then unconditionally appends `worktreeNote`. `metadataList`'s
  `default` arm fires again on the same combination. Two mentions.

### Evidence
`internal/report/run.go:407`:
```go
	v.lead += worktreeNote(run.WorktreePath)
	return v
}
```
fires on every `withFailure` branch, with no condition on `run.CommitSHA`,
directly alongside `internal/report/run.go:575-581`:
```go
	default:
		// A non-success run that committed nothing is not the same fact as a
		// successful run that had nothing to change (AC-3.1): the words must
		// differ, and this is where the reader is told where the work — if
		// there is any — still is.
		add("Commit", "none — this run did not succeed; if it produced anything, it is still in the worktree"+
			inWorktreeAt(run.WorktreePath))
```
which fires whenever `run.CommitSHA == "" && run.Branch != "" &&
!view.succeed` — a condition `withFailure` always satisfies on the
`view.succeed` side and, in all three new subtests, also on the `CommitSHA`
side.

By contrast, `run_push_failed.golden`'s fixture (`successRun()` with only
`Pushed` flipped to `false` and `Failure` set — `CommitSHA` is left at
`successRun()`'s `"9c1e0a4b7d2f8e6a5c3b1d09f8e7a6b5c4d3e2f1"`,
`report_test.go:106`) takes `metadataList`'s `case run.CommitSHA != ""`
branch (line 569-570, "in the local worktree only — not pushed") instead of
the `default` arm, so `metadataList` never mentions the path itself there —
`withFailure`'s line 407 is the *only* site that names it, which is exactly
the single legitimate case Bug 1 carved out.

### Recommended fix
Guard `withFailure`'s call on the same condition that keeps `metadataList`'s
`default` arm from firing: `run.CommitSHA != ""`. Concretely, at
`internal/report/run.go:407`, change

```go
	v.lead += worktreeNote(run.WorktreePath)
```
to
```go
	if run.CommitSHA != "" {
		v.lead += worktreeNote(run.WorktreePath)
	}
```

Reasoning, worked through explicitly rather than assumed: the two sites are
mutually exclusive by construction once this guard is in place, because
`metadataList`'s switch on `run.CommitSHA` (line 566-582) is itself
mutually exclusive —
- `run.CommitSHA != ""` → `metadataList` takes one of the two
  `CommitSHA != ""` branches (line 567-570) and never calls
  `inWorktreeAt`. Under the fix, this is exactly the condition under which
  `withFailure` *does* still call `worktreeNote` — so the path is named
  once, by `withFailure`, precisely reproducing today's `run_push_failed`
  behaviour.
- `run.CommitSHA == ""` → `metadataList` takes either the `run.Branch == ""`
  no-op arm (no mention, matching the pre-existing "no branch, no worktree
  to name" design already relied on by Bug 1's fix) or, when `Branch != ""`
  and (as established above) `!view.succeed` always holds after
  `withFailure`, the `default` arm — which now becomes the *sole* mention.
  Under the fix, `withFailure` does not call `worktreeNote` in this case, so
  there is no second mention.

No third combination is reachable: `metadataList`'s switch has no branch
where `CommitSHA == ""` produces a mention other than the `default` arm, and
the guard's condition (`CommitSHA != ""`) is the exact complement of that
arm's own guard (`CommitSHA == ""`), so the two call sites can never both
fire, and — for any run with a `Branch` — at least one of them always does
(no case is silently left unmentioned other than the pre-existing
`Branch == ""` no-op, which is unaffected by this change).

`worktreeNote`'s doc comment (`internal/report/run.go:416-419`) already
describes this call as "the *only* mention in the `run.Failure != nil` +
`CommitSHA != ""` case" — the doc comment already states the invariant this
fix enforces in code; only the `if` was missing.

### Affected tests / goldens
- `TestFailureAroundTheRunWithNoCommitNamesTheWorktreeOnce`
  (`internal/report/report_test.go:1365`), all three subtests
  (`turn_never_reached_the_agent`,
  `run_succeeded_but_the_commit_itself_failed,_not_merely_the_push`,
  `run_failed_and_cleanup_afterwards_also_failed`) — these are the tests this
  bug makes fail; the fix above makes `strings.Count(body, testWorktreePath)`
  equal 1 for all three, since in every one `run.CommitSHA == ""`, so
  `withFailure` stops mentioning the path and `metadataList`'s `default` arm
  becomes the sole mention.
- `run_push_failed.golden` (`internal/report/report_test.go:531-553`) — not
  affected. Its fixture has `CommitSHA != ""`, so the new guard still lets
  `withFailure` append `worktreeNote`, and `metadataList` still takes its
  `CommitSHA != ""`/"not pushed" branch rather than the `default` arm,
  exactly as today. No byte of that golden changes.
- All six goldens listed under Bug 1's "Affected tests / goldens"
  (`outcome_failed.golden`, `outcome_timed_out.golden`,
  `outcome_no_result.golden`, `outcome_abandoned.golden`,
  `run_never_started.golden`, `run_failed.golden`) — none of their fixtures
  set `Run.Failure`, so `withFailure` is never called for them and this fix
  does not touch their output.
- `TestWorktreePathIsRenderedThroughTheSanitiserSpan`
  (`internal/report/report_test.go:1314`) — uses `failureRun()` (`CommitSHA`
  unset) with `Failure` unset (plain `OutcomeFailed`, no wrapped failure), so
  `withFailure` is never invoked for this fixture either; this test is
  unaffected by the guard.
