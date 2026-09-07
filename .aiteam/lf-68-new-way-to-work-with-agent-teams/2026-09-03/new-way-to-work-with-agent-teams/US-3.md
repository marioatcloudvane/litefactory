# US-3 — `schemaVersion` migration on load (index schema)

> As a platform operator, I want an older team index file to keep loading correctly against a newer
> version of litefactory, and a too-new index file to fail with a clear message rather than a
> confusing crash, so that upgrading litefactory doesn't silently break my team setup.

Delivers sequential `schemaVersion` migration for the index schema (per-team inline `agents` maps,
a top-level `shared` map, each entry a `{source,dest}` pair), retargeted from the previously-planned
three-layer config shape.

## Why This Matters

Index files will change shape over time as this feature grows, and nobody wants last year's file
to stop working the day they upgrade the tool, or a newer file to fail with a stack trace instead
of a plain explanation of what's wrong.

## Architecture Notes

- Retargeted: the migration subject is now the index schema (US-1's `Index` type), not the rejected
  three-layer config shape. Migration mechanics are unchanged: hand-written sequential steps
  mirroring `internal/store/migrate.go`'s pattern.
- A `schemaVersion` newer than the binary supports is a hard load failure, never a silent downgrade
  or best-effort read.
- T-06's dependency on US-1 T-02 (the index types) still holds now that T-02 targets the index
  schema — migration operates on whatever the current `Index` type shape is.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-06 | `schemaVersion` migration framework + sequential steps against the index schema | logic | US-1 T-02 | M |
| T-07 | Tests: sequential migration application on an older schemaVersion; newer-than-supported schemaVersion fails to load | test | T-06 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | An index file written under an older schemaVersion migrates sequentially and loads correctly as the current `Index` type. |
| AC-2 | An index file with a schemaVersion newer than the binary supports fails to load with an error naming the mismatch, rather than crashing or silently truncating fields. |
