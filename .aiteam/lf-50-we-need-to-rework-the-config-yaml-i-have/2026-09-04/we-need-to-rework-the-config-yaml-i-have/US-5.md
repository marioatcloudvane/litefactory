# US-5 — Accurate operational guidance

> As an operator, I want comments and generated guidance to describe current behavior so that configuration changes are safe and unsurprising.

This story removes stale descriptions of the legacy hierarchy and permission-selection model.

## Why This Matters

The generated file and source-level operational comments are the primary reference for this young project. Incorrect guidance can be more dangerous than missing prose because it encourages configurations that are now refused or ignored.

## Architecture Notes

- Update only comments and guidance proven stale; generated content remains governed by production round-trip/default tests.
- Platform: comments state current invariants under STYLE-1; do not rename types merely for prose consistency.
- Risk: distinguish false operational guidance from harmless historical identifiers and preserve comment-only runtime neutrality.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Correct stale production comments and verify generated configuration guidance | logic | T-1.1, T-2.1, T-3.1 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | Comments no longer describe root-level Claude configuration, label-selected profiles, or Spike as the only workflow mode. |
| AC-5.2 | Generated configuration remains complete, decodes, validates, and agrees with runtime defaults where applicable. |
| AC-5.3 | Comment and guidance cleanup does not alter runtime behavior. |

