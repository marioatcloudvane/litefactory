# Research brief

## Request

Research removal of the old team CLI, installation and publishing setup while retaining all production internal/teamsource files and dependencies, inert database columns and untouched operator disk data.

## Decisions

- **d1** — Issue #489 supplies the reviewed scope replacing #324. Preserve every production internal/teamsource file and required dependencies until #317; do not apply old roadmap gates.
- **d2** — Owner decision reported in issue #489: external selections require explicit switching through re-registration; ordinary re-registration must not clear them silently. New runs must refuse with an actionable switch step.
- **d3** — Retain the three team-related database columns without a migration; never inspect, acquire or delete operator catalogs, caches, overrides or credentials.


## Assumptions

- **a1** — Interpret 'fresh repository registers' as removal of the legacy aiteam presence prerequisite as well as the installed-catalog prerequisite. This follows the explicit fresh-registration outcome and removal of installation setup.
- **a2** — Retire both the top-level teams settings and deprecated settings.team_config with clear notices; both are currently emitted by configuration generation. Exact notice and switch syntax remain shaping decisions.
- **a3** — Repository findings describe HEAD 680c4f2ba575a18b2f704a90f8b777079470724f, inspected read-only in the supplied worktree. Counts were measured with wc; no tests or live registration/run probes were executed.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
