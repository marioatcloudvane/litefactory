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
- **O-06** — A Linear connection can be saved without label maps; the check reports "labels not set up" and setup fills them. (agent's call, pending q1)
- **O-07** — This work adds the optional Linear bug-label setting that #1425 then reuses. (agent's call, pending q2)
- **O-08** — Setup runs only when the owner runs the command; `repo add` and `repo connection set` print a one-line hint naming it. (agent's call, pending q3)
- **O-09** — On Linear, a story's inherited Coding Agent label is written using the stored coding-agent map, as on GitHub. (agent's call, pending q4)
- **O-10** — On Linear, setup looks in the team and at workspace level, adopts a match from either, and creates missing labels in the team only. (agent's call, pending q5)
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
- **N-05** — No creating Linear labels at workspace level. (agent's call, pending q5)
- **N-06** — No change to #1425's filing behaviour; it only gains a filled setting. (agent's call)
- **N-07** — No background or scheduled label sync; setup runs on request only. (agent's call, pending q3)
- **K-01** — Never rename, recolour, delete or duplicate existing labels, and never touch labels the factory does not own. Existing cockpit (MAR) connections and flat names such as `Mode: Plan` keep working unchanged. (owner said: issue body)
- **K-02** — `repo add` stays offline. (agent's call, rests on f9)
- **K-03** — A connection without the new maps behaves exactly as today, and manual `--mode-labels` editing stays supported. (agent's call)
- **K-04** — GitHub mode and agent label names and how the daemon reads them stay unchanged. (agent's call)


## Assumptions

- **as1** — The Linear bug-label map lives in the same connection setting that #1425 introduces. If #1425 is not merged first, this work adds the field and #1425 reuses it. (agent's call) Rests on f7. Now asked as q2.
- **as2** — 'One per registered runner' means the runners the factory registers today (Claude and Codex). Setup reads that list at run time rather than hard-coding it. (agent's call)
- **as3** — Superseded by O-08: 'when a project is added' becomes a hint printed by `repo add` and `repo connection set`, not an automatic run. (agent's call, rests on f9)
- **as4** — The GitHub label colour and descriptions are the factory's choice, one colour per family (Mode, Coding Agent, bug). (agent's call)
- **as5** — Linear labels in one group are mutually exclusive on an issue, which fits one mode and one coding agent per issue. Rests on Linear's product behaviour; to confirm in the live check (O-19). (agent's call)
- **as6** — Created Linear children go in the same scope as the group they join: under a team group in the team. If only a workspace group exists and a child is missing, setup reports it instead of creating it. (agent's call, rests on O-10)


## Open questions

### q2 — Who adds the Linear bug-label setting, this work or #1425?

_Asked of: Product Owner Questions_

- **a** — Recommended. This work adds the optional bug-label setting, and #1425 reuses it unchanged. #1425's rule that filing never creates a label still holds, because only setup creates labels. Nothing waits.  *(recommended)*
- **b** — Wait until #1425 is merged and fill its setting. Cleaner ownership, but this work is blocked until then.

### q3 — When does label setup run?

_Asked of: Product Owner Questions_

- **a** — Recommended. Only when you run the command. `repo add` and `repo connection set` print a one-line hint naming it. `repo add` keeps its offline promise and nothing changes on a tracker without you seeing the dry run.  *(recommended)*
- **b** — `repo add` runs the dry run on its own. Less to remember, but `repo add` then needs network and tracker credentials.
- **c** — `repo connection set` runs the dry run after saving a connection. The credentials exist at that point, but every connection edit then talks to the tracker.

### q4 — Should Linear stories also get their parent's Coding Agent label written, now that its ID is stored?

_Asked of: Product Owner Questions_

- **a** — Recommended. Yes. Today the factory tries to copy the parent's Coding Agent label onto a story, and that fails on Linear because only mode labels can be written there. With the stored map, the copy works on Linear like on GitHub. It is a small addition and closes a gap the code already names.  *(recommended)*
- **b** — No. Only reading grouped labels is in scope; writing waits for a follow-up. Smaller change, and stories still run with the parent's agent as today.

### q1 — How does a Linear connection exist before its labels are set up?

_Asked of: Technical Questions_

- **a** — Recommended. `repo connection set` may save a Linear connection without label maps. The check reports it as "labels not set up" and mode work is refused with the existing plain reason. Setup then fills the maps. Simplest order: connect, then set up.  *(recommended)*
- **b** — The setup command also creates the connection: it takes team, project and credential itself. One step, but two commands then write connections.
- **c** — Keep maps required. The owner maps mode labels by hand first, and setup only adds the rest. No store change, but a fresh project still needs hand-made IDs, which this issue wants to end.

### q5 — Where does setup look for and create Linear labels?

_Asked of: Technical Questions_

- **a** — Recommended. Look in the connection's team and at workspace level, and adopt a match from either. Create missing labels in the team only. Your existing groups are found wherever they live, and setup never needs workspace admin rights.  *(recommended)*
- **b** — Team only, for both looking and creating. Simpler, but a workspace-level `Mode` group would be missed and duplicated in the team.
- **c** — Look in both and create at workspace level, so all teams share one set. That needs admin rights and changes other teams, which goes beyond this project.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
