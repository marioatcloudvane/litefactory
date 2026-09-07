# US-6 — An unknown cost renders "unknown"

> As a repository owner, I want a run whose cost is unknown to say "unknown"
> rather than omit the line, so that an absent cost is not silently
> indistinguishable from a free run.

Fixes the cost line in the report renderer so a run with no recorded cost
explicitly says "unknown" instead of dropping the line entirely.

## Why This Matters

A failed run's report currently has no cost line at all — which looks exactly
like a run that legitimately cost nothing. Before scoping a fix, this story
confirmed by reading the code that the cost data isn't actually missing
anywhere in storage — it's already stored correctly as "unknown" — the bug is
purely in how the report displays it. That check also settled a separate open
question about token usage: no token counts are recorded anywhere in the
codebase, so there's nothing to display, and adding that would be a new
feature rather than this bug fix, so it stays out of scope here.

## Architecture Notes

- Confirmed by reading the code, not assumed: this is a display gap, not a
  storage gap. Cost is already stored as a nullable value guarded by a
  has-cost flag, so "unknown" is already persisted correctly for a failed run.
  The defect is a `> 0` comparison in the report renderer's cost branch, which
  makes "unknown" and "exactly zero" both render as no Cost line — unlike Exit
  status and Duration in the same function, which already render an explicit
  "unknown."
- Fix: replace the `res.CostUSD > 0` branch in
  `internal/report/run.go`'s `metadataList` with a `HasCost`/`HasResult`-driven
  three-way, matching the existing sibling patterns in the same function. Do
  not touch the underlying storage — the persistence side is already correct.
- Fully independent of every other story in this plan — no shared files, no
  ordering constraint. Good first task or parallel task.
- Token counts: confirmed absent from every layer of the codebase (agent
  results, harness, store, report, CLI) — out of scope, not deferred; there is
  nothing to display.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Replace the `res.CostUSD > 0` branch in `internal/report/run.go`'s `metadataList` with a `HasCost`/`HasResult`-driven three-way, matching the existing "Exit status: unknown" / "Duration: unknown" sibling patterns | logic | — | S |
| T-6.2 | Table tests: reported non-zero cost renders the amount; reported zero cost renders `$0.0000`, not "unknown"; no terminal result renders "unknown"; the Cost row is never silently omitted | test | T-6.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | A run with no reported cost renders `- **Cost:** unknown`; a run reporting exactly `0` renders `- **Cost:** $0.0000`; a run reporting a positive amount renders as it does today. |
