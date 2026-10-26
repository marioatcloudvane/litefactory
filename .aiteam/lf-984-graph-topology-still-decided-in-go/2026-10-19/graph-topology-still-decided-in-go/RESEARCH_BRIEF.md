# Research brief

## Request

Inventory where Go decides topology and where hard states block orchestration, then settle a light-guardrails scope for follow-up work.

## Decisions

- **d1** — Split into B1 retry and lifetimes, B2 narrowed heal, B3 no silent stops, B4 doctor repair on the existing flow, and A1-A4 for topology.
- **d2** — Owner-reply resume is a named DOT edge; without one, the origin-node behaviour remains the fallback.
- **d3** — Bug digest pin is replaced by the existing structural check.
- **d4** — Plan stage configurability (#957) is out of this issue.
- **d5** — Track B ships before Track A.
- **d6** — B2 is limited to the story comment sweep after hand repair, 'dispatch no longer matches' and 'instance already terminal'. Identity checks that guard actor or ticket ownership stay hard stops.
- **d7** — B4 extends the existing doctor repair flow (holds, drain, approval, journal) and does not rebuild it. Doctor repair holds join the lifetime list in B1.
- **d8** — Light guardrails C (#1050) and E (#1052) stay with their own issues and are out of this scope.
- **d9** — Proposed, not settled: add A5 graph-controlled workflows (own stages, gates and continuation in the DOT) after A1-A3. This is a design decision for the owner.
- **d10** — Proposed, not settled: Go keeps the safety fence (permission ceiling, owner identity, ticket ownership checks, idempotent journaled effects, cost and time ceilings, redaction, audit). Order, stage names, section rights, gate policy, retries, lifetimes and prompts move to the DOT, in four levels with a recommended stop after level 3.
- **d11** — Proposed, not settled: 12 top-level stories, 9 now (DOT-owned limits and pinning; heal and no silent stops; doctor repair; transient and lifetimes; visible transitions; Bug graphs can change; graph safety tooling; Plan stages and facts; proof graphs) and 3 held (generic effect blocks; one structural validator; workflow registry) until the proof graphs show how Plan behaves. About 30 child stories. Cut by capability, not by mode.
- **d12** — Proposed, not settled: first iteration is one parent with three children. A dedicated lf_plan_approval activity (owner and auto modes) replaces the gate-and-review pair; auto approvals are guarded, recorded and listed in the final review; the set stage and the final /approve-plan-set stay owner-only. Replaces the first slice of the 12-story set.


## Assumptions

- **a1** — Line numbers in the issue are approximate and may have drifted; the file and symbol references are assumed correct.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
