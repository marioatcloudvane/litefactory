# US-9 — Persist the sync outcome on the run record

> As the owner of a task, I want the outcome of each sync attempt saved with
> the run, so that the history of what happened to my branch survives past the
> comment that first reported it.

This story adds the storage that lets US-3's disclosure and US-4's refusal
be reconstructed later, not just announced once.

## Why This Matters

A comment posted once is easy to lose track of — scrolled past, notified
away, or simply not the thing someone opens six issues later when they want
to know why a branch looks the way it does. Persisting the sync outcome
alongside the run itself means the answer to "did this run touch my branch,
and why" survives independently of whether anyone was watching when the
comment went out, and it's what lets US-3's disclosure logic read a
consistent record instead of re-deriving the outcome from scratch every time.

## Architecture Notes

- Package placement: `internal/store`, following the project's existing
  convention of one embedded `.sql` migration file per schema change.
- New migration: `0005_run_sync.sql`, applied on top of `0004` (the current
  latest — verify this number against the actual latest migration file before
  writing it; do not assume `0004` is still current without checking, since
  time may have passed since this plan was written).
- New columns on the run row: `sync_outcome` (text: `up_to_date`, `synced`,
  `blocked`, `skipped`, `failed`), `sync_behind` (int), `sync_ahead` (int),
  `sync_commit_sha` (text, nullable — populated only on `synced`).
  Conflicting-file paths themselves are not persisted as a separate column in
  this story; they are rendered once, at refusal time, from US-2's live
  `MergePreview` result, not reloaded from storage. If a later run needs the
  historical conflict list this is a gap to flag, not one to silently fill by
  adding a column beyond what this story's AC calls for.
- Integration point: written once, immediately after `syncBase` (US-2)
  completes and before `Execute` proceeds to the comment-rendering step that
  reads it back for US-3/US-4.
- Migration test invariant: existing migration-ordering tests must continue
  to pass with `0005` applied after whatever is currently the latest
  migration — do not renumber or reorder an existing migration file to make
  room.
- NOT in scope: reading these fields back into `report.Sync` (US-3's job);
  storing the conflict-path list (explicitly deferred, see above); any schema
  change unrelated to sync.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-9.1 | Confirm current latest migration number, add `000N_run_sync.sql` with the four new columns | Code | — | S |
| T-9.2 | Write the four sync fields to the run row after `syncBase` completes | Code | US-1 T-1.2, US-2 T-2.5, T-9.1 | M |
| T-9.3 | Tests: migration applies cleanly on top of the current latest, round-trip write/read of all five outcomes | Test | T-9.1, T-9.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-9.1 | The new migration applies cleanly on a database already at the current latest migration, and existing migration-ordering tests continue to pass unmodified. |
| AC-9.2 | After a run completes, its row carries `sync_outcome`, `sync_behind`, `sync_ahead`, and — only when `sync_outcome=synced` — a non-null `sync_commit_sha`. |
| AC-9.3 | For every one of the five outcome values, a round-trip write followed by a read returns the same values that were written. |
