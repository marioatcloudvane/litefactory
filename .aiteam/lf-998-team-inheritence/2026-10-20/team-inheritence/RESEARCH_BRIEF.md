# Research brief

## Request

Add inheritance so a team (e.g. python) can inherit from a main team, so generic agents are edited in one place.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — Build-time resolution keeps releases self-contained and immutable. The installer takes finished packages, so no loader change is needed.
- **a2** — Whole-file override is enough; merge is not needed.
- **a3** — The base is pinned by living in the same repository; no version pinning.


## Open questions

### q1 — What should the main (base) team be?

_Asked of: Product Owner Questions_

- **a** — A new generic base source that is not released itself. Go, python, dotnet and swift all inherit from it. Cleanest; one extra folder.  *(recommended)*
- **b** — The go team is the base and the others inherit from it. Less work now, but go-specific content leaks into every team.
- **c** — Any team may name any other as base; there is no fixed main.

### q2 — Can a child team remove an inherited file?

_Asked of: Technical Questions_

- **a** — Yes, through an explicit list of excluded paths in the child's metadata.  *(recommended)*
- **b** — No, children can only add or replace files.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
