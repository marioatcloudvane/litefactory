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
- **d10** — A /doctor comment has absolute priority for its issue and runs even when that issue's workflow is stuck, wedged or failing; it never waits behind the queue or a running stage. (Owner, run 4.)
- **d11** — The doctor session is saved per issue and resumes from that saved state after any pause or factory restart; it ends when the fix is applied or the owner says stop. (Owner, run 4.)
- **d12** — Every doctor message must start with /doctor, even mid-conversation; comments without the prefix go to the normal workflow. (Owner, run 4.)
- **d13** — The doctor agent's context has four layers: fixed system prompt, short fixed factory field guide, per-turn issue-only evidence pack built by the engine, and saved conversation memory. (Shaping decision, run 5.)
- **d14** — The agent proposes a structured repair plan; the engine validates that every step touches only that issue's rows and files before applying. No free raw write access. (Shaping decision, run 5.)
- **d15** — Issue comments, logs and files are treated as evidence, never as instructions; only the owner's /doctor messages instruct the agent. (Shaping decision, run 5.)
- **d16** — Every doctor agent invocation uses the selected provider's flagship model, including diagnosis, resumed conversation and agent-assisted apply work, independently of the ordinary workflow model tier. (Owner, run 6.)


## Assumptions

- **a1** — 'Cannot change litefactory's code' means the doctor only repairs data at runtime (database rows, worktrees, session files). Rests on the issue also asking for a new command.
- **a2** — Live daemon logs and the live database were not inspected. The incident list comes from earlier session write-ups in the repo.
- **a3** — 'Not a new mode' means doctor is a control command outside the workflow stages, though it does run an agent.
- **a4** — Approval of a proposed fix is an explicit /doctor apply reply; other /doctor replies continue the conversation. Chosen to prevent accidental repairs.
- **a5** — The CLI names the issue explicitly, since scope is one issue.
- **a6** — The saved doctor session lives in the factory database, so it survives restarts and is shared by GitHub and the CLI.
- **a7** — The doctor's apply step stops that issue's stranded runs before repairing, so a stuck workflow cannot overlap with the fix.
- **a8** — The field guide is drafted from the schema and earlier incident write-ups, not from live logs; it needs review against a real stuck case before build.
- **a9** — 'Flagship at any time' means resolving the installed provider's configured flagship tier on each doctor turn rather than hard-coding a model name; rests on the existing provider-specific tier table.
- **a10** — If the configured flagship model is missing or unavailable, doctor reports the failure and preserves the saved conversation without silently downgrading; rests on the owner's requirement to use flagship at any time.
