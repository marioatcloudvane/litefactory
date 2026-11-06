# Research brief

## Request

Explain the impact of the recent extraction stories (#1251 and its sub-stories #1268, #1269, #1270, #1271), say what was won, and give a diagram of the new application architecture.

## Decisions

- **S-01** — mode=refactor, size=small — the request is a read-only report on a finished refactoring plus diagrams; no code change is asked for, so the work is smaller than first sized. (agent's call)
- **O-01** — The final answer states what changed, what was won and what it cost, using the measured line counts in f7 and the boundary evidence in f2-f6. (agent's call, from the issue request)
- **O-02** — The final answer carries the current source-level architecture diagram and a small before diagram based on commit 436cb605. (agent's call, from the issue request)
- **O-03** — The final answer separates proven from unproven: boundary tests pass at c05ebd29; live daemon upgrade and runtime performance are not proven. (agent's call)
- **O-04** — Pending q2: the final answer includes a short ranked list of recommended next extraction steps, advice only. (agent's call, awaiting owner)
- **N-01** — No code changes and no further extraction work are in scope for this request. (agent's call)
- **N-02** — No runtime, speed or load measurements. (agent's call)
- **N-03** — No deployment diagram, because the app still runs as one process. (agent's call)
- **N-04** — No live upgrade or recovery check on the deployed daemon; that stays the owner's check. (agent's call)
- **K-01** — No source, schema, configuration or test change; at most one new docs page if the owner picks q1 option b. (agent's call)


## Assumptions

- **a1** — 'What did we win' is answered from the stated goals of #1268-#1271 and the code and tests observed today, not from runtime or performance measurements (agent's call).
- **a2** — The diagram shows the source-level architecture inside the single daemon process. It is not a deployment diagram, because there are no separate processes (agent's call).
- **a3** — The owner is the reader of the report and wants plain language, not a package-by-package listing (agent's call).


## Open questions

### q1 — Where should the final architecture answer live?

_Asked of: Product Owner Questions_

- **a** — Issue comment only. Fastest, no code change. Downside: the diagram gets buried in the thread. (recommended)  *(recommended)*
- **b** — Issue comment plus a new architecture page in the repo docs. The diagram stays current with the code, but it becomes a small docs change that has to be built and reviewed.

### q2 — Should the answer include what to extract next?

_Asked of: Product Owner Questions_

- **a** — Yes, a short ranked list of next steps (for example: splitting the large 'turn' package, removing the last direct store imports from messaging and the connector, adding tests to the untested services). Advice only, no issues created. (recommended)  *(recommended)*
- **b** — No. Only report what happened and what we won.
- **c** — Yes, and also draft follow-up issues for you to approve.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
