# Research brief

## Request

Overview of blockers to adding non-Go languages, and first teams: python-fastapi-stack, .NET C# web, Swift iOS.

## Decisions

- **D-1** — Scope is: multi-team release tooling, stack-neutral roles, stack profile wiring, wider Plan guidance, Spike manifests, three new teams. Go team unchanged.
- **D-2** — Owner: stack profile lives in the team with per-field repository override.
- **D-3** — Owner: one shared neutral role set; each team adds its own stack guidance.
- **D-4** — Owner: team selection per repository already exists (#317); new teams are linked in, no new setting.
- **D-5** — Owner: .NET team uses Blazor; Python team uses React + TypeScript + Vite.
- **D-6** — Owner: Swift team ships as a release now; live run later on macOS. No automated per-team tests; Python and .NET proof is a manual live run.


## Assumptions

- **A-1** — Stack settings travel with the team, consistent with #317.
- **A-2** — Tool stack suggestions come from model knowledge (cutoff June 2026), not a web check.
- **A-4** — Shipped Go release stays as-is.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
