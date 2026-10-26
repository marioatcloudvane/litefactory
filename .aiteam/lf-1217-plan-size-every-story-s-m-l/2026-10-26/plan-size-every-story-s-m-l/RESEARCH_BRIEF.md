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
- **O-04** — The plan-set agent assigns every proposed story its size and reason using Research S-01 as context, and later Plan agents carry that assessment forward rather than routinely sizing the story again (owner said: A-01).
- **O-05** — Represent size as exactly S, M or L with a nonblank single-line complexity reason in authored story content, saved state and final typed handoff, preserving both through promotion, selective revisions, owner edits and assembly (agent's call).
- **O-06** — Show size and reason beside each story in set review, individual story review and assembled final review, preserving the set assessment unless an explicit owner-reviewed correction changes it (agent's call).
- **O-07** — Use shared Plan guidance to classify S as bounded known work with low risk, M as coordinated work with understood interfaces, and L as substantial coupling, risk, unknowns or migration, explaining the decisive factors rather than counting files or estimating time (agent's call).
- **O-08** — Scale planning by judgment: S uses the fewest sufficient tasks and concise criteria, M uses normal detail, and L explicitly covers relevant interfaces, risks, unknown resolution, migration and failure behavior without mandatory filler or fixed task counts (agent's call).
- **O-09** — Replace the blanket three-to-five-criteria instruction with sufficient observable coverage for the story's size while retaining all applicable architecture, criterion proof methods and task coverage (agent's call).
- **O-10** — An explicit sizing correction uses the existing exact-candidate revision and affected-downstream approval rules without rewriting a promoted set or introducing a separate resizing step (agent's call).
- **O-11** — Require S/M/L and a nonblank single-line reason for every entry in newly authored story sets and preserve them in their downstream candidates, rejecting malformed sizing without a default (agent's call).
- **O-12** — Demonstrate success with set-to-story-to-final preservation, edit and restart round trips, explicit correction revision behavior, invalid-input rejection, continued use of legacy unsized plans and contrasting S/M/L planning examples (agent's call).
- **N-02** — Do not add a sizing stage, new command, estimation service, numeric scoring system, time or token budget, automatic story splitting or automatic size-driven routing (agent's call).
- **N-03** — Do not rewrite promoted story sets, immutable captures or historical reports, cut a team release, or change unrelated agent roles (agent's call).
- **K-02** — Keep current permissions, factory ownership of publication, stage order, story identities, dependencies and retirement rules unchanged (agent's call).
- **O-13** — Place sizing judgment and examples in plan-story-set with a shared Plan evidence pointer, make downstream skills consume the size for depth, and enforce the size-and-reason contract in factory validation without testing exact agent wording (agent's call).
- **A-01** — q1 compatibility explanation and sizing ownership — owner: "what means \"when next changed\"? and what is this about? a sizing should happen in the plan-set agent i would say ... " (owner said: comment 6427530493).
- **N-04** — Do not backfill or reopen existing unsized plans solely to add sizes, or require them to visit plan-set before continuing through their existing workflow (agent's call).
- **K-03** — Previously saved unsized plans remain loadable and usable with their existing approvals, and their missing size remains explicit rather than being fabricated as S, M or L (agent's call).


## Assumptions

- **a1** — Use current team 1.0.0 and canonical base-team sources; do not inherit old #957 stage topology or hard architecture-section ceilings. The owner explicitly restarted both replacement issues (agent's call).
- **a2** — The owner's plan-set direction replaces the earlier proposal for routine story-stage refinement; later roles consume the stored assessment and raise a mismatch for an explicit correction rather than silently changing it (agent's call, based on A-01).
- **a3** — Size measures complexity rather than time or tokens; good: 'S — one known behavior, existing tests, no migration'; bad: 'S — two hours'; M and L reasons likewise name actual coupling, risk, unknowns or migration instead of restating the label (agent's call).
- **a4** — The owner did not choose either legacy-backfill option in q1, so the compatibility question is withdrawn and the narrow future-set-only boundary is proposed in N-04 and K-03 for this scope review (agent's call).
- **a5** — For an old workflow that begins with existing stories and skips set creation, retain the existing unsized planning behavior; absence of Research S-01 in a new set is recorded as an assumption and sizing still uses available scope evidence (agent's call).

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
