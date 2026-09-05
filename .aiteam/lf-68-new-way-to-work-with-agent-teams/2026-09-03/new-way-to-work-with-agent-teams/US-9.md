# US-9 — `team add <repo-identifier> <team>` write engine

> As a platform operator, I want to add a team's agents to a target repo with one command, and know
> that either every file lands correctly or nothing changes at all, so that a failed or interrupted
> run never leaves my repo half-configured.

Delivers the write engine behind `team add`: resolve the target repo, render every agent in the
named team for every active harness, and write the results atomically, recording enough about what
was written for `team status` (US-10) to later tell a hand-edit apart from an upstream change.

## Why This Matters

Installing a team into a repo touches several files across one or more harness directories in one
step. If that step is interrupted partway — a crash, a full disk, a bad markdown file — the repo
must not end up with three files written and a fourth missing; that's a harder mess to diagnose
than the command simply having refused to run at all. This story also plants the one piece of
bookkeeping every later story that talks about drift depends on: a record, kept outside the files
themselves, of exactly what was written and from what source, so a later run can tell "someone
edited this file by hand" apart from "the file upstream changed" without needing to re-derive the
answer from state that no longer exists by then.

## Architecture Notes

- Replaces the rejected three-hash drift model entirely. What this story writes is a **provenance
  marker**, recorded per rendered file in a lockfile, not a config layer: `{agentKey, harness,
  destPath, sourcePath, sourceHash, renderedHash}` — `sourceHash` is a content hash of the markdown
  source at write time, `renderedHash` is a content hash of the bytes actually written. Two hashes,
  not three: there is no `baseSourceHash` because there is no merged-config baseline left to hash
  against (US-2). US-10 consumes this record; this story only produces it.
- **Bookkeeping directory, resolved here (Open Question 12):** the lockfile lives at
  `.litefactory/team.lock.json`, a new litefactory-owned directory distinct from `.aiteam/` (which
  is reserved for planning-session artifacts, not per-repo installed-team bookkeeping). Assumption,
  documented here since no sponsor confirmation exists: `.litefactory/` is created on first `team
  add` if absent. US-16 depends on this directory existing and adds gitignore/protection rules for
  it — it does not invent the name.
- `team add <repo-identifier> <team>` first resolves `<repo-identifier>` to a concrete registered
  repo via US-19's fuzzy resolver (exact `owner/name`, else case-insensitive substring match,
  ambiguity surfaced rather than guessed) — this story's own scope is everything after that repo is
  known.
- Render-then-write-atomically: every agent in the named team (plus every agent in `shared`, per
  US-2's implicit-availability resolution) is rendered for every active harness (US-5's contract)
  *before* any file is written; a failure at any renderer aborts the whole operation with nothing
  written. Writes themselves happen under a per-repo lock on the lockfile (`flock`-style), so two
  concurrent `team add` invocations against the same repo serialize rather than race.
- An agent whose renderer output is a marker-tagged root-file section (US-18) is written via US-18's
  insertion mechanics, not as a standalone file — this story's atomic-write guarantee covers both
  output shapes uniformly (all sections and all standalone files in one all-or-nothing batch).
- Re-running `team add` for a team already fully installed, with nothing changed upstream, is a
  no-op: the write engine compares the fresh render against the current lockfile record first, and
  skips the write when both `sourceHash` and `renderedHash` already match.
- Depends on US-18 (marker-tagged-section writes) and US-19 (repo resolution) landing first, per the
  Story Index. Atomic-write/lockfile mechanics themselves are unchanged from the original design
  intent — only the record's shape (two hashes, not three) and its location (`.litefactory/`, not
  `.aiteam/`) changed.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-18 | Lockfile schema (`.litefactory/team.lock.json`): per-file `{agentKey,harness,destPath,sourcePath,sourceHash,renderedHash}` records | data | US-1 T-02 | M |
| T-19 | `.litefactory/` bookkeeping directory bootstrap (create on first write if absent) | setup | T-18 | XS |
| T-20 | `<repo-identifier>` resolution integration (calls US-19's fuzzy resolver) | logic | US-19 | S |
| T-23 | Atomic write engine: render-all-then-write-all-or-nothing, per-repo lockfile lock, no-op short-circuit when nothing changed | logic | T-18, T-20, US-18 | L |
| T-24 | Tests: idempotent re-add is a no-op; concurrent `team add` invocations serialize via lock; a renderer failure partway leaves zero files written | test | T-23 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `team add <repo-identifier> <team>` writes every agent in the named team plus every `shared` agent, for every active harness, or writes nothing at all if any renderer fails. |
| AC-2 | Each written file has a corresponding lockfile record at `.litefactory/team.lock.json` carrying `sourceHash` and `renderedHash`. |
| AC-3 | Two concurrent `team add` invocations against the same repo do not interleave writes; the lockfile never ends up with a torn or partial record. |
| AC-4 | Re-running `team add` for a team already installed, with no upstream change, writes nothing and reports a no-op. |
