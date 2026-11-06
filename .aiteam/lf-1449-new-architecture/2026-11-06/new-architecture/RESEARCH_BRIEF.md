# Research brief

## Request

Explain the impact of the recent extraction stories (#1251 and its sub-stories #1268, #1269, #1270, #1271), say what was won, and give a diagram of the new application architecture.

## Decisions

- **S-01** — mode=refactor, size=small — the request is a read-only report on a finished refactoring plus diagrams; no code change is asked for, so the work is smaller than first sized. (agent's call)
- **O-01** — The final answer states what changed, what was won and what it cost, using the measured line counts in f7 and the boundary evidence in f2-f6. (agent's call, from the issue request)
- **O-02** — The final answer carries the current source-level architecture diagram and a small before diagram based on commit 436cb605. (agent's call, from the issue request)
- **O-03** — The final answer separates proven from unproven: boundary tests pass at c05ebd29; live daemon upgrade and runtime performance are not proven. (agent's call)
- **O-04** — The final answer ends with a short ranked list of recommended next steps, advice only, with no issues created. (owner said: "proceed", taking q2 option a)
- **N-01** — No code changes and no further extraction work are in scope for this request. (agent's call)
- **N-02** — No runtime, speed or load measurements. (agent's call)
- **N-03** — No deployment diagram, because the app still runs as one process. (agent's call)
- **N-04** — No live upgrade or recovery check on the deployed daemon; that stays the owner's check. (agent's call)
- **K-01** — No source, schema, configuration, test or docs change; the answer lives only on the issue. (owner said: "proceed", taking q1 option a)
- **A-01** — q1 "Where should the final architecture answer live?" — owner: "proceed" (read as the recommended option a: issue comment only). (owner said: "proceed")
- **A-02** — q2 "Should the answer include what to extract next?" — owner: "proceed" (read as the recommended option a: ranked advice list, no issues). (owner said: "proceed")
- **N-05** — No follow-up issues are drafted or created for the recommended next steps. (owner said: "proceed", taking q2 option a)


## Assumptions

- **a1** — 'What did we win' is answered from the stated goals of #1268-#1271 and the code and tests observed today, not from runtime or performance measurements (agent's call).
- **a2** — The diagram shows the source-level architecture inside the single daemon process. It is not a deployment diagram, because there are no separate processes (agent's call).
- **a3** — The owner is the reader of the report and wants plain language, not a package-by-package listing (agent's call).
- **a4** — The owner's reply "proceed" accepts every recommended option from the previous review (agent's call).

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
