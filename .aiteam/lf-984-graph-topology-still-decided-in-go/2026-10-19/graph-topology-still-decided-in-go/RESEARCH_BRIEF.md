# Research brief

## Request

Inventory where Go decides topology and where hard states block orchestration, then settle a light-guardrails scope for follow-up work.

## Decisions

- **d1** — Split into small issues: B1 retry and lifetime rules, B2 heal-instead-of-block checks, B3 no silent stops plus the Plan-to-Implement story case, B4 doctor repair, and A1-A4 for topology. Decided by me from the owner's direction.
- **d2** — Owner-reply resume is a named DOT edge; without one the current origin-node behaviour remains the fallback. Decided by me (my earlier recommendation).
- **d3** — Bug digest pin is replaced by the existing structural check; the shipped graph passes it. Decided by me (my earlier recommendation).
- **d4** — Plan stage configurability (#957) is out of this issue. Decided by me (my earlier recommendation).
- **d5** — Track B ships before Track A, because its faults are blocking sessions now. From the owner's principles 1-4 and 6.


## Assumptions

- **a1** — Line numbers in the issue are approximate and may have drifted; the file and symbol references are assumed correct.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
