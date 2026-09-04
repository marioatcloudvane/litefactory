# Plan Brief — we-need-to-rework-the-config-yaml-i-have-continuation
# Branch: lf/50-we-need-to-rework-the-config-yaml-i-have
# Date: 2026-09-04
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/50
# Citation base: dde65eb06ab0461e54aecc503e21c0519c2325a2
# Research brief: RESEARCH_BRIEF.md

## Shared Decisions

- The implemented canonical configuration design is fixed and is not reopened by this continuation.
- Legacy `lf:tools=` labels remain refusal-only and must never regain permission-selection authority.
- Permissions remain derived from resolved mode plus agent: Research, Plan, Implement, and Validate select like-named profiles; Spike remains Spike but selects Implement permissions.
- Claude Validate must expose no Bash, Edit, or Write tool because ambient Claude permissions are additive; Codex Validate remains `read-only`.
- Existing tests are migrated to canonical behavior. Deleted cases are restored only when they still express a valid invariant; retired behavior becomes refusal coverage rather than compatibility code.
- No new packages, dependencies, schema fields, migrations, interfaces, or runtime abstractions are introduced.

## Workflow

- Shipping order: US-1, US-2, and US-3 may proceed independently; US-4 follows their canonical coverage; US-5 follows the behavior tests so comments match proven behavior; US-6 runs review and the complete quality/delivery gates last.
- Hard coupling: mode-routing, Validate policy, and legacy-label refusal tests must agree on one fail-closed authority model before the regression audit is complete.
- All continuation changes ship together; completion requires the full repository gate plus accurate drift/context/manifest artifacts.

## Cross-Story Constraints

- Platform: preserve existing package ownership and dependency direction; configuration legality stays in `internal/config`, routing in `internal/daemon`, refusal rendering in `internal/report`, and runner-local enforcement in the existing adapters.
- Security: preserve CFG-1/4/5, SEC-1/3/5, exact profile matrices, non-disclosing errors, and no missing-profile fallback.
- Tests: use stdlib/table-driven tests and established fakes (TEST-1/2/3), deterministic traversal (TEST-7), no sleeps/polling (TEST-8/9), and do not weaken design-enforcement tests (TEST-10).
- Concurrency: preserve copied permission slices and established daemon quiescence patterns (CONC-1/2); add no goroutines.
- External tools: never execute real coding agents (EXT-1), never run `make build`, and never touch `dist/` or runtime state.
- Ring-fenced: production schema/routing redesign, other providers, config migration tooling, retention behavior, dependencies, CI configuration, and unrelated cleanup.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Automatic mode-selected profile coverage | M | — | https://github.com/marioatcloudvane/litefactory/issues/51 |
| US-2 | Read-only Validate coverage | M | — | https://github.com/marioatcloudvane/litefactory/issues/52 |
| US-3 | Obsolete label refusal coverage | S | — | https://github.com/marioatcloudvane/litefactory/issues/53 |
| US-4 | Canonical regression restoration | L | US-1, US-2, US-3 | https://github.com/marioatcloudvane/litefactory/issues/54 |
| US-5 | Accurate operational guidance | S | US-1, US-2, US-3 | https://github.com/marioatcloudvane/litefactory/issues/55 |
| US-6 | Demonstrable delivery completion | M | US-4, US-5 | https://github.com/marioatcloudvane/litefactory/issues/56 |

## Out of Scope

- Reworking or extending the already-implemented canonical schema.
- Restoring legacy permission-label grants or missing-profile fallback.
- New providers, migrations, dependencies, abstractions, or database changes.
- Unrelated tests, comments, lint configuration, CI changes, or dependency upgrades.

## Provenance

- Citation base: `dde65eb06ab0461e54aecc503e21c0519c2325a2`.
- Known-stale areas: daemon tests expecting label-selected profiles; canonical mode tests omitting Validate; comments describing the retired hierarchy and permission flow.
- Re-pinning requirements: before completion, compare the working tree to the citation base, verify every continuation-touched file, and run `make check` after the final fix.

