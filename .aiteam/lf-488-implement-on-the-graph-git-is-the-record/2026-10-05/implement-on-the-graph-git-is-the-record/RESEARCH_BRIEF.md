# Research brief

## Request

Analyze moving Implement onto the graph (git is the record). Per the owner's comment: the full test suite belongs to Validate; Implement covers tailored unit tests, written and run by agents, not by the factory.

## Decisions

- **d1** — Story workers write and run tailored unit tests (race tests on affected packages). The factory runs no tests. The full suite and acceptance review belong to Validate.
- **d2** — Each story runs as its own child workflow on its own branch; only done stories merge into the feature branch in plan order with a marker.
- **d3** — incomplete and blocked park the story with the agent's reason; a reply resumes it. Nothing after exit fails an accepted result.


## Assumptions

- **a1** — Serial first, parallel later with a cap of four. Pending q1.
- **a2** — Implement does not depend on #321 and can ship in either order.
- **a3** — A done story with no code changes is accepted and noted in the handoff.


## Open questions

### q1 — Ship serial story execution first, parallel later?

_Asked of: Product Owner Questions_

- **a** — Serial first. Parallel workers (cap four) as a follow-up issue. Smallest first pass, easiest to prove.  *(recommended)*
- **b** — Serial and parallel together in this issue. Faster runs, bigger and riskier change.

### q2 — If some stories are parked, does the run still hand off to Validate?

_Asked of: Product Owner Questions_

- **a** — No. The run stops with the batched question. Validate starts only once all stories are done, or the owner says to proceed with the rest.  *(recommended)*
- **b** — Yes. Hand off the done stories to Validate automatically and report the parked ones.

### q3 — What happens when a done story fails to merge or build?

_Asked of: Technical Questions_

- **a** — The story parks with the conflict or build error as its reason. The feature branch stays untouched. A reply resumes the story and it rebases.  *(recommended)*
- **b** — The story worker is re-run automatically to resolve it, within the visit limit, then parks.

### q4 — How is the graph path switched on for Implement?

_Asked of: Technical Questions_

- **a** — Graph path handles every new Implement run. The old path stays in the code, unused, until #323 removes it.  *(recommended)*
- **b** — Keep both behind a setting and default to the old path until the graph path is proven.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
