# Research brief

## Request

Add t-shirt sizing of stories as a workflow activity; small sizes should change story creation, e.g. do the three steps (story, technical, acceptance) at once instead of sequentially.

## Decisions

- **d1** — The agent proposes the size after reasoning about the story's complexity; the owner can change it at review.
- **d2** — Sizes are S, M and L. Only S is fast-tracked. Detail scales up with size: fewer acceptance criteria and lighter technical design for S, more for M, most for L.
- **d3** — Fast-tracked stories still go through the same owner review gate.


## Assumptions

- **a1** — The 'three steps' in the issue are the story, technical and acceptance stages.
- **a2** — Sizing is a new Plan activity before or while the story set is proposed; the size is stored on the story.
- **a3** — Fast-tracked stories still end at the owner review gate.


## Open questions

### q6 — You said S only gets the 'architecture session', M one more, L all. What are those sessions?

_Asked of: Product Owner Questions_

- **a** — Read it as depth: S gets a short architecture note only; M adds data model and interfaces; L adds permissions, validation rules, edge cases and out-of-scope detail.  *(recommended)*
- **b** — It means design review sessions held with people. Tell me the list and who attends.
- **c** — Drop it. Depth scaling by criteria count and design detail is enough.

### q4 — Where does sizing happen in the flow?

_Asked of: Technical Questions_

- **a** — A new small step per story, right before its story step (informed, visible, path known up front).  *(recommended)*
- **b** — In the parent task, sizing all stories at once from titles only (cheap, but least informed).
- **c** — Inside the story step, no new step (best informed, but the path is only known mid-run).

### q5 — How do we build the S fast-track agent?

_Asked of: Technical Questions_

- **a** — Keep the three stage agents for M and L. Add one fast-track agent that loads the three shaping skills in one run.  *(recommended)*
- **b** — Replace the three agents with one agent and skills for all sizes.
- **c** — Keep the three agents and run them back to back in one run for S.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
