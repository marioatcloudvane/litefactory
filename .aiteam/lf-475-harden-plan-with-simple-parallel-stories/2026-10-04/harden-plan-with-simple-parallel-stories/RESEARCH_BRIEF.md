# Research brief

## Request

Make Plan faster and simpler: create the initial story drafts in parallel instead of one agent after another; run independent story stages in parallel the simple way; keep it very simple and break nothing.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — First-draft parallelism is the smallest safe first step and can ship before per-story tasks. It rests on the owner's comment and on the sequential story_pick loop in the Plan graph.
- **a2** — Parallel agents do not write shared state; the factory merges each finished draft into freshly read state one at a time, in displayed order. This rests on the shared revision-checked state found in research.
- **a3** — Only an owner reply approves a stage; fan-out changes none of the gates. This rests on the existing Plan graph, where review-ready is never approval.


## Open questions

### q1 — Ship first-draft parallelism now, and per-story tasks as a follow-up?

_Asked of: Product Owner Questions_

- **q1a** — Yes. Ship fan-out of first drafts now; per-story tasks, clean-up and progress listing come as follow-up issues. Smallest change, fastest relief, lowest risk. (recommended)  *(recommended)*
- **q1b** — No. Do everything in #475: fan-out, per-story tasks, clean-up, progress. Much bigger, closer to the #400 risk.

### q3 — If one story's draft fails, what happens to the rest?

_Asked of: Product Owner Questions_

- **q3a** — The others are kept and published. The failed story is reported on its own and the owner retries it with a reply. (recommended)  *(recommended)*
- **q3b** — The whole creation step fails and all drafts are discarded. Simpler, but wastes finished work.

### q4 — When should each story's review appear on GitHub?

_Asked of: Product Owner Questions_

- **q4a** — As soon as that story's draft is merged, so the owner can start reading while others still run. (recommended)  *(recommended)*
- **q4b** — All together once every draft is done. Tidier, but the owner waits for the slowest story.

### q2 — Should first drafts start all at once, or only for stories whose upstream already has a draft?

_Asked of: Technical Questions_

- **q2a** — In waves: every story with no undrafted upstream starts together; dependants start when their upstream is drafted. Keeps today's dependency rule, and most sets have mostly independent stories. (recommended)  *(recommended)*
- **q2b** — All at once, ignoring dependencies. Fastest, but a dependant draft cannot see its upstream draft.

### q5 — Is the state clean-up (removing mirror tables and after-exit equality checks) part of this issue?

_Asked of: Technical Questions_

- **q5a** — No. Only fix the draft-size refusal now; do the clean-up as its own issue after parallelism is proven. You asked that nothing breaks, and clean-up touches 15k lines. (recommended)  *(recommended)*
- **q5b** — Yes. Do it here. Net code shrinks, but the risk of breaking Plan rises.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
