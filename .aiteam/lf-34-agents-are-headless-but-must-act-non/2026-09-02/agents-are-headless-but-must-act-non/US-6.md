# US-6 — Spike records a blocked-needs-decision outcome

> As a feature owner, I want a blocked Spike to preserve evidence and surface the needed decision so exploratory work is not lost.

This adds a low-frequency decision handoff to the shared Spike mode.

## Why This Matters
A Spike rarely needs clarification, but can reach a blocking decision. It must leave a useful artifact and report instead of waiting or abandoning findings.

## Architecture Notes
- Edit only the shared Spike mode; Codex uses it inline.
- Always write `SPIKE.md`; put the blocker under `## Not done`, the decision/options under `## Recommendation`, and repeat it in the final report.
- Risk: a symmetry-only Codex Spike file creates an unregistered surface.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-6.1 | Add blocked-needs-decision artifact/report behavior | logic | — | XS |
| T-6.2 | Validate sections and absence of a Codex Spike artifact | test | T-6.1 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-6.1 | A blocked Spike still writes `SPIKE.md` and preserves evidence. |
| AC-6.2 | Blocker and decision appear in the required sections and final report. |
| AC-6.3 | The mode never waits, polls, or enters another mode. |
| AC-6.4 | No dedicated Codex Spike counterpart is created. |
