# US-5 — Remove obsolete profile reply surfaces

> As an operator, I want messages to describe only mode-selected permissions so retired label behavior cannot confuse changes.

This story removes unreachable replies and guidance while retaining legacy-label refusal.

## Why This Matters

Dead messages imply labels can still grant permissions. Removal clarifies authority while the surviving refusal remains safe and actionable.

## Architecture Notes

- Delete only unreachable selection/mismatch replies; retain `lf:tools=` refusal.
- Platform: rendering stays in report, mappings with callers, SEC-5 prevents payload echo.
- Risk: preserve report ordering and malformed/mixed-case refusal.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Remove unreachable replies, mappings, and guidance | logic | — | S |
| T-5.2 | Update report/daemon tests for surviving refusal | test | T-5.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | Unreachable reply kinds/mappings are removed without aliases. |
| AC-5.2 | Comments no longer describe label-selected permissions. |
| AC-5.3 | Every `lf:tools=` form still refuses before dispatch. |
| AC-5.4 | Guidance never echoes label payload/profile/sentinel. |

