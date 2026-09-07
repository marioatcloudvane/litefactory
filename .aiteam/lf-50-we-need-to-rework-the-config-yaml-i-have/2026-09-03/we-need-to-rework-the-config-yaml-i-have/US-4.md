# US-4 — Ordered default agent

> As an operator, I want the first configured agent to be the default so that ordering the file directly expresses my preference.

This story preserves YAML agent order and uses it consistently for unlabelled work and classification.

## Why This Matters

Removing an explicit default simplifies the file, but ordinary mappings in the implementation do not preserve deterministic order. The parser must retain declared order without weakening strict YAML validation or allowing duplicates.

## Architecture Notes

- Inspect YAML mapping nodes for agent order alongside the existing strict typed decode; expose an ordered value/accessor rather than ranging over a Go map.
- Platform: preserve CFG-1 and TEST-7, keep YAML-specific work in `internal/config`, and pass the resolved default through existing daemon wiring.
- Risk: aliases, duplicate keys, hard-coded Claude fallbacks, and inconsistent typed/order views can make routing nondeterministic.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Preserve and expose configured-agent declaration order during strict decoding | logic | T-3.1 | L |
| T-4.2 | Use the ordered default in runner registration, routing, and classification | logic | T-3.2, T-4.1 | M |
| T-4.3 | Cover ordering, duplicates, default routing, classification, and explicit overrides | test | T-4.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | The first declared YAML agent is the default for unlabelled runs and mode classification. |
| AC-4.2 | Reordering agent keys deterministically changes only default selection. |
| AC-4.3 | Explicit agent labels still select any configured non-default agent. |
| AC-4.4 | Empty agents, duplicate keys, unsupported names, and typed/order inconsistencies fail before startup. |
| AC-4.5 | Strict known-field and single-document decoding remains effective while order is extracted. |

