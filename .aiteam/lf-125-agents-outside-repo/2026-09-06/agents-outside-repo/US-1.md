# US-1 — Team-first index schema and strict loader

> As an operator, I want a team-first index with named teams and mode rosters so that I can see which specialists are available in each workflow state.

This replaces an all-agent index with an auditable, structured description of each team without guessing role placement.

## Why This Matters

Teams need a clear view of which specialists a run can receive. The hard part is accepting legacy installations while rejecting malformed or unsafe definitions early, before a coding process starts.

## Architecture Notes

- Create `internal/teamsource`; it owns schema v2 decoding, one-tier selection, strict validation and typed errors.
- Schema keys are `schemaVersion`, shared `agents`, and team-keyed `teams`; a team has a single-line description and declared non-empty mode rosters.
- Validate closed mode vocabulary through the existing mode type, not a duplicate string list. Preserve roster order; sort only map-derived diagnostics.
- Apply CFG-1/CFG-5, TEST-7 and root/path ownership rules; do not add `$LF_HOME` logic or vendor-specific code.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1 | Define v2 types, source-tier input and typed errors | implementation | — | M |
| T2 | Decode one complete index tier with strict nested validation | implementation | T1 | M |
| T3 | Add narrow embedded Go-team fixture/index | implementation | T2 | S |
| T4 | Add table-driven schema/error-order tests | test | T1–T3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | V2 accepts only a positive supported schema version, shared registry and team map with strict unknown-field rejection at every level. |
| AC2 | A team requires a bounded single-line description and non-empty declared mode rosters using the existing closed modes. |
| AC3 | Embedded/static/repository choices select one complete tier and never merge. |
| AC4 | Invalid keys, modes, descriptions and empty rosters produce stable dotted-path diagnostics. |

