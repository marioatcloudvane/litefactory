# ST-3 — Store read accessors

> As the operator, I want the dashboard, projects and session pages backed by
> efficient, correct database reads, so the UI stays fast and accurate even
> while the daemon is actively dispatching runs.

Adds four read-only accessors and three result types to `internal/store`,
covering exactly the data the dashboard and drill-down pages need and
nothing more.

## Why This Matters

litefactory's database was built for the daemon's own bookkeeping, not for
answering "how many sessions does this repo have, broken down by state" in
one query — that accessor does not exist yet, and neither do a couple of
others the UI pages need. Getting these queries right matters more than it
might look: the database is shared with every currently-running turn on a
connection pool capped at exactly one, so a UI page that reads carelessly
does not just render slowly, it can make the whole daemon wait. This story
is entirely backend work with no HTTP surface of its own, which is also why
it can start on day one, in parallel with everything else.

## Architecture Notes

- No `Store` interface for testability — test against a real, migrated
  SQLite database in `t.TempDir()`, matching the codebase's existing pattern
  for `internal/store`. No fakes, no mocks.
- Placement follows the existing per-entity file convention:
  `ListSessionsByRepo` in `sessions.go`; `ListRepoSessionCounts` in
  `repos.go` (it is repo-shaped, one row per repo); `ListRecentRuns` and
  `CountRunsSince`, plus their result types, in `runs.go`.
- `ListRepoSessionCounts` MUST be one grouped query, not N+1, and MUST
  include repos with zero sessions — a repo the operator just registered and
  hasn't run yet must still appear in the "Projects" list with `0 sessions`.
- NULL mapping follows the store's existing uniform rule: a zero
  `time.Time` means NULL; use pointers only where zero is itself a
  meaningful, distinct value.
- Every accessor takes `ctx` first and is used by callers passing
  `r.Context()` — no accessor call is safe to leave un-cancellable given the
  single-connection cap.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | `ListSessionsByRepo(ctx, repoID) ([]Session, error)`, newest `updated_at` first | data | — | S |
| T-3.2 | `ListRepoSessionCounts(ctx) ([]RepoSessionCounts, error)` + type — one grouped query, one row per repo, a counter per `SessionState`, repos with zero sessions included | data | — | S |
| T-3.3 | `ListRecentRuns(ctx, limit) ([]RunSummary, error)` + type — newest `started_at` first, joined to `sessions` and `repos` | data | — | M |
| T-3.4 | `CountRunsSince(ctx, since) (RunTotals, error)` + type | data | — | S |
| T-3.5 | Tests against real migrated SQLite: ordering, empty results, repos with zero sessions present in the counts, the join carrying repo name and issue number | test | T-3.1..T-3.4 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | `ListSessionsByRepo` returns a given repo's sessions newest-`updated_at`-first, and an empty slice (not an error) for a repo with none. |
| AC-3.2 | `ListRepoSessionCounts` returns exactly one row per repo from a single grouped query, including repos with zero sessions, with `Total`/`Idle`/`Running`/`Failed` counters that sum correctly against a seeded fixture. |
| AC-3.3 | `ListRecentRuns` returns runs newest-`started_at`-first, each carrying its session's `TaskRef`/`IssueNumber` and its repo's `RepoID`/`RepoName`. |
| AC-3.4 | `CountRunsSince` returns correct `Running`/`Succeeded`/`Failed`/`Reported` totals for runs since a given timestamp, excluding runs before it. |
