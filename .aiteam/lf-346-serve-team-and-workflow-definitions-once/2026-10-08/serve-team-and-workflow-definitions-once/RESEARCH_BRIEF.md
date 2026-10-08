# Research brief

## Request

Research making installed per-factory teams the only runtime source of roles, skills and workflows; retain teams/go-development/v1 as editable installation source; remove repository and embedded copies, close fallback delivery paths, preserve coverage, and keep unrelated #323 engine retirement out of scope.

## Decisions

- **d1** — Owner direction dated 2026-10-08 supersedes the September issue: retain teams/teams.json and teams/go-development/v1; there is no standard team.
- **d2** — Remove .claude, .codex, AGENTS.md, CLAUDE.md, obsolete publish source and embedded role/skill/DOT files; retain reusable capture Go code. New unlinked workflows must refuse with guidance to link a team.
- **d3** — The remainder of #323, including deleting the old engine, is excluded unless required to eliminate dependence on per-worktree definitions.


## Assumptions

- **a1** — Existing retained workflow and immutable delivery snapshots are run records, not competing editable team sources. Preserve resume from their saved bytes; current code restores them before consulting the linked source.
- **a2** — The owner's statement that litefactory is linked to go-development is accepted as operational context, not independently measured. This investigation establishes code behavior, not authentic Claude/Codex host proof.
- **a3** — No owner-only missing information prevents the bounded research handoff. Missing-delivery behavior and placement of reusable agent guidance are shaping decisions.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
