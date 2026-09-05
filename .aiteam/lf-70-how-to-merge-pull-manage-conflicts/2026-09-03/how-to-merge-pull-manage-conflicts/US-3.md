# US-3 — Disclose every automatic change

> As the owner of a task, I want the run comment to plainly state whether
> litefactory synced my branch, skipped syncing, or found nothing to sync, so
> that I never have to wonder what happened to my code without my say-so.

This story adds the visible record: a sync section in the run comment for
every outcome litefactory's automatic branch-syncing can produce.

## Why This Matters

A machine quietly merging code into a branch on someone's behalf is only
acceptable if the person can see exactly what it did, every single time — not
just when something goes wrong. This story is the trust mechanism for the
whole feature: it is what turns "litefactory changed my branch without
telling me" into "litefactory told me it changed my branch, and why." It
matters just as much when *nothing* happened (the branch was already current)
as when a merge occurred, and it matters when litefactory chose *not* to sync
even though it could have — silence in any of those cases is the failure mode
this story exists to prevent.

## Architecture Notes

- Pattern: additive rendering block, following the existing shape of other
  optional comment sections that render nothing when their pointer is nil.
- Package placement: `internal/report/run.go` plus a testdata golden-file
  fixture per outcome.
- Key interfaces: `report.Run.Sync *report.Sync` (nil renders nothing at
  all — not an empty section); `report.Sync{Outcome, Behind, Ahead, Base,
  CommitSHA, Conflicts []string}`. `Outcome` covers all five values:
  `up_to_date`, `synced`, `blocked`, `skipped`, `failed`.
  Conflict-path rendering itself (truncation, rejection of unsafe paths) is
  US-5's responsibility — this story renders whatever list US-5's sanitizer
  hands it, it does not sanitize.
- Integration point: populated from the `SyncOutcome`/`SyncBehind`/
  `SyncAhead`/`SyncCommitSHA` fields US-9 persists on the run row, after
  `syncBase` (US-2) completes.
- Every outcome gets a rendered sentence, including `skipped` — this was
  called out explicitly by the architect when resolving the
  `auto_sync_max_behind` assumption: a skip must be visible, never silent,
  whether it came from the config cap, `auto_sync_base: false`, or
  `lf:sync=off`.
- NOT in scope: computing the outcome (US-1, US-2), the refusal comment kind
  for `blocked` (US-4 — that path uses `ReplyRefusesSync`, a different
  mechanism from this story's normal-run sync section), sanitizing or
  truncating conflict paths (US-5).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Add `report.Sync` struct and the `Run.Sync` field | Code | US-2 T-2.2 | S |
| T-3.2 | Add renderer covering all five outcomes, including `skipped` | Code | T-3.1 | M |
| T-3.3 | Golden-file tests, one fixture per outcome | Test | T-3.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Given `SyncOutcome=up_to_date`, the comment renders no sync section at all — not an empty one. |
| AC-3.2 | Given any `skipped` outcome — cap exceeded, config disabled, or `lf:sync=off` — the comment discloses the skip and its reason; a skip is never silent. |
