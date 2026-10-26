# Research brief

## Request

Take the owner's simplification: no lf_approval auto mode; a stage without an approval activity rushes, only a generic approval activity stops and waits. State what this gets and what stays open.

## Decisions

- **d12** — Proposed, not settled: no `auto` mode. A generic `lf_gate` only stops and waits for an owner command named in the DOT (`lf_command`, `lf_args`); a Plan stage with no gate gets a guarded graph-given approval recorded by Go. The set stage keeps its gate; the final plan-set approval stays owner-only.


## Assumptions

- **a2** — The guards from the earlier proposal (no open question, no hold, upstream approvals current, cap on automatic re-runs, final review lists graph-given approvals) still apply to the implicit approval. Rests on keeping the safety fence of d10; not confirmed by the owner.
- **a3** — First iteration covers Plan only; Implement, Validate and Bug validators pin their node sets and were not checked for accepting an approval node.


## Open questions

### q2 — When a stage has no gate, may Go stop for you anyway if something looks wrong?

_Asked of: Product Owner Questions_

- **a** — Yes. Go approves only when there is no open question, no hold and earlier stages are still approved. Otherwise it stops for you. Re-runs per story are capped. The final review lists graph-given stages.  *(recommended)*
- **b** — No. Every applied draft is approved and the story advances, whatever the state.

### q3 — Is this the right shape for `lf_gate` in Plan?

_Asked of: Product Owner Questions_

- **a** — Yes: the gate carries the command and parameters in the DOT; stories, technical and acceptance may omit it; the set stage must keep it; Plan only for now.  *(recommended)*
- **b** — Same, but any stage including set may omit it. Go would then promote the story set without you.
- **c** — Same, and wire Validate and Implement now. Larger; their validators pin node sets.

### q4 — Besides the final review, how should graph-given approvals show up?

_Asked of: Product Owner Questions_

- **a** — Final review only. Less noise.  *(recommended)*
- **b** — Final review plus one short comment per stage.

### q1 — Should the stop be a new `lf_gate` action or a kind on `lf_ask`?

_Asked of: Technical Questions_

- **a** — A new `lf_gate` action. Keeps 'ask for information' and 'ask for permission' apart. Costs one name in the shared action list.  *(recommended)*
- **b** — A new `approval` kind on `lf_ask`. No registry change, but mixes two different ideas.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
