# US-7 — Regression and delivery closure

> As a maintainer, I want merged regressions and quality gates resolved so the redesign ships with trustworthy evidence.

This story audits the combined branch and completes review and artifacts.

## Why This Matters

Several merges changed configuration and tests after the original implementation. The final state must be verified as one system, not as historically passing commits.

## Architecture Notes

- Compare final work against audit head and keep fixes with owners; persistent context states current facts only.
- Platform: preserve TEST-10; final gate is `make check`, never `make build`.
- Risk: do not hide lint failures via config changes, upgrades, or unrelated cleanup.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Audit and restore canonical regressions/comments | test | T-1.2, T-2.2, T-3.2, T-4.2, T-5.2, T-6.2 | L |
| T-7.2 | Run review/full gates and resolve in-scope failures | test | T-7.1 | M |
| T-7.3 | Refresh drift, context, and manifest artifacts | infra | T-7.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | Canonical regressions cover roots, bounds, order, tiers, profiles, secrets, ceilings, runners, attachments, and slices. |
| AC-7.2 | Code review has no blockers. |
| AC-7.3 | Formatting, vet, pinned lint, race tests, and `make check` pass. |
| AC-7.4 | Gates do not write dist, run live agents, or alter runtime state. |
| AC-7.5 | Drift, context, and manifest artifacts match final state. |

