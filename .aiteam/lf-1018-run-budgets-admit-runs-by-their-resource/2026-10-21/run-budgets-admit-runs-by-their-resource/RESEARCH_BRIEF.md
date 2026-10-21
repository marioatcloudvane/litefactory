# Research brief

## Request

Suggest a dynamic DOT location for workflow slot costs; keep max_concurrent_runs as capacity, default cost 1, and allow Implement work to cost 3 of 8 slots without hardcoded mode or team costs.

## Decisions

- **d1** — Owner retains max_concurrent_runs, requires an omitted cost to mean 1, and requires costs to be assignable in workflow DOT rather than hardcoded in Go.
- **d2** — This pass establishes facts and an advisory configuration recommendation; it does not implement or approve a design.


## Assumptions

- **a1** — Dynamic means declarative assignment to arbitrary workflow steps. Hot reload and changing already admitted snapshots are not assumed; existing capture uses embedded sources.
- **a2** — The stated step requirement makes a node attribute the leading recommendation. Proposed precedence is node lf_slot_budget, graph lf_slot_budget, then 1. This is advisory syntax, not an existing supported setting.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
