# Research brief

## Request

Add a label setup command (e.g. `litefactory repo labels setup <project>`), also run when a project is added. For GitHub it ensures Mode, Coding Agent and bug labels. For Linear it finds or creates grouped labels, stores their IDs on the ticket connection, and reads grouped labels. It checks first, creates only what is missing, is idempotent, has a dry run with confirmation or --yes, and extends `repo connection check`.

## Decisions

- **S-01** — mode=feature+integration, size=large — a new command spanning two trackers, the connection store and the connection check. (agent's call)
- **O-01** — A setup command shows a dry run (found, would create, would store) and applies on confirmation or --yes. Running it twice changes nothing. (owner said: "It is idempotent: running it twice changes nothing.")
- **O-02** — GitHub: ensure Mode: Research|Plan|Implement|Validate|Spike, Coding Agent: per registered runner, and bug. Match existing labels by exact name, case-insensitively, and adopt them. Create only missing ones, with a consistent colour and short description. (owner said: issue body, GitHub expectations)
- **O-03** — Linear: find or create the groups Mode and Coding Agent with children, plus a bug label. Store the mode, coding-agent and bug IDs on the connection. Read grouped and flat names. (owner said: issue body, Linear expectations)
- **O-04** — `repo connection check` reports missing or unmapped labels and points to the setup command. (owner said: issue body)
- **K-01** — Never rename, recolour, delete or duplicate existing labels, and never touch labels the factory does not own. Existing cockpit (MAR) connections and flat names such as `Mode: Plan` keep working unchanged. (owner said: issue body)


## Assumptions

- **as1** — The Linear bug-label map lives in the same connection setting that #1425 introduces. If #1425 is not merged first, this work adds the field and #1425 reuses it. (agent's call) Rests on f7.
- **as2** — 'One per registered runner' means the runners the factory registers today (Claude and Codex). Setup reads that list at run time rather than hard-coding it. (agent's call)
- **as3** — 'When a project is added' means `repo add` offers or runs setup only when a ticket connection with credentials exists. Otherwise the command is run later. (agent's call)
- **as4** — The GitHub label colour and descriptions are the factory's choice, one colour per family (Mode, Coding Agent, bug). (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
