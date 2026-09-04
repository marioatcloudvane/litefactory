# US-4 — Complete generated configuration

> As an operator, I want generated configuration to expose the full attachment policy so it remains an authoritative editing reference.

This story integrates merged settings and defaults into the canonical template.

## Why This Matters

The file claims completeness but omits supported limits and flags, hiding functionality and security policy.

## Architecture Notes

- Interpolate effective values from centralized defaults under CFG-3.
- Platform: include all roots, limits, agents, tiers, profiles, flags, and sandboxes; preserve CFG-4.
- Risk: avoid whitespace-only tests and removed keys.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Complete generated canonical configuration | infra | T-1.1, T-2.1, T-3.1 | S |
| T-4.2 | Add round-trip, defaults-equivalence, and completeness coverage | test | T-4.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Generated YAML shows all global ceilings with defaults. |
| AC-4.2 | Every profile shows attachment flag and agent authority. |
| AC-4.3 | No removed keys or literal secrets appear. |
| AC-4.4 | Generated YAML round-trips and agrees with defaults. |

