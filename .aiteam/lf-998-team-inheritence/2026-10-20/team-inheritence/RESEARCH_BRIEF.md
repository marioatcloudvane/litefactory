# Research brief

## Request

Add inheritance so a team (e.g. python) can inherit from a main team, so generic agents are edited in one place.

## Decisions

- **d1** — The base is a new generic team in the team folder, marked abstract so it is never installable. (owner, q1)
- **d2** — A child may remove an inherited file through an explicit exclude list. (owner, q2)
- **d3** — Workflows (graphs), roles and skills are all inherited. (owner)


## Assumptions

- **a1** — Build-time resolution keeps releases self-contained and immutable; the installer takes finished packages, so no loader change is needed.
- **a2** — Whole-file override handles real stack logic; merge is not needed.
- **a3** — The base is pinned by living in the same repository; no version pinning.
- **a4** — Child sizes (about 18 python, 10 go) are estimates from the python comparison; swift and dotnet were not compared in detail and swift may need more overrides.


## Open questions

### q3 — May the base use simple {{variables}} so child teams stay tiny?

_Asked of: Product Owner Questions_

- **a** — Yes. The child declares a few variables (team name, stack skill, example command) and the build fills them in. Children stay at about 18 and 10 files. Recommended.  *(recommended)*
- **b** — No. Whole-file override only. Every workflow graph and about 20 roles that differ by one line must be copied into each child, so python stays near 45 files and inheritance saves little.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
