# Research brief

## Request

Spike is a supported mode, but there is no clear definition of what it should solve. The owner wants Spike to run as a graph (like Research, Plan and Bug) and asks what could be done with the spike command.

## Decisions

- **d1** — This research does not choose what Spike should solve. It records the options for the shaping step and recommends keeping Spike short and evidence-first.
- **d2** — Flow is investigate, size check, optional fix, check, report, built only from existing shared steps (agent, if, comment, and ask if q4 is yes).
- **d3** — Reuse the current Spike result format and add only a routing verdict (small fix or hand back).
- **d4** — The existing /spike command starts the graph; no new command, one report on the issue.
- **d5** — Investigation step is read-only; only the fix step may write code, after the size check passes.
- **d6** — Write the missing Spike guide and fix the broken links in CLAUDE.md and AGENTS.md as part of this work.


## Assumptions

- **a1** — 'Graph-driven' means Spike should run like Research, Plan and Bug: a fixed DOT flow, factory-controlled limits and checkpoints, a role file and a result contract. Based on the issue title and the existing graph docs.
- **a2** — The word 'spike' is used in its usual agile sense: a time-boxed exploration that ends in an answer, not a finished feature. Based on the mode file's own text and on what the archived Spikes did.
- **a3** — Archived SPIKE.md notes are a fair sample of real use. This is a research pass, not a spike, so reading them was allowed here.
- **a4** — The time box is the graph's fixed limits: one investigation run, one fix run, existing timeouts. No per-repo budget. Rests on how Research limits its own steps.
- **a5** — The Spike graph reports through the existing comment step, so no new publishing code is needed.
- **a6** — Graph Spike keeps the current rule of not reading archived sessions.


## Open questions

### q1 — What should a graph-driven Spike be for?

_Asked of: Product Owner Questions_

- **a** — Investigate only: measure or find a root cause, write SPIKE.md, never change code. Cheapest and clearly apart from Implement, but a one-line fix needs a second run.
- **b** — Investigate, then optionally make one small tested fix. If the fix is not small, stop and hand back to Research or Plan. Matches how Spike was really used (measure, root cause, one-line fix with a test).  *(recommended)*
- **c** — Decision spike: compare two or three approaches and recommend one for Plan. Closest to the agile meaning, but overlaps with Research.

### q2 — What counts as a 'small fix' the graph will allow?

_Asked of: Product Owner Questions_

- **a** — Hard rule: one defect, at most three changed files including the test, a regression test that fails then passes, and no new package, dependency, schema or API change. Anything else is handed back.  *(recommended)*
- **b** — Agent's judgement only, as today. Flexible, but bundles of fixes (four stacked defects in one past Spike) keep happening.
- **c** — Stricter: one source file plus its test. Very safe, but hands back many legitimate one-line fixes.

### q3 — Should the owner check Spike's work before it is final?

_Asked of: Product Owner Questions_

- **a** — No approval stage. The graph runs to a report, and you review the SPIKE.md and any fix like any other change. Keeps the fast lane fast.  *(recommended)*
- **b** — Owner approves the investigation before any fix is made. Safer, but every fixing Spike now takes two owner round trips.
- **c** — Owner approval only when a fix touches code. Middle ground, but still adds a wait to the most common Spike.

### q4 — May a Spike ask the owner a question when it is blocked?

_Asked of: Product Owner Questions_

- **a** — No. A blocked Spike writes SPIKE.md with the blocker and options and ends. Same as today; your reply starts a new run.
- **b** — Yes, one round of questions with options and a recommendation, then the run resumes with your answer. Needs a small change to the Spike result format, which rejects questions today.  *(recommended)*

### q5 — How is graph Spike switched on?

_Asked of: Technical Questions_

- **a** — Its own repo setting, default legacy (the current Spike), with graph opt-in. Same pattern as Research, but Spike can be trialled and rolled back on its own.  *(recommended)*
- **b** — Reuse the Research setting. Less code, but one switch then controls two unrelated modes.
- **c** — Always on, replace the current Spike now. Simplest end state, but no fallback while graph execution is still opt-in for Research.
