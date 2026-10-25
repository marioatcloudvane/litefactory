# Research brief

## Request

Inventory where Go decides topology and where hard states block orchestration, then settle a light-guardrails scope for follow-up work.

## Decisions

- **d1** — Split into B1 retry and lifetimes, B2 narrowed heal, B3 no silent stops, B4 doctor repair on the existing flow, and A1-A4 for topology.
- **d2** — Owner-reply resume is a named DOT edge; without one, the origin-node behaviour remains the fallback.
- **d3** — Bug digest pin is replaced by the existing structural check.
- **d4** — Plan stage configurability (#957) is out of this issue.
- **d5** — Track B ships before Track A.
- **d6** — B2 is limited to the story comment sweep after hand repair, 'dispatch no longer matches' and 'instance already terminal'. Identity checks that guard actor or ticket ownership stay hard stops.
- **d7** — B4 extends the existing doctor repair flow (holds, drain, approval, journal) and does not rebuild it. Doctor repair holds join the lifetime list in B1.
- **d8** — Light guardrails C (#1050) and E (#1052) stay with their own issues and are out of this scope.
- **d9** — Proposed, not settled: add A5 graph-controlled workflows (own stages, gates and continuation in the DOT) after A1-A3. This is a design decision for the owner.


## Assumptions

- **a1** — Line numbers in the issue are approximate and may have drifted; the file and symbol references are assumed correct.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
