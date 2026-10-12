# Research brief

## Request

Research an additional lf_sync_base graph activity that can be inserted and looped without replacing existing synchronization logic.

## Decisions

- **d1** — The owner's direct instruction preserves current logic and makes this additive. It supersedes the issue body's proposed removal of hidden synchronization. No migration away from prepare or Bug preparation is assumed.


## Assumptions

- **a1** — Reuse the existing generic activity and outcome vocabulary rather than create a second sync mechanism; current implementation and graph routes support this reading.
- **a2** — Exact opt-in graph placements, policy interactions and release rollout belong to shaping. Research does not select them.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
