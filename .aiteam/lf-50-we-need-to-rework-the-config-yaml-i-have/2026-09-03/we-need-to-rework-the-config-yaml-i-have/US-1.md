# US-1 — Canonical global settings

> As an operator, I want global daemon and repository settings grouped together so that the configuration is easier to understand and maintain.

This story moves the existing global controls beneath one clear settings section without changing what they do.

## Why This Matters

The current root mixes operational controls with major subsystems, making the file harder to scan. Moving these values is straightforward, but existing files must fail with useful migration guidance instead of mysterious unknown-key errors.

## Architecture Notes

- Keep schema, defaults, normalization, bounds, and migration diagnostics in `internal/config`; consumers receive the canonical shape directly.
- Platform: preserve CFG-1, CFG-2, CFG-5, CFG-6, and CFG-8; this is path rewiring, not a new settings service.
- Risk: explicit invalid zero values must remain distinguishable from omitted values, and retention behavior must not change accidentally.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Define the canonical settings schema, defaults, normalization, and legacy-path diagnostics | data | — | M |
| T-1.2 | Rewire CLI, daemon, and repository consumers to canonical settings | logic | T-1.1 | M |
| T-1.3 | Cover settings decoding, defaults, bounds, diagnostics, and consumers | test | T-1.1, T-1.2 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | All existing global daemon/repository keys are accepted only beneath `settings`. |
| AC-1.2 | Existing default values, validation bounds, normalization, and runtime behavior are unchanged. |
| AC-1.3 | Every legacy root key fails with an actionable canonical replacement path. |
| AC-1.4 | Explicit invalid zero values fail rather than being replaced by defaults. |
| AC-1.5 | All consumers read the canonical settings paths and focused tests pass. |

