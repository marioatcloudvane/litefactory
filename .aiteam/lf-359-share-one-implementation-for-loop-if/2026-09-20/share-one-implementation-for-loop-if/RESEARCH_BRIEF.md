# Research brief

## Request

Assess how to make graph workflows generic across modes without copying activity implementations, with Plan added primarily through agent Markdown files and a DOT graph plus minor mode-specific code. Disregard the issue's proposed fix and everything following it as a prescribed solution.

## Decisions

- **d1** — The owner requires shared activity implementations across modes and wants Plan authoring to be primarily Markdown roles and DOT topology. This is an assessment, not authorization to implement a particular design.
- **d2** — The issue's proposed fix and subsequent discussion are not accepted design constraints. Action naming and the shared interface remain unsettled for shaping.


## Assumptions

- **a1** — Interpret 'minor code additions' as the ongoing cost of adding Plan after a one-time generic-runtime refactor. Current Research-specific boundaries make a files-only Plan addition infeasible today; the owner supplied no numeric size or effort limit.
- **a2** — Assume application-owned workflow packages and existing permission ceilings remain the baseline. Repository-selected arbitrary workflows, executable DOT expressions and dynamic code plugins are not implied by the request.
- **a3** — Preserve existing managed Plan validation, generation identity, publication recovery and readiness semantics. The exact Plan stage payload and owner-review semantics remain open for shaping; the related issues were not used as authoritative implementation specifications.
