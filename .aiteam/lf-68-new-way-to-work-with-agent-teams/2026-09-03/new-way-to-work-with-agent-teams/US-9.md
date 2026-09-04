# US-9 — `team add` (hashing, lockfile, atomic writes, install)

> As a platform operator, I want `litefactory team add <team>` to generate every harness file a
> team needs and record exactly what it generated, so that I have a reliable starting point and
> something to detect drift against later.

This story is the write engine of the whole feature: hashing, the lockfile format, atomic writes,
and the `team add` command itself.

## Why This Matters

This is the first point in the plan where litefactory actually writes inside someone else's repo
— which is new, deliberate, and carries real risk if done carelessly: overwriting a file the
operator didn't know was already there, or leaving a repo half-written if something fails midway.
This story is where the plan's hashing and lockfile design gets built for the first time — every
later story that talks about drift or updates is reading the record this story creates. Getting
the atomicity right (everything written, or nothing) and refusing to clobber an unrelated file
that happens to already exist at a target path are the two things this story cannot get wrong.

## Architecture Notes

- The hashing/lockfile engine here (T-18, T-19, T-20, T-23, T-24) must land **before** US-4's CLI
  scaffold (T-25), since that scaffold's error translation depends on this story's sentinel
  errors; this story's own CLI wiring (T-28) lands **after** T-25. This story spans both sides of
  that coupling — see `plan_brief.md`'s Workflow section.
- The three-hash model (`renderedHash`, `sourceHash`, `baseSourceHash`), the `hashVersion` field,
  and the lockfile's structural invariants (sorted entries, no duplicate paths, path validation
  against traversal) are Shared Decision 1 in `plan_brief.md` — this story is their first
  implementation, not the place they are defined or re-argued.
- Write ordering is fixed: every artifact is written via temp-file-then-rename; the whole command
  is all-or-nothing; the lockfile itself is written last, only after every artifact has succeeded.
- Every write target is checked with `os.Lstat` first and refused if it is a symlink — this closes
  a real path to writing outside the five Owned Paths via a symlink planted at a target location.
- `team add` refuses outright if a lockfile already exists for this repo, and points the operator
  at `team update` instead of silently overwriting.
- Use `internal/gittest`'s `AssertUnchanged` / `Capture`+`Snapshot.Diff` to prove `--dry-run`
  writes nothing and that a failed `team add` leaves no partial install — not hand-rolled
  directory walks.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-18 | Canonical hash input + `hashVersion`; `renderedHash`/`sourceHash`/`baseSourceHash` computation | logic | US-2 T-08, US-5 T-13 | M |
| T-19 | Lockfile types, marshal, load-time invariant validation incl. path-traversal rejection | data | T-18 | M |
| T-20 | Tests: lockfile round-trip, sortedness, traversal-path rejection, newer-schema refusal | test | T-19 | M |
| T-23 | Atomic write layer: `os.Lstat` symlink refusal, owned-path allowlist at the write site, temp+rename, lockfile written last | logic | T-19 | M |
| T-24 | Tests: symlink refusal; no partial install on mid-loop failure; `--dry-run` writes nothing (`gittest.AssertUnchanged`) | test | T-23 | L |
| T-28 | `team add` command incl. `--dry-run`, `--adopt` | api | US-4 T-25, T-23 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `team add` creates the harness artifacts plus a lockfile with one entry per artifact, each carrying `renderedHash` and `sourceHash`. |
| AC-2 | `team add` against a pre-existing, unmanaged file at an owned path aborts, names the path, and leaves the filesystem unchanged. |
| AC-3 | `team add --adopt` overwrites that file and records it; a following `team status` reports it clean. |
| AC-4 | A renderer failure partway through `team add` leaves the filesystem unchanged — no partial install, no lockfile written. |
