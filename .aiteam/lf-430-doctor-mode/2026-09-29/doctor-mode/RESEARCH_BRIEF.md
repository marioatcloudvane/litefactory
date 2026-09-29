# Research brief

## Request

Add a /doctor <instructions> GitHub command and a CLI command that share one implementation. It must run even when workflow stages are stuck or wedged, be lightweight, diagnose problems and repair state (database rows, stuck processes, broken files). It must not change litefactory's code and is not a new mode. Also study recent failures from logs and evaluate options.

## Decisions

- **d1** — Doctor must be picked up ahead of the per-task queue and the CLI must work without the daemon, both calling one shared implementation.
- **d2** — Follow the existing safe pattern: diagnose and preview by default, change state only on explicit apply.
- **d3** — Doctor is a control command that never starts an agent run and never changes litefactory's code or history.


## Assumptions

- **a1** — 'Cannot change litefactory's code' means the doctor only repairs data (database rows, worktrees, session files) at runtime. Building the doctor itself is allowed new code. Rests on the issue also asking for a new command.
- **a2** — Live daemon logs and the live database were not inspected in this run. The incident list comes from prior session write-ups in the repo, not fresh logs.
- **a3** — 'Not a new mode' means /doctor is a control command like /schedule and /confirm and never starts an agent run.


## Open questions

### q1 — How much may the doctor change on its own?

_Asked of: Product Owner Questions_

- **a** — Report only. It never changes anything. Safest, but you still fix things by hand.
- **b** — Preview first, change only on explicit apply (`/doctor apply`, CLI `--apply`). You see the plan before anything moves.  *(recommended)*
- **c** — Auto-fix a short safe list, and preview the rest. Faster, but changes happen unseen.

### q2 — Who may send /doctor on GitHub?

_Asked of: Product Owner Questions_

- **a** — Only the repository owner, the same person who controls other commands. Simplest and safest.  *(recommended)*
- **b** — Owner plus collaborators with write access. Useful for teams, wider risk.
- **c** — Anyone who can comment. Not safe; it can change state.

### q3 — Which repairs ship in the first version?

_Asked of: Product Owner Questions_

- **a** — The five known incidents: stranded running rows, unrecorded posted report, stale poll flag on closed issue, malformed command wedge, dirty worktree from failed run.  *(recommended)*
- **b** — Diagnose only, across all state, with no repairs yet.
- **c** — The five incidents plus a broad integrity scan of the database and session files, report only.

### q4 — How are the free-text instructions read?

_Asked of: Technical Questions_

- **a** — Plain rules: a target (for example `task 430`) and an optional check name. No AI, so it stays fast and works when agents are broken.  *(recommended)*
- **b** — Hand the text to an AI agent to decide what to check. Flexible, but it needs the very system that may be broken.
