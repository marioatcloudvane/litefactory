# ST-5 — Projects and sessions

> As the operator, I want a list of registered repos with per-repo session
> counts, and a repo's sessions listed with state and recency, so I can pick
> the one I care about and drill into it.

Builds `GET /projects`, `GET /projects/{repoID}` and the
`GET /sessions/{sessionID}` redirect that lands on the newest run.

## Why This Matters

An operator managing several repos through litefactory currently has no way
to see "which of my projects has something running, and which issue is it
working on" without querying the database directly. This is the drill-down
path that turns "litefactory is doing something" (the dashboard) into
"litefactory is doing this specific thing, on this specific issue" — the
step before actually reading a run's log. It also has to behave sensibly for
the two states that will be common early on: a repo just registered with
nothing in it yet, and a session with no runs yet.

## Architecture Notes

- `/projects` and `/projects/{repoID}` both stay within the plan brief's
  ≤4-queries-per-page budget (one call to `ListRepoSessionCounts` /
  `ListSessionsByRepo` respectively, plus the layout's `GetPollCursor`).
- `/sessions/{sessionID}` is a redirect target, not a page of its own: `303`
  to the newest run when one exists, or an in-layout `200` "No runs yet"
  page — never a `404` for a session that legitimately has no runs.
- Unknown or non-numeric `{repoID}`/`{sessionID}` render the shared `404`
  page from ST-2, not a bare HTTP 404 with no layout.
- Repo/session rows use ST-2's status badge and relative-time template funcs
  — do not reimplement either locally.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | `GET /projects` handler + template: name, remote URL, `N sessions · M running`; empty state "No repos registered. Run `litefactory repo add`." | api | ST-2's T-2.3, ST-3's T-3.2 | M |
| T-5.2 | `GET /projects/{repoID}` handler + template: sessions newest first, state badge, relative time, empty state, 404 on unknown/non-numeric id | api | ST-2's T-2.3, ST-3's T-3.1 | M |
| T-5.3 | `GET /sessions/{sessionID}`: 303 to the newest run, or a 200 "No runs yet" page in-layout when there are none | api | T-5.2 | S |
| T-5.4 | Tests: both listings, both empty states, the redirect, the no-runs page, 404 for unknown and non-numeric ids | test | T-5.1..T-5.3 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | `GET /projects` lists every registered repo with its remote URL and `N sessions · M running`, computed from one grouped query. |
| AC-5.2 | `GET /projects/{repoID}` lists that repo's sessions newest-first with state badge and relative time; a repo with no sessions renders the empty state. |
| AC-5.3 | `GET /sessions/{id}` with runs returns `303` to the newest run; with no runs returns `200` and the "No runs yet" page. |
