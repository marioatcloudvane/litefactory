# Research brief

## Request

Let Bug work run on Codex as well as Claude: an explicit Coding Agent label wins; unlabelled and factory-filed Bugs use an agent the owner configures for Bug work (or inherit from the parent task); verify a Codex-labelled Bug runs analyst, fixer, judge and delivery on Codex with Codex grants; show agent and model in the Bug report.

## Decisions

- **S-01** — mode=change+feature, size=medium — existing agent selection changes for Bugs and a new Bug-agent setting is added. (agent's call)
- **O-01** — A Bug with an explicit Coding Agent label runs analyst, fixer, judge and delivery on that agent with that agent's Bug grants. (owner said: 'an explicit Coding Agent label on the Bug wins')
- **O-02** — An unlabelled or factory-filed Bug uses the owner-configured Bug agent, or inherits from its parent task, not the first listed agent. (owner said: 'Factory-filed Bugs get the agent the owner configures for Bug work (or inherit it from the parent task)')
- **O-03** — The Bug report shows which agent and model ran it. (owner said: 'The Bug report shows which agent and model ran it.')
- **O-04** — Automated tests prove a Codex-labelled Bug runs analyst, fixer, judge and delivery on Codex with Codex grants, and that unlabelled and log-analysis Bugs get the configured Bug agent. (owner said: 'Verify end to end ... (Auto) A factory-filed Bug ... gets the configured Bug agent.')
- **O-05** — Order of precedence for a Bug's agent: explicit label, then parent task's agent, then Bug setting, then first listed agent. (agent's call)
- **N-01** — No change to how stories choose agents. (agent's call)
- **N-02** — No Linear label writing or Linear Bug filing. (agent's call)
- **N-03** — The manual real-Bug Codex delivery is the owner's check; the agent does not run it. (agent's call)
- **K-01** — With no new setting and no label, behaviour stays as today (first configured agent), so existing installs are unaffected. (agent's call)
- **K-02** — Ambiguous or unknown Coding Agent labels keep being refused as today. (agent's call)


## Assumptions

- **A-01** — Labelled Bugs on Codex probably already work through the shared path; the automated end-to-end check is still needed to prove it. (agent's call, rests on F-04 and F-06)
- **A-02** — Inheritance order is label, parent, Bug setting, first agent (O-05); the setting's home and the filing approach await q01 and q02, recommended a and a. (agent's call)
- **A-03** — The model shown is the one the run reported; if a run reports none, the report says 'not reported'. Added to the shared run report so all workflows show it. (agent's call)


## Open questions

### q01 — Where do you set the agent for Bug work?

_Asked of: Product Owner Questions_

- **a** — New optional config key `bug_agent` (e.g. codex). Unset means first listed agent, so existing installs do not change.  *(recommended)*
- **b** — No setting. Bugs only use a label or the parent's agent; otherwise first listed agent. Smallest change, but your Claude-outage case stays open for log-analysis Bugs.
- **c** — Per-workflow agent map in config (bug, story, and so on). Most flexible, biggest change.

### q02 — How do factory-filed Bugs get their agent?

_Asked of: Technical Questions_

- **a** — Resolve at dispatch: no label, no parent agent, so use `bug_agent`. One rule covers log analysis, Validate follow-ups, babysitter and hand-filed Bugs. Nothing is written to the issue.  *(recommended)*
- **b** — Write a `Coding Agent: X` label when the factory files the Bug. Visible on the issue, but needs changes at each filing site and Linear cannot take it.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
