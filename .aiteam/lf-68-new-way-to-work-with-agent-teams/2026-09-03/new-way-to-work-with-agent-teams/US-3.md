# US-3 — `schemaVersion` migration on load

> As a platform operator, I want an older on-disk team config to keep loading correctly against a
> newer version of litefactory, so that upgrading the binary doesn't silently break an existing
> customization.

This story adds sequential, hand-written migration of a team config's `schemaVersion` at load
time, and a hard refusal when the file declares a version the binary doesn't understand yet.

## Why This Matters

A repo's `.aiteam/team.json` or an organization's config file can sit untouched for a long time
while litefactory itself keeps shipping new releases. Without a migration path, the day the
schema needs to change is the day every existing customization either breaks outright or — worse
— gets silently misread. This story makes that upgrade boring: old files keep working, and a file
from the future (newer than the running binary understands) is rejected clearly rather than
half-interpreted.

## Architecture Notes

- Hand-written sequential migration steps, mirroring the existing pattern in
  `internal/store/migrate.go` — no migration library, none is needed or wanted here.
- A `schemaVersion` newer than the binary supports is a hard load failure, never a silent
  downgrade or best-effort read.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-06 | `schemaVersion` migration framework, hand-written sequential steps | logic | US-1 T-02 | M |
| T-07 | Tests: sequential migration application; a `schemaVersion` newer than the binary supports fails to load | test | T-06 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A config declaring an older `schemaVersion` migrates sequentially and loads correctly. |
| AC-2 | A config declaring a `schemaVersion` newer than the binary supports fails to load, with an error naming the mismatch, rather than being silently misinterpreted. |
