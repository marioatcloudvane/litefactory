# Research brief

## Request

Research how to move every agent instruction outside the graph modes into editable Markdown files: inventory of what exists, proposed layout and loading design, split between editable wording and code-enforced safety, migration order, and a story split for Plan.

## Decisions

- **d1** — Settled by owner: all non-graph agent wording becomes Markdown files embedded in the litefactory binary, one copy each, extracted to the factory home on first run. Not installable like teams. Go keeps enforcement only, and no Go copy of moved text remains.
- **d2** — Settled by owner: users edit the extracted files.
- **d3** — Settled by owner: the file text is captured at the start of each unit of work with its digest recorded.
- **d4** — Owner's reading of a missing or invalid file: re-extract the shipped file. Shaping adds that a broken edit is saved as .invalid first and the user is told.
- **d5** — Shaping decision: on upgrade, unedited files (digest matches what was extracted) are replaced; edited files are kept and flagged as having a newer shipped version.
- **d6** — Migration order: loader and extraction with shipped-file test, then conflict and schedule, then chat and #631 agents, then doctor with its field guide, then graph extras if the owner agrees. Each step deletes its Go text in the same change.
- **d7** — Out of scope: changing agent behaviour, tools or models; the story progress event schema; team-style install or clone; visual editor.


## Assumptions

- **a1** — Superseded by the owner: factory agents are not a team-style package. They are embedded in the binary and extracted on first run.
- **a2** — Superseded by the owner: users edit the extracted files. Upgrade handling is decision d5.
- **a3** — Settled: the runtime captures a factory agent's text at the start of each unit of work and records its digest next to the existing prompt-version columns.
- **a4** — Superseded by the owner: a missing or invalid file is re-extracted from the binary (decision d4), not failed closed. Tests load every shipped file and check frontmatter, size, required placeholders and that no Go constant with the same text remains.
- **a5** — What must stay in Go regardless of text: tool and sandbox limits; isolation proof; the result protocol and schema (decoders, citation checks, event file schema and byte limits); redaction before sending and after receiving; the untrusted-data wrapper around factory-supplied context (labelled JSON appended by code after the editable text); prompt size bounds. The editable file holds tone, scope, task and examples.
- **a6** — Superseded by decision d6 (migration order).


## Open questions

### q5 — Should the extra rules the factory adds on top of graph roles also become files?

_Asked of: Product Owner Questions_

- **a** — Yes. Research, Plan, Implement and Validate each already have a role file. The factory also adds hidden Go text on top, for example 'do not commit' and how to report results. In #643 that hidden text contradicted the role file and only a code change could fix it. This option moves the wording of those extras into the same extracted files, so you can see and edit it. The hard enforcement (the result protocol, blocking commits) stays in Go. Done as the last stories.  *(recommended)*
- **b** — No. Leave the graph extras in Go for a separate issue. This issue covers only the non-graph agents. The #643 drift risk stays.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
