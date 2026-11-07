# Research brief

## Request

Add a label setup command (e.g. `litefactory repo labels setup <project>`), also run when a project is added. For GitHub it ensures Mode, Coding Agent and bug labels. For Linear it finds or creates grouped labels, stores their IDs on the ticket connection, and reads grouped labels. It checks first, creates only what is missing, is idempotent, has a dry run with confirmation or --yes, and extends `repo connection check`.

## Decisions

- **S-01** — mode=feature+integration, size=large — a new command spanning two trackers, the connection store, the connection check and grouped-label reading. (agent's call)
- **O-01** — A setup command `litefactory repo labels setup <project>` shows a dry run (found, would create, would store) and applies on confirmation or --yes. Running it twice changes nothing. (owner said: "It is idempotent: running it twice changes nothing.")
- **O-02** — GitHub: ensure Mode: Research|Plan|Implement|Validate|Spike, Coding Agent: per registered runner, and bug. Match existing labels by exact name, case-insensitively, and adopt them. Create only missing ones, with a consistent colour and short description. (owner said: issue body, GitHub expectations)
- **O-03** — Linear: find or create the groups Mode and Coding Agent with children, plus a bug label. Store the mode, coding-agent and bug IDs on the connection. (owner said: issue body, Linear expectations)
- **O-04** — `repo connection check` reports missing or unmapped labels and points to the setup command. (owner said: issue body)
- **O-05** — The Linear reader recognises grouped labels (`Mode › Plan`, `Coding Agent › Codex`) and today's flat names (`Mode: Plan`, `Coding Agent: Codex`) the same way. (owner said: issue body, "read labels with their parent group")
- **O-06** — A Linear connection can be saved without label maps; the check reports "labels not set up", mode work is refused with the existing plain reason, and setup fills the maps. (owner said: A-01)
- **O-07** — This work adds the optional Linear bug-label setting; #1425 reuses it unchanged. (owner said: A-02)
- **O-08** — Setup runs only when the owner runs the command; `repo add` and `repo connection set` print a one-line hint naming it. (owner said: A-03)
- **O-09** — On Linear, a story's inherited Coding Agent label is written using the stored coding-agent map, as on GitHub. (owner said: A-04)
- **O-10** — On Linear, setup looks in the team and at workspace level, adopts a match from either, and creates missing labels in the team only. (owner said: A-05)
- **O-11** — Labels are set up only on the tracker the project uses for tickets: GitHub labels for a GitHub-tracked project, Linear labels for a Linear-tracked one. (agent's call)
- **O-12** — A label that already exists in flat form (`Mode: Plan`) is adopted; no group is created beside it. A group is created only when neither form exists. (agent's call, rests on K-01 'never duplicated')
- **O-13** — An ID already stored on the connection wins over a label found by name; a mismatch is shown in the dry run and never changed silently. (agent's call, rests on K-01)
- **O-14** — Without a terminal and without --yes, the command only shows the dry run and writes nothing. (agent's call)
- **O-15** — The bug label is matched by the name `bug`, case-insensitively, so Linear's default `Bug` label is adopted. (agent's call)
- **O-16** — If a create fails, the command stops, reports what was created and what failed, and stores nothing; a rerun adopts what was created and finishes. (agent's call)
- **O-17** — A run that finds nothing to change writes no new connection revision. (agent's call, rests on f2 immutable versions and O-01)
- **O-18** — Missing coding-agent or bug mappings are reported by the check as warnings with the setup hint; missing mode mappings keep today's blocking report. (agent's call)
- **O-19** — Proof: behaviour is tested with fakes for both trackers; the owner runs one live dry run and apply against a Linear test team and a GitHub test repository to confirm the label-group API. (agent's call, rests on f13)
- **N-01** — No deleting, renaming or recolouring of any label, including old grey auto-created ones such as lisisite's `Mode: Research`. (owner said: "never duplicated, renamed, recoloured or deleted")
- **N-02** — No moving existing flat Linear labels into groups. (agent's call)
- **N-03** — No labels beyond Mode, Coding Agent and bug (no priority, type or status labels). (agent's call)
- **N-04** — No removal of a Coding Agent label when a runner is no longer registered. (agent's call)
- **N-05** — No creating Linear labels at workspace level. (owner said: A-05)
- **N-06** — No change to #1425's filing behaviour; it only gains a filled setting. (agent's call)
- **N-07** — No background or scheduled label sync; setup runs on request only. (owner said: A-03)
- **K-01** — Never rename, recolour, delete or duplicate existing labels, and never touch labels the factory does not own. Existing cockpit (MAR) connections and flat names such as `Mode: Plan` keep working unchanged. (owner said: issue body)
- **K-02** — `repo add` stays offline. (agent's call, rests on f9)
- **K-03** — A connection without the new maps behaves exactly as today, and manual `--mode-labels` editing stays supported. (agent's call)
- **K-04** — GitHub mode and agent label names and how the daemon reads them stay unchanged. (agent's call)
- **A-01** — q1 "How does a Linear connection exist before its labels are set up?" — owner: "q1: a, q2: a, q3: a, q4: a, q5: a" (option a). (owner said: comment 6667479608)
- **A-02** — q2 "Who adds the Linear bug-label setting, this work or #1425?" — owner: "q1: a, q2: a, q3: a, q4: a, q5: a" (option a). (owner said: comment 6667479608)
- **A-03** — q3 "When does label setup run?" — owner: "q1: a, q2: a, q3: a, q4: a, q5: a" (option a). (owner said: comment 6667479608)
- **A-04** — q4 "Should Linear stories also get their parent's Coding Agent label written, now that its ID is stored?" — owner: "q1: a, q2: a, q3: a, q4: a, q5: a" (option a). (owner said: comment 6667479608)
- **A-05** — q5 "Where does setup look for and create Linear labels?" — owner: "q1: a, q2: a, q3: a, q4: a, q5: a" (option a). (owner said: comment 6667479608)


## Assumptions

- **as1** — The Linear bug-label setting is added by this work and reused by #1425. (owner said: A-02) Rests on f7.
- **as2** — 'One per registered runner' means the runners the factory registers today (Claude and Codex). Setup reads that list at run time rather than hard-coding it. (agent's call)
- **as3** — Superseded by O-08: 'when a project is added' becomes a hint printed by `repo add` and `repo connection set`, not an automatic run. (agent's call, rests on f9)
- **as4** — The GitHub label colour and descriptions are the factory's choice, one colour per family (Mode, Coding Agent, bug). (agent's call)
- **as5** — Linear labels in one group are mutually exclusive on an issue, which fits one mode and one coding agent per issue. Rests on Linear's product behaviour; to confirm in the live check (O-19). (agent's call)
- **as6** — Created Linear children go in the same scope as the group they join: under a team group in the team. If only a workspace group exists and a child is missing, setup reports it instead of creating it. (agent's call, rests on O-10)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
