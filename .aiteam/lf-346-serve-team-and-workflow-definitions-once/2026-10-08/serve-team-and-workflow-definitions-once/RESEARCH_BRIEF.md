# Research brief

## Request

Research making installed per-factory teams the only runtime source of roles, skills and workflows; retain teams/go-development/v1 as editable installation source; remove repository and embedded copies, close fallback delivery paths, preserve coverage, and keep unrelated #323 engine retirement out of scope.

## Decisions

- **d1** — Owner direction dated 2026-10-08 supersedes the September issue: retain teams/teams.json and teams/go-development/v1; there is no standard team.
- **d2** — Remove .claude, .codex, AGENTS.md, CLAUDE.md, obsolete publish source and embedded role/skill/DOT files; retain reusable capture Go code. New unlinked workflows must refuse with guidance to link a team.
- **d3** — The remainder of #323, including deleting the old engine, is excluded unless required to eliminate dependence on per-worktree definitions.
- **d4** — Every fresh workflow admission must select a linked installed package and fail clearly for absent binding, invalid content or an unavailable workflow.
- **d5** — Retained workflow captures remain authoritative for resume, including older builtin provenance, and damaged records must fail without recapture.
- **d6** — The existing repository binding, installed release, retained capture/provenance and immutable delivery records are sufficient; no new configuration surface is proposed.
- **d7** — Reusable agent rules move into delivered Go team resources, with litefactory-specific repository rules clearly scoped and obsolete dispatcher guidance removed.
- **d8** — Bug validation must enforce factory-owned semantic safety invariants without pinning editable DOT bytes to an embedded digest.
- **d9** — Both hosts must launch factory agents only with verified delivered instructions and retain existing permission ceilings.
- **d10** — Test-owned fixtures replace deleted embedded fixtures while shipped-release contracts, catalogue integrity, delivery isolation and saved-capture coverage remain.
- **d11** — Branch rewrites, live factory installation changes and the remaining engine retirement are excluded from this scope.


## Assumptions

- **a1** — Existing retained workflow and immutable delivery snapshots are run records, not competing editable team sources. Preserve resume from their saved bytes; current code restores them before consulting the linked source.
- **a2** — The owner's statement that litefactory is linked to go-development is accepted as operational context, not independently measured. This investigation establishes code behavior, not authentic Claude/Codex host proof.
- **a3** — Guidance placement and replacement of the embedded Bug digest check can be settled from repository context; the user-visible loss of instruction-free single-run support requires the owner's choice.


## Open questions

### q1 — How should surviving single-run requests without an instruction bundle behave?

_Asked of: Product Owner Questions_

- **refuse** — Recommended: refuse before agent launch with guidance to the supported workflow route; accept loss of this legacy launch capability and keep its migration in #323.  *(recommended)*
- **deliver** — Preserve single-run support by capturing the linked team's appropriate role and skills and delivering a verified isolated bundle; include that additional delivery work here without deleting the old engine.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
