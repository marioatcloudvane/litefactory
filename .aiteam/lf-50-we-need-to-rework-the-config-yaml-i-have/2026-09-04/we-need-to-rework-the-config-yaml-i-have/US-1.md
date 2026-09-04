# US-1 — Global optional attachment ceilings

> As an operator, I want attachment limits in global settings so that they apply consistently to every coding agent.

This story relocates the four limits while preserving their defaults and bounds.

## Why This Matters

Litefactory ingests attachments before either agent runs, so Claude ownership is misleading and breaks Codex-only configuration. Omission must stay convenient without masking invalid explicit values.

## Architecture Notes

- Keep schema/defaults/bounds in `internal/config`; daemon setup consumes canonical settings.
- Platform: preserve CFG-1/2/5/6; legacy locations fail strict decoding and omission means defaults.
- Risk: do not change numeric values, ingestion behavior, or add aliases.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Relocate optional attachment ceilings to global settings | infra | — | S |
| T-1.2 | Cover explicit, omitted, bounded, zero, and legacy-location loading | test | T-1.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | All four ceilings are accepted only beneath `settings`. |
| AC-1.2 | Omission applies current defaults. |
| AC-1.3 | Zero and out-of-range values fail with canonical paths and aggregate errors. |
| AC-1.4 | Legacy Claude keys are rejected without alias or rewrite. |
| AC-1.5 | Codex-only configuration receives the same defaults. |

