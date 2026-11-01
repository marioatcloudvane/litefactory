# Research brief

## Request

Route the Implement story worker's model tier by story size in implement.dot: L on flagship, S/M/missing/invalid on the role's own tier (stated as balanced), with no size-to-tier policy in Go; provenance shows the tier used.

## Decisions

- **S-01** — mode=change+agent-guidance, size=medium — changes Implement graph wiring plus the factory checks and facts that allow it (agent's call).
- **K-01** — No size-to-tier policy in Go; the routing itself lives in implement.dot (owner said: "The routing belongs in the Implement graph (DOT), like Plan's fast track. No size-to-tier policy in Go.").


## Assumptions

- **a1** — Missing or invalid size takes the default (role) tier, as the issue states (agent's call).
- **a2** — Provenance already records the resolved main model per delivery (agent.Delivery.MainModel), so the acceptance check can read it (agent's call; not traced end to end).


## Open questions

### q1 — Which size routing should the story worker get, now that every role is on flagship?

_Asked of: Product Owner Questions_

- **a** — Route down: keep the role on flagship and run S/M stories on balanced through the graph. This saves cost on the bulk of the work and keeps today's quality for L.  *(recommended)*
- **b** — Route up, as the issue says: put the role back to balanced and run only L stories on flagship. Same end result as (a), but it reverts part of this morning's team decision.
- **c** — No routing: all sizes stay on flagship. The issue is then covered by #1293 and can close.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
