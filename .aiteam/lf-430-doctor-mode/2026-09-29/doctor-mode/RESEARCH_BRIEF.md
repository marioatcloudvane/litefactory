# Research brief

## Request

Add a /doctor <instructions> GitHub command and a CLI command that share one implementation. It must run even when workflow stages are stuck or wedged, be lightweight, diagnose problems and repair state (database rows, stuck processes, broken files). It must not change litefactory's code and is not a new mode. Also study recent failures from logs and evaluate options.

## Decisions

- **d1** — Doctor is picked up ahead of the per-task queue; the CLI works without the daemon; both share one engine.
- **d2** — Analysis is read-only and posts a proposed fix; changes happen only after explicit owner approval.
- **d3** — Doctor is a control command, not a mode, and never changes litefactory's code or deletes history.
- **d4** — Doctor only examines and repairs the issue the command came from. (Owner comment A.)
- **d5** — An installed agent finds the problem, given database, file and log context for that issue. (Owner comment B.)
- **d6** — Only the factory owner may send /doctor; others are ignored silently. (Owner comment C.)
- **d7** — Doctor is conversational: analyze, propose, owner replies with /doctor, repeat until the fix goes live. (Owner comment D.)
- **d8** — One forced factory restart, only after a repair is applied. (Owner comment D.)
- **d9** — A backup of affected rows and files is taken and every change logged before applying. (Shaping decision.)


## Assumptions

- **a1** — 'Cannot change litefactory's code' means the doctor only repairs data at runtime (database rows, worktrees, session files). Rests on the issue also asking for a new command.
- **a2** — Live daemon logs and the live database were not inspected. The incident list comes from earlier session write-ups in the repo.
- **a3** — 'Not a new mode' means doctor is a control command outside the workflow stages, though it does run an agent. Rests on owner comments B and the issue text.
- **a4** — Approval of a proposed fix is an explicit reply such as /doctor apply; other replies continue the conversation. Chosen to prevent accidental repairs.
- **a5** — The CLI names the issue explicitly, since scope is one issue.
