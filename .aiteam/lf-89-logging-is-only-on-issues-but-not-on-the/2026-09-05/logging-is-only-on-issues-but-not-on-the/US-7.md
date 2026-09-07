# US-7 — Carry `--grep`/`-i`/`--invert` to the app log

> As an operator, I want `--grep`/`-i`/`--invert` on the app log so that filtering works the same way it already does for issue logs.

This story wires the existing text-search flags onto the new app-log view.

## Why This Matters

Operators already know how to narrow down an issue's log with a text search. Making the same search work on litefactory's own log means there is nothing new to learn, and no reason to reach for a separate tool just because the source of the log changed.

## Architecture Notes

- Pattern to follow: reuse `lineFilter` and `newLineFilter(pattern, ignoreCase, invert)` from `cmd_logs_render.go` verbatim — nothing new is built here; this story is wiring an existing filter onto a new source, not constructing one.
- The filter applies to the rendered line, not the raw JSON, matching `renderSink.emit`'s existing rule. With `--raw`, the "rendered line" is the JSON line, which is consistent rather than a special case.
- Composes with US-5: level filtering happens first (cheap, structural, on the decoded record), grep second (on the rendered text).
- An invalid regexp is a usage error, exit 2, same as today's issue-path behavior.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Wire `--grep`/`-i`/`--invert` to the app-log rendering pipeline, applied after level filtering | code | US-3, US-5 | 2h |
| T-7.2 | Tests: `--grep`, `-i`, `--invert` on the app log match the same semantics as the issue path; an invalid regexp exits 2 | test | T-7.1 | 1.5h |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | `litefactory logs --grep tick`, `-i` and `--invert` filter app-log lines with the same semantics they have for issue logs |
| AC-7.2 | An invalid `--grep` pattern on the app-log path exits 2, same as on the issue path |
