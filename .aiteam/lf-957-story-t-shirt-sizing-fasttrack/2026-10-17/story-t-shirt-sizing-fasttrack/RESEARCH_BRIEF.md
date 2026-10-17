# Research brief

## Request

Add t-shirt sizing of stories as a workflow activity; small sizes should change story creation, e.g. do the three steps (story, technical, acceptance) at once instead of sequentially.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — The 'three steps' in the issue are the story, technical and acceptance stages.
- **a2** — Sizing is a new Plan activity before or while the story set is proposed; the size is stored on the story.
- **a3** — Fast-tracked stories still end at the owner review gate.


## Open questions

### q1 — Who sets the size of a story?

_Asked of: Product Owner Questions_

- **a** — The agent proposes the size; the owner can change it at review.  *(recommended)*
- **b** — The agent sets it alone, no override.
- **c** — The owner always sets it.

### q2 — Which sizes exist, and which ones fast-track?

_Asked of: Product Owner Questions_

- **a** — XS, S, M, L, XL. Only XS and S fast-track.  *(recommended)*
- **b** — S, M, L. Only S fast-tracks.
- **c** — XS, S, M, L, XL. Only XS fast-tracks.

### q3 — Does the owner still review fast-tracked stories?

_Asked of: Product Owner Questions_

- **a** — Yes. Same review gate as every story; only the writing is faster.  *(recommended)*
- **b** — No. Fast-tracked stories are approved automatically.

### q4 — Where does sizing happen in the flow?

_Asked of: Technical Questions_

- **a** — A new step per story, right before its story step.  *(recommended)*
- **b** — Inside the parent task, when the story set is proposed.
- **c** — Merged into the story step, no new step.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
