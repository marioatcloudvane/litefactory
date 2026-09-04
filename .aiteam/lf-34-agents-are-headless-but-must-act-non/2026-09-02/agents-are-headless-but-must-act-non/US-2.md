# US-2 — Requirements Engineer uses litefactory-mediated clarification

> As a feature owner, I want planning questions surfaced through litefactory so that headless planning does not mistake silence for nobody to ask.

This aligns both Requirements Engineer definitions around a headless clarification lifecycle.

## Why This Matters
Requirements discovery sometimes needs an owner decision, but the current script reads like live chat. The challenge is preserving useful clarification without waiting inside the run.

## Architecture Notes
- Replace the lifecycle across prerequisites and all phases, not isolated verbs.
- Material gaps return consolidated questions; non-material gaps permit a spec with reported assumptions.
- Preserve both wrappers and the feature-spec contract.
- Risk: partial edits could retain a hidden wait or fabricate requirements.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-2.1 | Update the Claude/Codex pair with shared headless clarification behavior | logic | — | S |
| T-2.2 | Validate gap, assumption, no-wait, and wrapper behavior | test | T-2.1 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-2.1 | Both definitions are updated at corresponding semantic locations. |
| AC-2.2 | No fill-in dialogue, confirmation wait, polling, or implied in-run response remains. |
| AC-2.3 | Missing both source inputs reports a blocker rather than fabricated requirements. |
| AC-2.4 | Non-material gaps allow completion with reported assumptions. |
| AC-2.5 | Tool-specific wrappers remain intact. |
