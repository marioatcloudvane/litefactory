# Research brief

## Request

Decouple ticket management from the repository behind an interface, move project configuration (code location and ticket system) from config.yaml to the database, and add Linear as the first non-GitHub ticketing integration.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **A-1** — The code host stays GitHub for now; only ticketing is abstracted, as the issue states.
- **A-2** — Per-project settings (code location, ticket system) move to the database; global settings stay in config.yaml. Rests on F-3 and F-4.
- **A-3** — Linear is the only new concrete integration; Jira is mentioned in the rationale but out of scope, as the issue says.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
