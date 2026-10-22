# Research brief

## Request

Shape issue #1052 into a buildable scope covering owner publication policy and configurable worker limits with recoverable timeouts.

## Decisions

- **d1** — Shape only: define scope and ask material policy questions; do not implement, commit, push or create stories.
- **d2** — Preserve the five-draft cap and all owner identity, immutable content, freshness and duplicate protections.
- **d3** — Use one resolved invocation allowance across provider execution, setup, waits and relaunches.
- **d4** — Retain validated submissions, local work and diagnostics without treating timeout evidence as completion or publication authority.
- **d5** — Use existing configuration, node attributes, publication records and attempt result surfaces rather than introduce a separate control interface.
- **d6** — Keep resource admission and billing policy outside this scope.
- **d7** — Reject invalid policy at admission and preserve the resolved policy across restart without resetting an invocation allowance.
- **d8** — Keep existing invocations and final publication outcomes under their recorded policy rather than silently migrate or replay them.


## Assumptions

- **a1** — Use the saved research findings against checkout 6dc340a9 as the factual baseline; no production-state claims are added in shaping.
- **a2** — Keeping work means recoverable files, validated submissions and diagnostics without granting engineering acceptance or publication.
- **a3** — The proposed 120-minute Implement default and four-hour ceiling are policy recommendations rather than measured production requirements.
- **a4** — A cooperative grace within the budget and a separate 30-second evidence cleanup are reasonable bounded defaults, subject to owner review of this scope.


## Open questions

### q1 — Should authenticated owner publication be exempt from the repository volume quota?

_Asked of: Product Owner Questions_

- **exempt** — Recommended: remove the daily volume check for authenticated factory owner publication, retaining five drafts per proposal and all identity, freshness and duplicate checks.  *(recommended)*
- **configurable** — Keep an operator-configurable owner-publication quota, defaulting to 20 charged artifacts in a rolling 24 hours; approved work can still be refused for volume.

### q2 — How long should an owner approval remain eligible for admission?

_Asked of: Product Owner Questions_

- **day_admission** — Recommended: default to a configurable 24 hours from the owner comment, permit settings from 30 minutes to seven days, and let durably admitted immutable publication finish later; changed proposals always need fresh approval.  *(recommended)*
- **short_admission** — Keep 30 minutes for initial admission, but allow durably admitted immutable publication to finish later; delayed processing can still require fresh approval.
- **strict_day** — Use a configurable 24-hour window and recheck expiry before every publication effect; slow or interrupted publication may require renewed approval.

### q4 — What should happen after a worker exhausts its time allowance?

_Asked of: Product Owner Questions_

- **owner_resume** — Recommended: retain an incomplete timeout attempt, pause automatic retries, keep it separate from engineering repair failures, and require explicit owner resume for a fresh allowance on the retained worktree.  *(recommended)*
- **count_failure** — Retain progress but count timeout against the existing failed-attempt allowance and follow the current retry or failure edge; repeated timeouts can consume repair capacity.

### q3 — Which default worker durations and operator ceiling should this scope adopt?

_Asked of: Technical Questions_

- **two_hours** — Recommended: Implement story workers get 120 minutes, other workers keep 30 minutes, explicit node durations such as 15-minute Validate repair win, and the default operator ceiling is four hours with a configurable absolute maximum of 24 hours; node and provider use the same resolved duration.  *(recommended)*
- **one_hour** — Use 60 minutes for Implement, retain other and explicit limits, and default to a two-hour operator ceiling; this limits occupancy but may still interrupt long stories.
- **config_only** — Add role and node configuration with aligned clocks but keep shipped defaults at 30 minutes and the operator ceiling at one hour; operators must opt into longer runs.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
