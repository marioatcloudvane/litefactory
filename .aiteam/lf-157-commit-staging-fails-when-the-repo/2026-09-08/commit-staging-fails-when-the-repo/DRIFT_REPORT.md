# Drift Report — commit-staging-fails-when-the-repo

Branch: lf/157-commit-staging-fails-when-the-repo | Date: 2026-09-08

## Delivered as planned

- US-1 / #158, frozen revision `7cc69b218735eac9a403581328344501089d914415b22bb10baf96bbdd552e62`: T-157.1 reuses the existing single staging-call fix, with both transport resets and public behavior preserved.
- T-157.2 verifies ignored nonempty/empty directories, unignored transport exclusion and byte preservation. This run closes the AC-157-5 coverage gap with direct public-method cases for already-staged transport and staged/unstaged transport-only no-ops.
- T-157.3 receives independent scoped review, focused real-Git tests, exact binary-version and scratch regression evidence, aggregate checks and the supplied per-run event protocol. TEST_RESULTS.md records the final command outcomes.

## Added scope

None. Additional test cases implement explicit AC-157-5 requirements.

## Dropped or deferred

None within the selected story. No infrastructure services or integration tags are applicable to offline Git fixtures.

## Architectural deviations

None. The production fix was already present when this run began; reusing it avoids duplicate implementation. No app-context or constraint changes were needed.

## Verdict

CLEAN — delivery matches the selected frozen story.
