# US-4 — Safe legacy-index migration

> As a maintainer, I want existing indexes to load predictably as the new format so that upgrades do not silently change specialist availability.

This keeps old installations working while making broad legacy availability visible for later operator refinement.

## Why This Matters

Existing configurations should survive the schema change. The risk is incorrectly guessing which old agents belong in each mode and changing runtime behavior during an upgrade.

## Architecture Notes

- Detect v1, transform in memory into canonical v2, then apply the same v2 validation as native input.
- Preserve legacy inline-then-shared ordering in every closed mode roster and mark the result legacy for inspection.
- Normal resolution never writes an index and never guesses a narrow mode mapping.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1 | Model and detect supported v1 index input | implementation | US-1, US-2 | S |
| T2 | Transform v1 agents/teams into canonical v2 in memory | implementation | T1 | M |
| T3 | Route migrated values through v2 validation and legacy provenance | implementation | T2 | S |
| T4 | Add ordered migration and no-write resolution tests | test | T1–T3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC1 | A valid v1 index resolves through an in-memory v2 representation without rewriting input. |
| AC2 | Every migrated team receives all five ordered legacy rosters, preserving former inline then shared availability. |
| AC3 | Invalid migrated content receives the same strict validation as native v2 input. |
| AC4 | CLI inspection identifies a migrated legacy result; narrowing requires an explicit operator change. |

