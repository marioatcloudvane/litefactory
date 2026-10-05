# Research brief

## Request

Inventory retirement of old team CLI, installation and distribution, quantify database/code removal, and list removal of internal/teamsource tests while preserving internal/teamsource production definitions and forwarding until #317.

## Decisions

- **d1** — Owner requests retirement of old CLI and installation functionality now; this bounded pass researches it and does not implement or create child issues.
- **d2** — Preserve internal/teamsource production assets and agent forwarding; owner explicitly requests a way forward removing tests in that package.
- **d3** — Remove all seven team commands, repository selection input, catalog runtime consumers and distribution-only publishing surfaces.
- **d4** — Preserve all team-source production files and retain dependencies required by those files or built-in delivery.
- **d5** — Remove the 27 team-source test files while preserving unique behavioral protections at consumer boundaries.
- **d6** — Keep zero table removals and retain the three historical repository columns as inert compatibility data.
- **d7** — Preserve captured pinned definitions and refuse recovery when required captured inputs are missing.
- **d8** — Remove catalog requirements from registration without weakening its unrelated repository checks.
- **d9** — Do not invent definitions for unsupported modes or automatically change a selected mode.
- **d10** — Leave operator catalog files, caches, overrides and credential material untouched.
- **d11** — Treat legacy team source configuration as inactive compatibility input without acquisition or credential resolution.


## Assumptions

- **a1** — Treat the direct owner instruction as current intent over the older rollout description. Keep runtime permission/result/provenance contracts and graph storage.
- **a2** — Preserve every internal/teamsource production file as the explicit owner boundary; installation helpers and their required types may remain dormant until a later authorized cleanup.
- **a3** — No live daemon state, installed operator catalogs, pinned instance cohort or #317 implementation was inspected. Repository evidence suffices for this removal inventory, not deployment readiness.
- **a4** — Retaining inactive database columns and accepting inactive legacy configuration favors non-destructive upgrades; neither restores retired installation functionality.


## Open questions

### q1 — How should new executions handle repositories with an existing external team selection?

_Asked of: Product Owner Questions_

- **explicit** — Recommended: refuse new execution with a clear retirement message until the owner explicitly re-registers the repository to use built-in definitions; intact pinned executions keep their captured definitions.  *(recommended)*
- **automatic** — Automatically use built-in definitions for new execution and report that custom catalog selections and overrides no longer apply; this may change the roles and permissions previously selected.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
