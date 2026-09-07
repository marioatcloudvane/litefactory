# US-8 — Config defaults for auto-sync and the behind cap

> As an operator running litefactory across many repositories, I want sensible
> defaults for whether branches auto-sync and how far behind is too far, so
> that most projects need zero configuration and the ones that don't can still
> tune it.

This story adds the three config knobs the rest of the feature reads:
whether auto-sync runs at all, how far behind base is too far to auto-merge,
and whether a conflict blocks the run.

## Why This Matters

A feature that merges code on someone's behalf has to have an opinion about
when it's safe to do that without being asked, and that opinion has to be
something an operator can see and change, not something buried in code. This
story is where that opinion lives: sync is on by default because staying
behind quietly is the problem the whole feature was built to fix, blocking on
conflict is on by default because guessing wrong is worse than stopping, and
the "how far behind is too far" number is deliberately a judgment call, not a
measurement — the research behind this plan never established a data-backed
threshold, and the plan says so plainly rather than dressing up a guess as
science.

## Architecture Notes

- Package placement: `internal/config`, following existing patterns for
  bool/int fields with defaults applied at load time — stdlib and `yaml.v3`
  only, no new dependency (`purity_test.go` enforces this).
- Fields: `AutoSyncBase bool` (default `true`), `AutoSyncMaxBehind int`
  (default `100`), `BlockRunOnConflict bool` (default `true`).
- `AutoSyncMaxBehind` semantics, exactly as the architect resolved them: `0`
  means no cap (never skip on distance alone), a negative value is a **config
  load error**, never silently clamped to `0` or to the default (CFG-6). The
  default of `100` is explicitly documented in code and in operator-facing
  docs as a judgment call, not derived from measurement — do not let it read
  as if it were.
- These three fields are read by US-2's `syncBase` and are overridable per
  issue only by US-7's `lf:sync=` label (which only ever overrides
  `AutoSyncBase`'s effective value for one issue, never `AutoSyncMaxBehind` or
  `BlockRunOnConflict` — those two have no per-issue override in this plan).
- Config docs update: whichever file documents existing config keys today
  gains these three, including the "why 100, why not measured" note.
- NOT in scope: the `lf:sync=` label itself (US-7); reading these fields
  inside `syncBase` (US-2, which owns the decision tree that consumes them);
  any per-project or per-issue hierarchy beyond what US-7 already provides
  (explicitly out of scope for the whole plan, see `plan_brief.md`).

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Add `AutoSyncBase`, `AutoSyncMaxBehind`, `BlockRunOnConflict` fields with defaults | Code | — | S |
| T-8.2 | Reject negative `AutoSyncMaxBehind` at load time as a config error | Code | T-8.1 | XS |
| T-8.3 | Document the three keys, including the "100 is judgment not measurement" note | Doc | T-8.1 | XS |
| T-8.4 | Tests: default values, `0`=no-cap, negative=error, explicit `true`/`false` override | Test | T-8.1, T-8.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | An unconfigured repository loads `AutoSyncBase=true`, `AutoSyncMaxBehind=100`, `BlockRunOnConflict=true` with no config file changes required. |
| AC-8.2 | `auto_sync_max_behind: 0` loads successfully and means no cap; a negative value fails config load with a clear error and never falls back to a default or to zero. |
