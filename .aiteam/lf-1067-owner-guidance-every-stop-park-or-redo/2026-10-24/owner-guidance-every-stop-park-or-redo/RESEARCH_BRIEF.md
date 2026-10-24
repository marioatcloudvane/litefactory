# Research brief

## Request

Audit all owner-facing stop, park, refusal, waiting and redo paths beyond issue #1067's examples, establishing current behavior and reusable guidance.

## Decisions

- **d1** — Remain in Research. Audit source and record gaps; do not implement, commit, publish or change workflow states.


## Assumptions

- **a1** — Treat 'all paths' as all owner-facing workflow and factory reporting families, including scheduling, confirmations and chat; internal errors matter when they reach those surfaces.
- **a2** — The four Wanted requirements in the supplied issue define the comparison criteria. An exact command means a valid command based on current retained state, not fabricated approval or a promise that retry resets limits.
- **a3** — Source inspection establishes current message behavior; the historical incident remains owner-reported. Exhaustive branch coverage and installed-release migration choices remain for shaping and subsequent verification.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
