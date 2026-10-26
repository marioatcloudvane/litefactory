# Research brief

## Request

Take the owner's simplification: no lf_approval auto mode; a stage without an approval activity rushes, only a generic approval activity stops and waits. State what this gets and what stays open.

## Decisions

- **d12** — Settled with the owner: a new generic `lf_gate` action only stops and waits for an owner command named in the DOT (`lf_command`, `lf_args`). A Plan stage with no gate gets a guarded graph-given approval recorded by Go. The set stage keeps its gate. The final plan-set approval stays owner-only.
- **d13** — Two light guards for a graph-given approval: the draft applied cleanly, and the story's earlier stages are still approved. Failing either falls back to today's owner stop. No new re-run cap; existing loop limits apply.
- **d14** — Settled with the owner: no shipped graph changes behaviour in this step. Shipped Plan graphs adopt `lf_gate` in place of the check-and-review pair with identical behaviour; a rush graph is not shipped now.


## Assumptions

- **a2** — The audit trail is the final review only, with no per-stage comments. Rests on the owner's wish for few and light stops; the owner did not answer that choice directly.
- **a3** — First iteration covers Plan only; Implement, Validate and Bug validators pin their node sets and were not checked for accepting an approval node.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
