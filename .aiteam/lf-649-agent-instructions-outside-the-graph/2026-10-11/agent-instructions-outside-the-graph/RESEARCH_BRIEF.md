# Research brief

## Request

Re-research all factory agent instruction sources for #649 (including judge and newer chat, conversation and routing agents), elaborate the judge findings, and list every agent call as Group | Topic | Instructions.

## Decisions

- **d1** — Superseded by the owner's design: instructions ship as a versioned internal package in the binary, loaded like a team on first start and after an update, with a JSON catalogue mapping hardcoded role keys to agents and skills.
- **d3** — Owner decision retained: capture text and digest at each unit of work; already-captured work keeps its bytes. A role is captured as one unit with all its agents and skills.
- **d7** — Boundary retained: no change to agent behaviour, tools, models or supported project languages; no visual editor.
- **d9** — Owner decision retained: graph extras and story protocol instructions stay internal for the later Book of Standards (#728).
- **d11** — Role keys (for example judge/challenger_role) are hardcoded in the factory. The catalogue returns only the agents and skills for a key and cannot change tools, models, sandbox, schemas or other enforcement.
- **d12** — Calling a role loads its full set (role, agents, skills) in catalogue order as one captured unit; a missing or invalid member stops the call.


## Assumptions

- **a5** — Editable scope is instruction wording including correction and variant text. Schemas, decoders, tool ceilings, owner confirmation, redaction, evidence binding and size checks stay in code.
- **a7** — Operators own selection; project content, evidence and model output cannot choose roles or widen permissions.
- **a14** — Location assumed as a hidden internal folder in the factory home, one folder per shipped version; exact name is design work.
- **a15** — Single-shot roles receive skills as labelled appended sections; tool-capable roles receive them as skills. Extra agents are allowed only where code already permits delegation; judge and doctor keep none.
- **a16** — Conversations and workflow judges keep the version captured at their start; new work uses the newest loaded version.
- **a13** — Coverage limit: repository source only; no live factory-home audit; no providers or tests run.


## Open questions

### q1 — How do you edit an internal package once the factory has loaded it?

_Asked of: Product Owner Questions_

- **a** — Clone it, like a team. The shipped package is read-only and versioned. You clone it into your own copy, edit that, and select it. A new release never touches your clone; the factory only tells you a newer shipped version exists. Safest: no merge or overwrite problems, and one mechanism for teams and internal roles.  *(recommended)*
- **b** — Edit in place. The loaded files in the factory home are editable. An update replaces unedited files and keeps edited ones, with a notice. Simpler for you, but it needs the compare-before-replace and backup rules, and the shipped and edited bytes can drift apart.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
