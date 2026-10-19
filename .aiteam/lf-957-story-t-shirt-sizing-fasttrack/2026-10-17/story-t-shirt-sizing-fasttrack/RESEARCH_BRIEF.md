# Research brief

## Request

Add t-shirt sizing of stories as a workflow activity; small sizes should change story creation, e.g. do the three steps (story, technical, acceptance) at once instead of sequentially.

## Decisions

- **d1** — The agent proposes the size after reasoning about the story's complexity; the owner can change it at review.
- **d2** — Sizes are S, M and L. Only S is fast-tracked.
- **d3** — Fast-tracked stories still go through the same owner review gate.
- **d4** — Sizing is a new step per story, right before its story step.
- **d5** — Keep the stage agents for M and L; add one fast-track agent that loads the story, technical and acceptance shaping skills in one run.
- **d6** — Size caps technical depth: S writes Architecture - General only, plus one more part only if truly necessary with a one-line reason; M adds at most two of Data Model, Service Design and Solution Design where needed; L adds all that apply. Acceptance criteria scale likewise.
- **d7** — The workflow gets a sizing block, a decision on size, and a fast-track block.


## Assumptions

- **a1** — The 'three steps' in the issue are the story, technical and acceptance stages.
- **a2** — The size is stored on the story and survives re-runs.
- **a3** — A single story request can carry story, architecture and acceptance content in one run; to be confirmed when building.
- **a4** — The owner's 'architecture session' means the four design parts of the technical stage: general, data model, service design, solution design.


## Open questions

### q7 — Should the stage names stay in code, or should the graph own them?

_Asked of: Technical Questions_

- **a** — Keep the code list for now. Add the two new names (size, fast-track) and teach the stage picker the order. Smallest change; the coupling stays and moves to a separate follow-up.
- **b** — Make the graph own it as part of this work. The graph declares its stages and their order, and the code only follows the graph. Cleaner, but a bigger change that touches every Plan stage.
- **c** — Do a first, then open a separate follow-up issue for b, so sizing ships without a refactor.  *(recommended)*

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
