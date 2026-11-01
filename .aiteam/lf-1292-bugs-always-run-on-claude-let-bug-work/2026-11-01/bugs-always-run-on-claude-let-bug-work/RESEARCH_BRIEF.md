# Research brief

## Request

Let Bug work run on Codex as well as Claude: an explicit Coding Agent label wins; unlabelled and factory-filed Bugs use an agent the owner configures for Bug work (or inherit from the parent task); verify a Codex-labelled Bug runs analyst, fixer, judge and delivery on Codex with Codex grants; show agent and model in the Bug report.

## Decisions

- **S-01** — mode=change+feature, size=medium — existing agent selection changes for Bugs and a new Bug-agent setting is added. (agent's call)
- **O-01** — A Bug with an explicit Coding Agent label runs analyst, fixer, judge and delivery on that agent with that agent's Bug grants. (owner said: 'an explicit Coding Agent label on the Bug wins')
- **O-02** — An unlabelled or factory-filed Bug uses the owner-configured Bug agent, or inherits from its parent task, not the first listed agent. (owner said: 'Factory-filed Bugs get the agent the owner configures for Bug work (or inherit it from the parent task)')
- **O-03** — The Bug report shows which agent and model ran it. (owner said: 'The Bug report shows which agent and model ran it.')
- **K-01** — With no new setting and no label, behaviour stays as today (first configured agent), so existing installs are unaffected. (agent's call)


## Assumptions

- **A-01** — Labelled Bugs on Codex probably already work through the shared path; the automated end-to-end check the issue asks for is still needed to prove it. (agent's call, rests on F-04 and F-06)
- **A-02** — Where the Bug-agent setting lives, and inheritance order (label, parent, Bug setting, first agent), are shaping decisions; this pass records them as open. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
