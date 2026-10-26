# Research brief

## Request

Take the owner's simplification: no lf_approval auto mode; a stage without an approval activity rushes, only a generic approval activity stops and waits. State what this gets and what stays open.

## Decisions

- **d12** — Proposed, not settled: no `auto` mode. A generic approval activity only stops and waits; a Plan draft with no approval node gets a graph-given approval recorded by Go and the story advances. Set promotion and the final plan-set approval stay owner-only.


## Assumptions

- **a2** — The guards from the earlier proposal (no open question, no hold, upstream approvals current, cap on automatic re-runs, final review lists graph-given approvals) still apply to the implicit approval. Rests on keeping the safety fence of d10; not confirmed by the owner.
- **a3** — First iteration covers Plan only; Implement, Validate and Bug validators pin their node sets and were not checked for accepting an approval node.


## Open questions

### q2 — Which guards apply when Go records a graph-given approval?

_Asked of: Product Owner Questions_

- **a** — Record it only if there is no open question, no hold, and upstream approvals are current. Cap automatic re-runs per story. List graph-given stages in the final review.  *(recommended)*
- **b** — No guards. Every applied draft is approved and the story advances.

### q3 — Should the first iteration cover Plan only?

_Asked of: Product Owner Questions_

- **a** — Plan only. Other modes later, after their validators are checked.  *(recommended)*
- **b** — Plan plus Validate and Implement now. Larger, needs validator changes in each.

### q4 — How should graph-given approvals show up for you besides the final review?

_Asked of: Product Owner Questions_

- **a** — Final review only. Less noise.  *(recommended)*
- **b** — Final review plus one short comment per stage.

### q1 — Which form should the generic approval activity take?

_Asked of: Technical Questions_

- **a** — A new `approval` kind on the existing `lf_ask`. No registry change. Smallest step.  *(recommended)*
- **b** — A new `lf_approval` action. Cleaner name, but the shared action list in the registry must be extended.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
