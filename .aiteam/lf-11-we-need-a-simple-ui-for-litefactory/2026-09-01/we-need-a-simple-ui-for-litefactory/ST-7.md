# ST-7 — Daemon log

> As the operator, I want the daemon's own operational log, filterable by
> level, so I can diagnose poll/dispatch behaviour.

Builds `GET /logs`: the daemon's own `daemon.log`, tailed, filterable by
level, with the same follow behaviour as run logs.

## Why This Matters

When something goes wrong with litefactory itself — not a specific run, but
the daemon's own polling or dispatch logic — the only place that's visible
today is a log file on disk. This page is the operator's diagnostic window
into the daemon's own health, deliberately kept off the main navigation
(it's a "when something's wrong" tool, not an everyday one) but reachable in
one click from the dashboard and settings pages. It's a small story once the
shared log-tailing machinery exists, but it has to handle a log file that's
actively being written to while it's being read, without treating a
mid-write truncated line as corruption.

## Architecture Notes

- Reuses ST-2's shared bounded tail reader (256 KiB window for
  `daemon.log`, distinct from ST-6's 4 MiB run-log window).
- `daemon.log` is slog JSON, not claude stream-json — it does not go through
  `internal/runlog`. Its own small decode step (time/level/msg + remaining
  attrs) lives in this story.
- A line that fails to parse as JSON renders verbatim as its own row rather
  than being dropped — the same posture ST-6 takes toward malformed claude
  events.
- Level filter is `>=` the selected level; `all` means no filter. The
  `<select>` is a plain GET form — no JavaScript.
- `?follow=1` behaves identically to ST-6's run-log follow: 5s refresh meta,
  present only when requested.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | slog-JSON line decode (`time`, `level`, `msg`, remaining attrs) with a non-parsing line rendered verbatim; level filter as `>=` the chosen level, `all` = no filter | logic | — | S |
| T-7.2 | `GET /logs` handler + template: 256 KiB tail, newest first, ≤500 rows after filtering, level `<select>` as a GET form, follow control, tail note, empty states | api | ST-2's T-2.8, T-7.1 | M |
| T-7.3 | Tests: `level=warn` shows warn+error only; malformed line rendered not dropped; 10 MiB file reads ≤256 KiB and shows the tail note; missing file; filter matching nothing; remaining attrs rendered | test | T-7.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | `GET /logs?level=warn` shows only `warn` and `error` rows; a malformed line renders verbatim rather than being dropped. |
| AC-7.2 | `GET /logs` against a 10 MiB `daemon.log` reads at most 256 KiB and renders the tail note. |
| AC-7.3 | `daemon.log` missing renders "The daemon has not written a log yet."; a filter matching nothing renders "No entries at this level." with the filter still selected. |
| AC-7.4 | `?follow=1` on `/logs` adds the refresh meta; without it, no refresh meta is present. |
