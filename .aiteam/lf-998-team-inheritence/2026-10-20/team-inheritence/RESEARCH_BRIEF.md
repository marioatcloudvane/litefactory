# Research brief

## Request

Add inheritance so a team (e.g. python) can inherit from a main team, so generic agents are edited in one place.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — Inheritance would most naturally be resolved at release build time so releases stay self-contained and immutable. Not verified against the runtime loader.
- **a2** — The 'main' team is the go-development source in internal/teamsource, since the others match it most. Unconfirmed.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
