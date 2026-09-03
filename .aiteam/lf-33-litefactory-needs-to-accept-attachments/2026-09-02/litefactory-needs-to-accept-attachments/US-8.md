# US-8 — Fetch attachments reliably against short-lived signed URLs

> As an operator, I want a transient authorization hiccup on a GitHub asset
> fetch to be retried once automatically, rather than silently dropping an
> attachment that would have worked a moment later, so that a flaky first
> attempt doesn't cost the agent a file it needed.

Delivers the one-shot retry and the fetch/turn timing budgets that harden
attachment fetching against short-lived signed URLs and an unconfirmed
token-scoping report.

## Why This Matters

GitHub attachment URLs are signed and expire, and there is a single, unverified
report online that litefactory's kind of GitHub credential — an app-style
installation token rather than a personal one — sometimes gets a "not found"
when downloading these files even though a personal token would have worked.
Nobody knows yet whether that is really a problem for litefactory. This story
is the cheap insurance either way: one retry, on a freshly re-resolved URL,
before giving up and reporting the attachment as unfetchable, plus a hard time
budget so a stuck fetch never holds the run open indefinitely.

## Architecture Notes

- **Resolves the CONC-6 tension, as decided in `plan_brief.md`'s Shared
  Decisions:** the feature spec places this retry and its 30s/120s timing
  budgets in `internal/github`, but this codebase's CONC-6 constraint holds that
  `internal/github` retries nothing and sleeps nowhere. Both are honoured by
  splitting the work: `internal/github`'s `FetchAsset` (built in US-3) stays
  dumb and single-shot, returning `ErrAssetUnauthorized`; this story's one retry
  — re-resolve the signed URL, refetch once — and both timeouts
  (`context.WithTimeout` off the turn's own context) live in `internal/attach`,
  one layer up. **If an implementer instead adds retry logic inside
  `internal/github`, that is a real deviation from this plan and must be logged
  in `.aiteam/AC_AMENDMENTS.md` before merge, not shipped silently.**
- The report this retry defends against — installation tokens 404ing on asset
  downloads where personal tokens succeed — is a single unconfirmed third-party
  account (2026-09-03 detour research brief), not reproduced against
  litefactory. This story is the mitigation regardless of whether it is true;
  the first real production run is the actual smoke test.
- Depends on US-3 — there is no fetch path to harden until one exists.
- A fetch that exhausts its retry or its timing budget is a rejection (US-6),
  not a turn failure.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Catch `ErrAssetUnauthorized`/404 from `FetchAsset`; retry exactly once via re-resolved URL | code | US-3 | M |
| T-8.2 | Apply 30s per-fetch / 120s per-turn budgets via `context.WithTimeout` inside `internal/attach` | code | US-3 | S |
| T-8.3 | Record which route succeeded (first attempt vs. retry) in the manifest outcome | code | T-8.1 | S |
| T-8.4 | Regression test: `FetchAsset` itself never retries or sleeps; `internal/attach` owns all retry/timeout behaviour | test | T-8.1, T-8.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | A `FetchAsset` call returning `ErrAssetUnauthorized` or 404 triggers exactly one retry via a re-resolved URL, never a loop. |
| AC-8.2 | A fetch exceeding its 30s per-file or 120s per-turn budget aborts and is recorded as a rejection, not left hanging. |
| AC-8.3 | `internal/github/surface_test.go`'s pinned method-set and no-interface tests pass with `FetchAsset` added and no retry logic inside `internal/github`. |
| AC-8.4 | The manifest records which fetch route (first attempt vs. retry) succeeded for each attachment. |
