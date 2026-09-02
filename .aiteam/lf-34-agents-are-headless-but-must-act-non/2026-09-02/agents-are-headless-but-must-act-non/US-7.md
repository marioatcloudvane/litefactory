# US-7 — Proxy Product Owner conditionally returns scope uncertainty

> As a feature owner, I want genuine scope uncertainty surfaced while routine decomposition continues automatically.

This adds equivalent conditional routing to both Proxy Product Owner definitions.

## Why This Matters
Decomposition normally needs judgment, not questions. Contradictory or materially incomplete scope, however, can distort the whole plan if silently invented.

## Architecture Notes
- Return genuine scope uncertainty without waiting; keep routine slicing, assignments, estimates, and dependencies autonomous.
- Preserve both wrappers.
- Risk: broad wording creates a default question loop.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-7.1 | Add paired conditional scope-question routing | logic | — | S |
| T-7.2 | Validate routine autonomy and wrappers | test | T-7.1 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-7.1 | Both definitions route material incomplete/contradictory scope equivalently. |
| AC-7.2 | Genuine uncertainty is reported to the Plan orchestrator without waiting. |
| AC-7.3 | Routine decomposition completes without a question loop. |
| AC-7.4 | Tool-specific wrappers remain intact. |
