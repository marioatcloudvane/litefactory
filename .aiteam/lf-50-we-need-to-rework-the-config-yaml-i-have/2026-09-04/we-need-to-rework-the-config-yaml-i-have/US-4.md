# US-4 — Canonical regression restoration

> As a maintainer, I want applicable regression coverage preserved so that the schema migration does not hide unrelated breakage.

This story audits deleted tests and restores every still-valid invariant against the canonical configuration.

## Why This Matters

A breaking schema legitimately retires some tests, but a large deletion can also discard safety and edge-case coverage accidentally. Each removed case must be classified rather than copied blindly or silently abandoned.

## Architecture Notes

- Restore tests beside their owning packages using current canonical fixtures; real SQLite is allowed only for existing transactional store behavior.
- Platform: preserve TEST-1 through TEST-4, TEST-7, and TEST-10; add no mocks, dependencies, tags, or duplicate fixtures.
- Risk: never change production behavior solely to satisfy an obsolete test.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Audit deleted cases and restore every still-applicable regression against canonical YAML | test | T-1.1, T-2.1, T-3.1 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Every deleted configuration/tier case is classified as retired or still applicable. |
| AC-4.2 | Applicable tests cover strict roots/documents, bounds, ordered defaults, tier resolution, profile completeness and agent mismatch, secret rejection, runner construction, ceilings, and slice ownership. |
| AC-4.3 | Retired grant and fallback behavior appears only as refusal or canonical-behavior tests. |
| AC-4.4 | Validate is included in the canonical mode vocabulary tests. |
| AC-4.5 | No dependency, mocking framework, integration tag, or duplicate fixture is introduced. |

