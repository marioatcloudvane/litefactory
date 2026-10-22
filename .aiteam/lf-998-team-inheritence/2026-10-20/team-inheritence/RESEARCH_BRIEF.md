# Research brief

## Request

Add inheritance so a team (e.g. python) can inherit from a main team, so generic agents are edited in one place.

## Decisions

- **d1** — The base is a new generic team in the team folder, marked abstract so it is never installable. (owner, q1)
- **d2** — A child may remove an inherited file through an explicit exclude list. (owner, q2)
- **d3** — Workflows (graphs), roles and skills are all inherited. (owner)
- **d4** — Simple plain-text variables are allowed (team, stack skill, example command); no conditions or nesting. (owner, q3)
- **d5** — After inheritance the go team keeps about 11 files: release metadata, stack profile, 6 rules skill files, 1 plan guidance skill, and 2 whole-file role overrides.


## Assumptions

- **a1** — Build-time resolution keeps releases self-contained and immutable; the installer takes finished packages, so no loader change is needed.
- **a2** — Whole-file override handles real stack logic; merge is not needed.
- **a3** — The base is pinned by living in the same repository; no version pinning.
- **a4** — Child sizes (about 18 python, 10 go) are estimates from the python comparison; swift and dotnet were not compared in detail and swift may need more overrides.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
