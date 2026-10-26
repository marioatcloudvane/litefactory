# Research brief

## Request

Take the owner's simplification: no lf_approval auto mode; a stage without an approval activity rushes, only a generic approval activity stops and waits. State what this gets and what stays open.

## Decisions

- **d12** — Proposed, not settled: replace the earlier auto-mode design. First iteration: a generic approval activity that only stops and waits; a Plan stage with no approval node records an implicit graph-given approval and rushes on. Set promotion and the final /approve-plan-set stay owner-only.


## Assumptions

- **a2** — The guards from the earlier proposal (no open question, no hold, upstream approvals current, cap on automatic re-runs, final review lists graph-given approvals) still apply to the implicit approval. Rests on keeping the safety fence of d10; not confirmed by the owner.
- **a3** — First iteration covers Plan only; Implement, Validate and Bug validators pin their node sets and were not checked for accepting an approval node.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
