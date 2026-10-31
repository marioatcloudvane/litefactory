# Research brief

## Request

Establish current foundations and sketch an extensible remote MCP interface for starting background workflows, discovering supported workflows, retrieving results and continuing standards authoring, per #1111 and owner comment 6542301480.

## Decisions

- **S-01** — mode=feature, size=medium — extensible workflow discovery and shared job views cross existing connector, admission and domain execution boundaries. (agent's call)
- **O-01** — Sketch a flexible MCP entry point, example tool calls and how agents discover workflows, accommodating future custom research workflows. (owner said: comment 6542301480)
- **O-02** — Long agent runs are background jobs; provide plain results, confirmed filed-issue links and proposed standards, support standards author answers, and retain Discord completion notices. (owner said: issue #1111)
- **K-01** — Preserve shared proposal/confirmation authority, scope checks, exact targets, impact tiers and uncertain-effect recovery; discovery does not authorize execution. (agent's call)
- **N-01** — This research pass provides evidence and an advisory sketch; implementation and final interface/scope choices belong to later shaping. (agent's call)


## Assumptions

- **AS-01** — Remote owner MCP is the intended client surface, rather than the run-scoped worker MCP, based on parent #1106. (agent's call)
- **AS-02** — Future custom research means an explicitly registered, permission-classified workflow adapter; it does not imply arbitrary model-supplied commands or unrestricted graph execution. (agent's call)
- **AS-03** — The generic catalogue and job API in WORKFLOW_INTERFACE_SKETCH.md is an advisory option for shaping, not a settled design. Existing generic action tools should remain compatible. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [WORKFLOW&#95;INTERFACE&#95;SKETCH&#46;md](WORKFLOW_INTERFACE_SKETCH.md)
