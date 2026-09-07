# US-5 — Team inspection and explicit migration CLI

> As an operator, I want to inspect a team’s mode rosters and explicitly write a migration so that agent availability is easy to audit and upgrades remain under my control.

This adds transparent operational visibility without copying specialist files into repositories.

## Why This Matters

Operators need to confirm which specialists are available before a run starts. Migration must be a deliberate action so an upgrade never rewrites configuration without review.

## Architecture Notes

- Add a `team` command group following existing CLI constructor/registration conventions; `teamsource` retains all interpretation and serialization.
- `list` and `show` render descriptions, declared modes, roster order, ref/direct status, resolved source and provenance deterministically.
- Explicit migration writes canonical v2 atomically with restrictive permissions; normal commands are read-only and must not mutate registered clones.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1 | Add team command group and list/show argument handling | implementation | US-1, US-2, US-4 | M |
| T2 | Render deterministic text/JSON inspection views | implementation | T1 | M |
| T3 | Add explicit migration writer with safe output handling | implementation | T1 | M |
| T4 | Add CLI ordering, error and explicit-write tests | test | T1–T3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | Team list/show display descriptions, available modes, ordered entries, ref/direct form and resolved sources deterministically. |
| AC2 | Mode filtering and missing resource errors are actionable and do not start a coding run or mutate input. |
| AC3 | Migration writes a v2 document only by explicit command and leaves registered clones untouched. |
| AC4 | Text and JSON output sort maps but retain authored roster order. |

