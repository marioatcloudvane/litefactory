# Research brief

## Request

Analyze #1268 and visualize the candidate reusable graph/agent library with a class diagram, repository changes and test impact.

## Decisions

- **S-01** — mode=refactor, size=medium — mixed graph and provider boundaries require a bounded cross-package analysis, without changing behavior. (agent's call)
- **A-01** — Owner direction — owner: "analyze this. visualize in a class diagram how the new library will look like. also show how the repo structure will change and what impact on tests this will have" (owner said: comment 6548677290)
- **O-01** — Extract reusable graph mechanics and neutral provider execution; integrate factory and demonstrate a non-factory consumer without store, ticket or approval/publication dependencies. (owner said: issue #1268 supplied scope)
- **O-02** — Retire direct parser/navigation/provider paths replaced by the extraction; keep each intermediate deployment usable. (owner said: issue #1268 supplied scope)
- **K-01** — Preserve graph/runner observable behavior, owner approvals, permissions, cancellation, bounded execution, retained ticket/code links, result receipts, delivery recovery and worktree isolation. (owner said: issue #1268 supplied scope)
- **K-02** — Keep immutable instruction/team captures at existing ownership boundary and retain compatibility/recovery for any state migration. (owner said: issue #1268 supplied scope)
- **N-01** — No standalone engine, second engine, new providers, remote authority, public release promise, team release changes or full daemon decomposition. (owner said: issue #1268 supplied scope)


## Assumptions

- **AS-01** — Use one repository and Go module with coordinated release, as the issue's drafting assumption states; no separate processes or independently supported versions. (agent's call)
- **AS-02** — The supplied diagram and pkg/execution names are advisory candidate structure; shaping owns final package/API choices. An importable package path allows demonstration outside internal without a support promise. (agent's call)
- **AS-03** — No persistence migration is expected for a package-boundary extraction. Preserve stored digests and IDs; any discovered migration requires explicit compatibility fixtures and recovery design. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [LIBRARY&#95;BOUNDARY&#46;md](LIBRARY_BOUNDARY.md)
