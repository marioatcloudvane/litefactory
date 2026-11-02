# Research brief

## Request

Establish existing options for a short path for minor changes to existing behavior, including whether extending Spike is preferable to another workflow.

## Decisions

- **S-01** — mode=change, size=medium — the existing Spike and routing capabilities overlap the request, but their contracts and delivery boundaries need comparison across workflow, agent and runtime layers. (agent's call)
- **O-01** — Establish a lower-overhead path for minor changes to existing behavior and compare extending Spike with other options before selecting an approach. (owner said: "often i just want to do a small change, not create a new feature" and "We could beef up the spike for that or do we have other options? what's best?")


## Assumptions

- **AS-01** — The request includes intentional behavior changes, documentation/configuration edits and similar tweaks, rather than only repairs of defects. The owner explicitly contrasts minor changes to existing behavior with creating a feature. (agent's call)
- **AS-02** — Recommendation for shaping: examine extending Spike first because investigation, bounded editing, measured size checks, pinned checks and one repair already exist. A distinct short-change mode becomes preferable if its approvals, delivery or eligibility must differ substantially from experiments. This is advice, not a selected design. (agent's call)
- **AS-03** — Eligibility criteria, treatment of edits with no meaningful regression test, approval timing, delivery expectations and fallback behavior remain design choices for shaping. No arbitrary new size threshold or automatic mode switch is assumed. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
