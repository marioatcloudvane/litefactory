# Research brief

## Request

Trace B1–B4 in issue #1049, establish their origins and current behavior, and explain improvements and verification needed.

## Decisions

- **d1** — Research only: supply diagnosis and advisory how-to; do not change code, commit, publish issues or claim implementation acceptance.


## Assumptions

- **a1** — The intended direction is to retain full safely captured evidence while keeping inline excerpts bounded, based on the supplied issue; it does not authorize removing execution, disk or model isolation safeguards.
- **a2** — Current source is the behavioral baseline; the issue's 032c55a6 line references are historical. No claim is made about production incidence or the assertion that failures are usually in the middle.
- **a3** — Artifact retention, full remote-thread coverage, untracked file bodies and target-body overflow policy are shaping decisions. Recommendations in the comment are advisory, not settled architecture.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
