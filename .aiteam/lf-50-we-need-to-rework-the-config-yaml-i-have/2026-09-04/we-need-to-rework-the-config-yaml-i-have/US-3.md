# US-3 — Profile-only Codex sandbox

> As an operator, I want each mode profile to be the sole Codex sandbox authority so no hidden fallback can widen execution.

This story removes the redundant agent-level sandbox field.

## Why This Matters

Every agent already requires a sandbox in every mode profile. A second default source is confusing and risks fallback behavior.

## Architecture Notes

- Remove the field from schema/defaults/validation/template/comments; the adapter consumes resolved permissions.
- Platform: strict rejection is the migration; preserve Validate read-only and mandatory profiles.
- Risk: do not replace it with empty-value fallback or invoke real Codex.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Remove Codex default sandbox and make profiles authoritative | infra | — | S |
| T-3.2 | Cover Codex-only defaults, removed-key rejection, and Validate policy | test | T-1.1, T-3.1 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | The field is absent from schema, defaults, template, and runtime. |
| AC-3.2 | The removed key fails strict decoding. |
| AC-3.3 | Every mode gets sandbox only from its profile. |
| AC-3.4 | Codex-only config defaults limits and rejects workspace-write Validate. |

