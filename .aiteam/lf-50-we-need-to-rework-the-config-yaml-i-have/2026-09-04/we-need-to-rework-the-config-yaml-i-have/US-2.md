# US-2 — Mode-specific attachment defaults

> As an operator, I want attachment access to follow workflow mode so research inputs are available without widening implementation or validation by default.

This story makes attachment defaults explicit for every profile.

## Why This Matters

The pipeline exists but generated profiles currently hide and disable it. Visible defaults clarify security policy while retaining explicit operator overrides.

## Architecture Notes

- Keep attachment permission in existing profiles, not ingestion code.
- Platform: Research/Plan true, Implement/Validate false, Spike uses Implement; profiles remain total/fail-closed.
- Risk: omitted booleans must not create permissive fallback.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Set canonical attachment defaults by workflow profile | infra | — | XS |
| T-2.2 | Cover defaults, overrides, and Spike-to-Implement selection | test | T-2.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | Research and Plan default enabled. |
| AC-2.2 | Implement and Validate default disabled. |
| AC-2.3 | Spike remains Spike and inherits disabled Implement policy. |
| AC-2.4 | Explicit canonical flags override defaults without weakening required profiles. |

