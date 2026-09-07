# Drift Report — Configurable Agent Teams (US-11: `team update`)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-07

## Delivered as planned

- T-30: `team update` command implemented, classifying every tracked lockfile
  record via US-10's `Classify`, auto-updating `In sync`/`Upstream updated`/
  `Missing` records and refusing `Locally modified`/`Conflict` records by
  default, reporting each by name and classification (AC-1).
- T-31: `--force` flag implemented, overwriting `Locally modified`/`Conflict`
  records only when explicitly passed, naming exactly which files it
  overwrote and what was discarded (AC-2).
- T-32: tests covering mixed-state repos (safe subset updated, unsafe subset
  left untouched, both asserted in the same run), `--force` precision
  (overwritten vs. plain-updated distinguished, and a no-op-force case with
  nothing eligible), and single-worktree scope via a real sibling
  `git worktree` proven byte-for-byte untouched by full-tree snapshot diff
  (AC-3).
- Scope held to single-worktree exactly as specified: no discovery of, or
  propagation to, sibling worktrees. `--all-worktrees` (US-15's future
  extension, named descriptively in `plan_brief.md`'s Shared Decision 2/4)
  was correctly not implemented, not stubbed, and not partially attempted —
  matching US-11's own explicit statement that it "has no partial or
  best-effort behavior toward it."
- `--migrate-config` dropped entirely per the story's explicit resolution —
  not implemented, not deferred, no stub flag left behind.
- Reused US-9's atomic-write engine and lockfile update (`team.Add`) rather
  than building a second write primitive; a successful update rewrites the
  affected records' `sourceHash`/`renderedHash` to match what was written.
- Exit-code contract preserved: refusing an update without `--force` remains
  a plain error (exit 1), never `PreconditionError` (exit 3), matching
  Shared Decision 2's table.
- `errors.Join`-based aggregation applied correctly in `runTeamUpdate`'s
  per-record loop from the start — this is the first pass on this issue's
  `team` subcommands where this recurring mistake (BLOCKING in runs 20 and
  25) did not need a fix cycle.
- Package boundary respected: rendering (`teamsource.RenderCurrent`) stayed
  in `internal/teamsource`; `internal/cli` only orchestrates.

## Added scope

- **Bug fix in `internal/team/write.go`'s `Add()`**, not called for by any
  task text directly: the pre-existing no-op/dedup check compared only
  hash-vs-lockfile-record and file presence, never actual on-disk bytes
  against what was about to be written. Since a `Locally modified` record's
  source hash is unchanged and the file is present by definition, `Add()`
  always treated a `--force` overwrite as an already-up-to-date no-op and
  silently skipped it — `--force` (T-31) could not function at all without
  this fix. This was necessary implementation work to satisfy AC-2, not
  scope creep; it surfaced because T-31 is the first caller to ever exercise
  `Add()` on a genuinely `Locally modified` record under `--force`.
- Additional unit tests beyond the Phase 1 set, added during Phase 3 to close
  three real gaps found on inspection: `StatusConflict` coverage in
  `team update` (previously only `StatusLocallyModified` was exercised),
  a single run proving both auto-update and refusal together (AC-1's
  same-run language), and `--force` precision (`overwritten` vs. `updated`
  labels not conflated, plus a nothing-to-force case) including table
  (non-JSON) output naming — previously only `--json` mode was tested.

## Dropped or deferred

None. All three tasks (T-30, T-31, T-32) and all three acceptance criteria
(AC-1, AC-2, AC-3) were completed in full within this run's scope.

## Architectural deviations

None. `team update` follows the same CLI seam
(`resolveTeamRepoPath`/`openTeamRuntime`/`resolveTeamIndex`) as every other
`team` subcommand, reuses US-9's write engine and US-10's classifier without
modification to either's public contract, and does not call US-18's
`removeMarkerSection` (T-50) — determined, and independently confirmed by
code review, to be correctly unreachable from this story's design:
`teamsource.RenderCurrent` re-renders one lockfile record's already-recorded
source with no roster/team-membership re-resolution, so `team update` has no
signal that a team has stopped contributing to a shared agent. That
observation is out of US-10's five-state classification (the only
classification this story uses) and is not this story's job to add.

## Verdict

CLEAN — delivered matches plan exactly; the one addition (the `Add()` dedup
bug fix) was a necessary correction to make an in-scope acceptance criterion
(`--force`, AC-2) actually work, not new scope, and is recorded above for
transparency.
