# ST-4 — Dashboard

> As the operator, I want a landing page showing daemon liveness, today's run
> tallies and recent activity, so I can see at a glance whether litefactory
> is working.

Builds `GET /`: a greeting, three status tiles, a link to the daemon log, and
up to 20 rows of recent activity linking into the run detail pages ST-6
builds.

## Why This Matters

Today, answering "is litefactory actually doing anything right now" means
running a CLI command and reading raw rows. This page is the one-glance
answer to that question — the first thing the operator sees, and the page
most likely to be open in a spare browser tab all day. Getting it wrong in
either direction is costly: too few queries and it lies about what's
happening; too many and it becomes the page most likely to slow down the
whole daemon, since every read on this page shares a database connection
with every currently-dispatching run.

## Architecture Notes

- Per the plan brief's query-budget decision, this page is the one exception
  to the "≤4 queries per page" rule: it needs six whole-result reads
  (`GetPollCursor`, `ListRepos`, `ListRepoSessionCounts`, `ListBacklogSessions`,
  `CountRunsSince`, `ListRecentRuns`). All six must still be gathered before
  any rendering begins, per ST-2's gather-then-render helper.
- "Today" is local midnight, computed from the injected clock (ST-2's `Now`
  seam), not `time.Now()` directly — this keeps the boundary testable.
- Poll tile's "next in Ns" must clamp at 0 and render `due` rather than a
  negative number.
- "Ok" in the today tile is `succeeded + reported`; "fail" is `failed` only.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | `GET /` handler: gather all six reads, build the view struct, render | api | ST-2's T-2.3, ST-3's T-3.2, T-3.3, T-3.4 | M |
| T-4.2 | Dashboard template: greeting, 3 tiles, daemon-log link, up to 20 activity rows each linking to `/runs/{id}` | api | T-4.1 | M |
| T-4.3 | Pure derivation funcs: liveness green/amber at 2× `poll_interval_seconds`; poll tile "next in Ns" clamped at 0 → "due"; today tile `X runs · Y ok · Z fail` | logic | ST-2's T-2.7 | S |
| T-4.4 | Tests: derivation table (boundary exactly at 2×, negative "next in", empty DB); handler test asserting rows, links, tiles | test | T-4.2, T-4.3 | M |
| T-4.5 | Test: `GET /` under concurrent store writers completes well inside 2s — ordinary in-package test, no integration build tag | test | T-4.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | `GET /` renders the greeting, the 3 tiles, the daemon-log link and up to 20 activity rows; each row links to `/runs/{id}`. |
| AC-4.2 | Under `max_concurrent_runs` concurrently dispatching turns, `GET /` completes in under 2s and issues no more than 6 database queries. |
