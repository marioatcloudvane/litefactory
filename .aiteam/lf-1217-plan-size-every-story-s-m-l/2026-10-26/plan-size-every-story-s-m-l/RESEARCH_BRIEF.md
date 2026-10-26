# Research brief

## Request

Establish current Plan behavior and impact for #1217: every story receives S/M/L plus a one-line complexity reason, refining Research S-01; show it in the plan set and final review and use it to scale planning depth. Exclude #1218 fast track.

## Decisions

- **S-01** — mode=feature+agent-guidance, size=medium — a bounded capability spanning typed story content, review rendering, saved-state compatibility and shared Plan skills (agent's call)
- **O-01** — Every Plan story has S, M or L and a one-line reason based on touched parts, risk, unknowns and retirement or migration complexity (owner said: issue #1217, What I want).
- **O-02** — Start from Research S-01; refine sizing per story rather than duplicate the request-level assessment (owner said: issue #1217, What I want).
- **O-03** — Show each story's size in the plan set and final review; scale tasks and acceptance detail from lean S to full-depth L (owner said: issue #1217, What I want).
- **N-01** — Fast-track paths and fewer workflow stops belong to #1218 (owner said: issue #1217, Not in this story).
- **K-01** — Retain existing authority, exact-candidate approval, criterion proof links and task coverage while scaling planning detail (agent's call).
- **O-04** — The set author proposes each story's size and reason using Research S-01 as context, and the story author confirms or explicitly refines that assessment without changing the request-level size (agent's call).
- **O-05** — Represent size as exactly S, M or L with a nonblank single-line complexity reason in authored story content, saved state and final typed handoff, preserving both through promotion, selective revisions, owner edits and assembly (agent's call).
- **O-06** — Show size and reason beside each story in set review, individual story review and assembled final review, using the current story assessment after refinement and retaining the promoted set as historical context (agent's call).
- **O-07** — Use shared Plan guidance to classify S as bounded known work with low risk, M as coordinated work with understood interfaces, and L as substantial coupling, risk, unknowns or migration, explaining the decisive factors rather than counting files or estimating time (agent's call).
- **O-08** — Scale planning by judgment: S uses the fewest sufficient tasks and concise criteria, M uses normal detail, and L explicitly covers relevant interfaces, risks, unknown resolution, migration and failure behavior without mandatory filler or fixed task counts (agent's call).
- **O-09** — Replace the blanket three-to-five-criteria instruction with sufficient observable coverage for the story's size while retaining all applicable architecture, criterion proof methods and task coverage (agent's call).
- **O-10** — Treat a change to size or reason as semantic story content requiring explicit revision at the story boundary and the existing affected-downstream review rules, while unrelated edits preserve the assessment and unrelated approvals (agent's call).
- **O-11** — Require valid sizing for newly authored candidates at their existing validation boundaries, reject invalid labels or missing reasons without inventing a default, and record missing Research S-01 as an explicit assumption while sizing from available scope evidence (agent's call).
- **O-12** — Demonstrate success with set-to-story-to-final preservation, edit and restart round trips, semantic revision behavior, invalid-input rejection and contrasting S/M/L planning examples, with legacy expectations governed by the owner's compatibility answer (agent's call).
- **N-02** — Do not add a sizing stage, new command, estimation service, numeric scoring system, time or token budget, automatic story splitting or automatic size-driven routing (agent's call).
- **N-03** — Do not rewrite promoted story sets, immutable captures or historical reports, cut a team release, or change unrelated agent roles (agent's call).
- **K-02** — Keep current permissions, factory ownership of publication, stage order, story identities, dependencies and retirement rules unchanged (agent's call).
- **O-13** — Place sizing judgment in the shared Plan evidence guidance with stage-specific pointers and examples across inherited teams, and enforce the size-and-reason contract in factory validation rather than testing exact agent wording (agent's call).


## Assumptions

- **a1** — Use current team 1.0.0 and canonical base-team sources; do not inherit old #957 stage topology or hard architecture-section ceilings. The owner explicitly restarted both replacement issues (agent's call).
- **a2** — Settled by O-04, O-06 and O-10: sizing starts in set review and is refined on the story through existing review boundaries, based on the current graph and promoted-set immutability (agent's call).
- **a3** — Size measures complexity rather than time or tokens; good: 'S — one known behavior, existing tests, no migration'; bad: 'S — two hours'; M and L reasons likewise name actual coupling, risk, unknowns or migration instead of restating the label (agent's call).
- **a4** — Final typed handoff retention is settled by O-05; legacy compatibility remains open in q1, with change-on-next-edit recommended, and no deployment or migration performed (agent's call).


## Open questions

### q1 — When must existing unsized plans receive story sizes?

_Asked of: Product Owner Questions_

- **a** — When next changed (recommended): keep saved plans readable and existing approvals valid; require size and reason for each newly authored or revised story, and require all active stories to be sized before a newly assembled final review is approved; leave completed plans and historical reviews untouched.  *(recommended)*
- **b** — Before any continuation: reopen every active unsized story for sizing and renewed review before its plan can advance; this makes active plans uniform but interrupts previously approved work.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
