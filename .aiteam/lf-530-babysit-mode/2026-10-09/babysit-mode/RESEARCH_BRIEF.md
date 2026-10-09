# Research brief

## Request

Research what exists today for a Bug-only babysit mode (issue #530): triggering, approval authority, agent and tier selection, rights, load.

## Decisions

- **D1** — Scope stays Bug workflow only. Settled by the owner comment of 2026-10-08.
- **D2** — Babysitter actions are limited to approve within limits, one send-back, flake answers and starting queued Bugs; all else goes to the owner. My decision.
- **D3** — Send back once per Bug, then leave to the owner. Settled by the issue text.
- **D4** — Off switch halts all babysitter actions immediately. My decision.
- **D5** — Summary time is one configured daily time in the daemon. Assumed from the earlier plan.


## Assumptions

- **A1** — Babysit approval will be a new daemon-authenticated path tied to one analysis revision, not an owner-impersonating comment. Rests on F1, F2 and the issue text 'never as the owner's identity'.
- **A2** — Load-flake guidance and queueing build on the same watch loop as the challenger. Rests on the issue text and the earlier story dependencies.
- **A3** — Only the owner can use /babysit commands. Rests on F1 that the owner is the only instruction source.


## Open questions

### Q2 — Which limits must a Bug analysis meet for the babysitter to approve it?

_Asked of: Product Owner Questions_

- **a** — Recommended: at most 10 files, at most 3 packages, no migration, no new stored state. Matches what you accepted on #513.  *(recommended)*
- **b** — Stricter: at most 5 files, 2 packages. Safer, more send-backs.
- **c** — Looser: at most 20 files. Faster, more risk.

### Q3 — Which agent gets swapped when two coding agents are configured?

_Asked of: Product Owner Questions_

- **a** — Recommended: the challenger only. It uses the agent that did not write the analysis, always on flagship. The fixer stays as configured.  *(recommended)*
- **b** — Challenger and fixer. The fixer also switches after a failed attempt. Bigger change with a new hook.
- **c** — Neither. Always flagship on the default agent.

### Q1 — How should a babysitter approval be recorded?

_Asked of: Technical Questions_

- **a** — Recommended: a new daemon-authenticated approval, author 'babysitter', naming the owner's delegation and the exact analysis revision, plus a signed comment. Owner-only rule stays for everyone else.  *(recommended)*
- **b** — Loosen the owner check so the bot may approve. Simpler, but it weakens the safety rule for every workflow.

### Q4 — What does 'machine not saturated' mean for starting the next queued Bug?

_Asked of: Technical Questions_

- **a** — Recommended: active runs are below the max-concurrent-runs setting, and the one-minute load average per CPU core is below 1.0.  *(recommended)*
- **b** — Run count only. Simplest, ignores real load.
- **c** — Real load only. Ignores the existing limit.

### Q5 — How does the babysitter recognise a known load flake?

_Asked of: Technical Questions_

- **a** — Recommended: a maintained list of known flaky tests in config, plus a rerun in isolation that must pass before it answers. Unknown failures go to the owner.  *(recommended)*
- **b** — List only, no rerun. Faster, may mask a real failure.
- **c** — Rerun only, no list. Catches new flakes, costs a run each time.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
