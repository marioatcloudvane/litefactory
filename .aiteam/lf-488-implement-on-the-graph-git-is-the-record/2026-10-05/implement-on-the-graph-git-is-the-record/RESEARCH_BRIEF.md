# Research brief

## Request

Analyze moving Implement onto the graph (git is the record). Per the owner's comment: the full test suite belongs to Validate; Implement covers tailored unit tests, written and run by agents, not by the factory.

## Decisions

- **d1** — Implement tests are tailored unit tests (race tests on affected packages) written and run by the story worker agent. The factory does not run or verify tests. The full suite and acceptance review belong to Validate.


## Assumptions

- **a1** — Serial story execution ships first; parallel workers and a default cap of four follow later. This rests on the issue's proposal and keeps the first pass small.
- **a2** — Order against the Validate branch-diff change (#321) is left to shaping; Implement does not depend on it because it drops acceptance work.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
