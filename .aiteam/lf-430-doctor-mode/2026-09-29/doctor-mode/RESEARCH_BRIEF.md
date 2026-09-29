# Research brief

## Request

Add a /doctor <instructions> GitHub command and a CLI command that share one implementation. It must run even when workflow stages are stuck or wedged, be lightweight, diagnose problems and repair state (database rows, stuck processes, broken files). It must not change litefactory's code and is not a new mode. Also study recent failures from logs and evaluate options.

## Decisions

- **d1** — Doctor must be picked up ahead of the per-task queue and the CLI must work without the daemon, both calling one shared implementation.
- **d2** — Follow the existing safe pattern: diagnose and preview by default, change state only on explicit apply.


## Assumptions

- **a1** — 'Cannot change litefactory's code' means the doctor only repairs data (database rows, worktrees, session files) at runtime. Building the doctor itself is allowed new code. Rests on the issue also asking for a new command.
- **a2** — Live daemon logs and the live database were not inspected in this run. The incident list comes from prior session write-ups in the repo, not fresh logs.
- **a3** — 'Not a new mode' means /doctor is a control command like /schedule and /confirm and never starts an agent run.
