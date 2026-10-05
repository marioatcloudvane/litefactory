# Research brief

## Request

Inventory retirement of old team CLI, installation and distribution, quantify database/code removal, and list removal of internal/teamsource tests while preserving internal/teamsource production definitions and forwarding until #317.

## Decisions

- **d1** — Owner requests retirement of old CLI and installation functionality now; this bounded pass researches it and does not implement or create child issues.
- **d2** — Preserve internal/teamsource production assets and agent forwarding; owner explicitly requests a way forward removing tests in that package.


## Assumptions

- **a1** — Treat the direct owner instruction as current intent over the older rollout description. Keep runtime permission/result/provenance contracts and graph storage.
- **a2** — Interpret 'dont remove the stuff in internal/teamsource' as preserving all production files initially, with explicit exception for requested test removal. This limits deletion of dependencies; shaping may distinguish protected built-in assets from installation helpers.
- **a3** — No live daemon state, installed operator catalogs, pinned instance cohort or #317 implementation was inspected. Repository evidence suffices for this removal inventory, not deployment readiness.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
