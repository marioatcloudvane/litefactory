# Research brief

## Request

Research how to make Discord command replies natural-language answers written by an agent from the factory's data, with findings, a design reusing existing profiles and a story split.

## Decisions

- **d1** — Chat intent and replies run as agent markdown roles through the standard agent runner, replacing the earlier plan to reuse the /ask native model with a new profile.
- **d2** — Story split: chat workflow and intent agent; per-command reply agents with strict checker and raw fallback; service hook with details, paging and exact strings; project-less metering and config switch; live Discord connection and confirmation flow; docs.
- **d3** — Owner answers: strict verbatim number/link check (q1 c); narrate every command (q2); language follows latest /ask else English (q3 a); live Discord connection (q4 a); state changes always need a plain yes (q5 a).
- **d4** — Intent agent returns command, parameters and reading; the service validates it against enabled commands; unsure means one clarifying question and no action.
- **d5** — Intent on balanced tier, reply agents on fast tier, each reply agent sees only its own command's data.


## Assumptions

- **a1** — Narration and intent live in the transport-neutral messaging service, not the Discord adapter.
- **a2** — Exact tokens and fixed strings in /question, /answer and /help replies are appended unchanged, outside the narrated text.
- **a3** — Without free text the reply language follows the latest /ask turn, else English.
- **a4** — On agent failure or failed number/link check the raw redacted text is sent, marked as raw.
- **a5** — The chat roles ship in the team package as a small new workflow; Plan settles packaging and versioning.
- **a6** — The standard agent runner can run a role with no tools and no project; otherwise a small runner change is included.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
