# Research brief

## Request

Inventory where Go code, rather than the DOT graph, decides topology (nodes, edges, resume points, order) in each mode, and establish what is true today as a map for follow-up work.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — Line numbers in the issue are approximate and may have drifted; the file and symbol references are assumed correct.


## Open questions

### q1 — How should this map be split into follow-up work?

_Asked of: Product Owner Questions_

- **a** — Four issues in the issue's order: explicit hidden transitions, DOT resume edge, Bug structural validation, then per-mode vocabularies. Recommended: each step ships alone.  *(recommended)*
- **b** — One issue per mode, covering everything for that mode.
- **c** — Do only steps 1-3 now; leave vocabularies until a real need appears.

### q4 — Should Plan stages become configurable in the DOT (#957)?

_Asked of: Product Owner Questions_

- **a** — Not in this issue. Decide separately after steps 1-3; Plan stage names are wired into drafts, approval and completion, so it is a design project of its own. Recommended.  *(recommended)*
- **b** — Yes, make Plan stage names and order DOT-driven as part of step 4.
- **c** — No, Plan stages stay fixed permanently.

### q2 — How should the DOT name the owner-reply resume point?

_Asked of: Technical Questions_

- **a** — A named edge out of the wait node that the executor follows; with no such edge, keep today's origin-node behaviour so existing graphs still work. Recommended.  *(recommended)*
- **b** — Require every graph to declare the resume edge and refuse graphs that do not.
- **c** — Keep resume in Go and only document it.

### q3 — What replaces the Bug digest pin?

_Asked of: Technical Questions_

- **a** — Drop the digest and version equality; keep the existing structural check (one loop, analyst, fixer, finalize, allowed edges). The shipped graph passes the same checks, so running Bug tasks survive a deploy. Recommended.  *(recommended)*
- **b** — Keep the digest as an allow-list and add a way to register new digests.
- **c** — Keep the pin; Bug graphs stay frozen.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
