# Research brief

## Request

Analyze moving Implement onto the graph (git is the record). Per the owner's comment: the full test suite belongs to Validate; Implement covers tailored unit tests, written and run by agents, not by the factory.

## Decisions

- **d1** — Story workers write and run tailored unit tests (race tests on affected packages). The factory runs no tests. The full suite and acceptance review belong to Validate.
- **d2** — Each story runs as its own child workflow on its own branch; only done stories merge into the feature branch, one at a time, with a marker.
- **d3** — incomplete and blocked park the story with the agent's reason; a reply resumes it. Nothing after exit fails an accepted result.
- **d4** — Parallel workers are in scope in this issue, per the owner's reply.


## Assumptions

- **a1** — Parallel cap is four, bounded also by the global run limit.
- **a2** — Implement does not depend on #321 and can ship in either order.
- **a3** — A done story with no code changes is accepted and noted in the handoff.
- **a4** — Parked stories stop the automatic hand-off to Validate until the owner replies.
- **a5** — A merge or build failure parks the story with the error as its reason; the feature branch is untouched and a reply resumes it.
- **a6** — The graph path handles every new Implement run; the old path stays unused until #323.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
