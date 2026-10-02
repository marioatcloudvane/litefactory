# Research brief

## Request

Spike is a supported mode, but there is no clear definition of what it should solve. The owner wants Spike to run as a graph (like Research, Plan and Bug) and asks what could be done with the spike command.

## Decisions

- **d1** — This research does not choose what Spike should solve on its own. The owner chose it (see d7).
- **d2** — Flow: investigate, optional one question round, size gate, then either note and report or fix, check, note and report. Declared entirely in the DOT file and built from the shared agent, loop, if, ask and comment steps plus three new Spike activities.
- **d3** — The same JSON envelope and MCP result route are reused. The spike payload keeps its fields and gains an optional proposed fix and an optional fix report. Hand-back advice uses the existing recommendation field.
- **d4** — The existing /spike command starts the graph; no new command, one report on the issue.
- **d5** — The investigator role is read-only; only the fixer role may write code, after the size gate says small. Each agent node declares its access level in the DOT file.
- **d6** — Write the missing Spike guide and fix the broken links in CLAUDE.md and AGENTS.md as part of this work.
- **d7** — Owner decision (q1 b): Spike investigates, then optionally makes one small tested fix; anything larger is handed back to Research or Plan.
- **d8** — Owner decision (q2 a): a small fix is one defect, at most three changed files including the test, a regression test, and no new package, dependency, schema or API change.
- **d9** — Owner decision (q3 a): no owner approval stage; the graph runs to one report.
- **d10** — Owner decision (q4 b): the investigator may ask one round of questions, and SPIKE.md is written before the wait. Only graph Spike accepts questions; legacy Spike keeps rejecting them.
- **d11** — Owner decision (q5 a): graph Spike has its own temporary repo setting, default legacy, graph opt-in. It is removed at retirement (d14).
- **d12** — Three new activities are needed: spike note, spike gate and spike check. Each does one measurable thing and returns an outcome. Everything else in the graph is reused.
- **d13** — Owner instruction: the DOT file is the leading definition of the Spike flow. No Spike workflow logic in Go: order, branches, limits, access level, size thresholds and note status are declared in the DOT file. Go supplies generic steps only.
- **d14** — Owner instruction: retire the old Spike when the graph is done. Retirement removes the legacy single-agent route, the old mode file, the no-questions rule, the legacy finalization guidance, Spike's borrowed Implement tools and the temporary switch. It is the final story of this work.
- **d15** — Graph-led is proven by acceptance checks: a swap test with a different DOT and no Go change, no Spike-specific topology validator, no DOT node names in Spike Go code, one embedded copy of the flow, and roles that never describe the next step.


## Assumptions

- **a1** — 'Graph-driven' means Spike should run like Research, Plan and Bug: a fixed DOT flow, factory-controlled limits and checkpoints, a role file and a result contract. Based on the issue title and the existing graph docs.
- **a2** — The word 'spike' is used in its usual agile sense: a time-boxed exploration that ends in an answer, not a finished feature.
- **a3** — Archived SPIKE.md notes are a fair sample of real use. This is a research pass, not a spike, so reading them was allowed here.
- **a4** — The time box is the graph's fixed limits: investigator two runs, fixer one run, one question, one check, plus existing timeouts. No per-repo budget.
- **a5** — The Spike graph reports through the existing comment step, so no new publishing code is needed.
- **a6** — Graph Spike keeps the current rule of not reading archived sessions.
- **a7** — The fixer cannot ask questions. The size gate has already fixed the plan; a blocked fixer records what is not done and the check reports it.
- **a8** — A failed check is reported, not retried. A retry loop would grow the fast lane into Bug.
- **a9** — The check activity can reuse the Bug check-runner code. Not verified; confirm in planning.
- **a10** — Investigator and fixer get separate tool profiles, as Bug's two roles do. Not verified for a mode; confirm in planning.
- **a11** — 'Retire the old spike mode' means retiring the legacy single-agent route. The /spike command and the Spike mode identity stay as the graph's key. Based on how deeply the identity is used (f15). Owner to correct if the command itself should go.
- **a12** — 'When done' means after graph Spike is the default and has completed real runs. Retirement is the last story and cannot ship before the graph.
- **a13** — A response the DOT does not route (for example the fixer answering ask) fails the node instead of being ignored. Recommendation only; confirm in planning.
- **a14** — Spike tasks already admitted on the legacy path need a drain rule (finish or close) before legacy code is deleted, because an admitted task keeps its path.
