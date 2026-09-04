# US-3 — Obsolete label refusal coverage

> As an operator, I want obsolete permission labels rejected clearly so that old issue metadata cannot silently change authority.

This story replaces outdated grant expectations with comprehensive pre-dispatch refusal tests.

## Why This Matters

Old labels may remain on existing issues after the migration. They must neither regain their former power nor be ignored ambiguously, and hostile label text must not leak into external replies or logs.

## Architecture Notes

- Exercise black-box daemon behavior with existing forge and executor fakes; refusal rendering remains in `internal/report`.
- Platform: preserve SEC-5 and report-before-state semantics; assert that neither runner dispatches.
- Risk: malformed, mixed-case, and multiple labels must not create alternate grant paths.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Replace legacy label-selection expectations with pre-dispatch refusal coverage | test | — | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Every case-insensitive `lf:tools=` prefix refuses before Claude or Codex dispatch. |
| AC-3.2 | Label values cannot select, override, or grant a profile. |
| AC-3.3 | Multiple and malformed legacy values provide no alternative grant path. |
| AC-3.4 | External guidance is actionable without echoing untrusted label content. |

