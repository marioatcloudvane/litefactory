# US-1 — Make mode a typed, explicit turn input

> As an operator, I want every run to carry a mode chosen by litefactory so that the workflow is observable before an agent starts.

This story establishes one validated mode value that every downstream artifact uses.

## Why This Matters

Today the system records a placeholder and delegates the real decision to the
agent doing the work. That makes routing invisible to the daemon and prevents it
from enforcing workflow rules. A closed value and an explicit request field
create the foundation for label selection and fallback classification.

## Architecture Notes

- Put the mode vocabulary in a dependency-safe leaf package usable by daemon,
  turn, store-facing code, and prompt builders without creating an import cycle.
- Preserve stored mode strings and metadata formats; historical `infer` values
  remain readable but are not valid inputs for new ordinary runs.
- Platform: avoid a duplicate string vocabulary across packages; conversion to
  display labels belongs beside the typed vocabulary.
- Risk: changing mode derivation too early would break free-text runs before the
  classifier exists, so retain a temporary compatibility adapter until US-5.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T1.1 | Add and validate the four-value mode type and canonical label mapping | Code | — | S |
| T1.2 | Carry explicit mode on turn requests and use it for runs, prompts, metadata, trailers, and ask policy | Code | T1.1 | M |
| T1.3 | Add compatibility and invalid-mode tests across turn and agent request boundaries | Test | T1.2 | S |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC1.1 | A new turn accepts exactly research, plan, implement, or spike as its concrete mode. |
| AC1.2 | The concrete request mode is identical in the run row, agent request, prompt header, metadata, commit trailer, and ask-permission decision. |
| AC1.3 | Empty, `infer`, and unknown new-run modes fail validation before worktree preparation. |
| AC1.4 | Existing persisted rows containing `infer` remain readable without a data rewrite. |
| AC1.5 | Slash-command parsing remains behaviorally compatible pending final routing integration. |

