# US-1 — Automatic mode-selected profile coverage

> As an operator, I want every workflow mode to select the intended permissions automatically so that runtime authority is predictable.

This story proves the implemented mode-to-profile routing for both coding agents.

## Why This Matters

The configuration redesign removed a separate permission label and made mode the authority selector. Without end-to-end routing coverage, a future fallback or ordering mistake could silently grant the wrong permissions.

## Architecture Notes

- Extend established daemon fixtures; profile selection stays in `internal/daemon` and runners only consume resolved permissions.
- Platform: preserve copied slices under CONC-1 and use existing quiescence helpers rather than sleeps under TEST-8/9.
- Risk: Spike must remain recorded as Spike even while borrowing Implement permissions.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | Add complete daemon mode-to-profile routing coverage for Claude and Codex | test | — | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | Research, Plan, Implement, and Validate select their like-named profile for Claude and Codex. |
| AC-1.2 | Spike selects Implement permissions for both agents while remaining recorded as Spike. |
| AC-1.3 | Tests assert the resolved profile and permission payload, not merely fake invocation. |
| AC-1.4 | Missing required runtime data cannot fall back to broader authority. |

