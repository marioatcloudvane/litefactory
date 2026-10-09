# Research brief

## Request

Overview of blockers to adding non-Go languages, and first teams: python-fastapi-stack, .NET C# web, Swift iOS.

## Decisions

- **D-1** — Scope is: multi-team release tooling, stack-neutral roles, stack profile wiring, wider Plan guidance, Spike manifests, three new teams. Go team unchanged.


## Assumptions

- **A-1** — Stack settings travel with the team (or a profile it selects), consistent with #317.
- **A-2** — Tool stack suggestions come from model knowledge (cutoff June 2026), not a web check.
- **A-4** — Shipped Go release stays as-is (carried from the earlier pass; not contradicted).


## Open questions

### Q-4 — Which frontend does the .NET web team use?

_Asked of: Product Owner Questions_

- **a** — Blazor, all C#, one language and one toolchain. (Recommended for a C# team.)  *(recommended)*
- **b** — React + TypeScript + Vite behind an ASP.NET Core API. Bigger ecosystem, two toolchains.

### Q-5 — Which frontend does the Python FastAPI team use?

_Asked of: Product Owner Questions_

- **a** — React + TypeScript + Vite with TanStack Query, Vitest, Playwright. (Recommended: widest ecosystem.)  *(recommended)*
- **b** — SvelteKit. Lighter, smaller ecosystem.
- **c** — Vue 3 + Vite.

### Q-6 — Is the Swift iOS team part of this issue's done-when, given it needs macOS and Xcode?

_Asked of: Product Owner Questions_

- **a** — Ship the Swift team as a validated release now; prove a live run later on a macOS host. Python and .NET carry the full-cycle proof. (Recommended.)  *(recommended)*
- **b** — Require a full Plan to merge cycle on Swift too before closing.
- **c** — Defer the Swift team to a follow-up issue.

### Q-1 — Do stack settings live in the team, or in a separate repository profile?

_Asked of: Technical Questions_

- **a** — Team carries a default stack profile; the repository can override single fields. (Recommended: one place to start, still flexible.)  *(recommended)*
- **b** — Separate profile file only, teams hold no stack settings. More moving parts.
- **c** — Team only, no override. Simplest, but a repo with a different test command is stuck.

### Q-2 — How do the stack-neutral roles get their stack-specific content?

_Asked of: Technical Questions_

- **a** — One shared neutral role set; each team adds its own stack guidance and profile commands. (Recommended: no copies to keep in sync.)  *(recommended)*
- **b** — Copy and edit the full role set per team. Fast to start, drifts apart.

### Q-3 — How does a repository choose its team in this issue, given #317 is not done?

_Asked of: Technical Questions_

- **a** — One field in the repository's litefactory config naming the team slug; default stays go-development. (Recommended.)  *(recommended)*
- **b** — Wait for #317 and ship teams only as releases.
- **c** — Pick the team at registration time only.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
