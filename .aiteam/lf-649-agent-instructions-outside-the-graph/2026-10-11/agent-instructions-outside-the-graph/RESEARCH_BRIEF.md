# Research brief

## Request

Re-research all factory agent instruction sources for #649 (including judge and newer chat, conversation and routing agents), elaborate the judge findings, and list every agent call as Group | Topic | Instructions.

## Decisions

- **d1** — Superseded by the owner's design: instructions ship as a versioned internal package in the binary, loaded like a team on first start and after an update, with a YAML catalogue of categories, each with a list of roles, naming the agents and skills for each role.
- **d3** — Owner decision retained: capture text and digest at each unit of work; already-captured work keeps its bytes. A role is captured as one unit with all its agents and skills.
- **d7** — Boundary retained: no change to agent behaviour, tools, models or supported project languages; no visual editor.
- **d9** — Owner decision retained: graph extras and story protocol instructions stay internal for the later Book of Standards (#728).
- **d11** — Category and role names are hardcoded in the factory. The catalogue lists each category once with its roles and returns only the agents and skills for a role. It cannot change tools, models, sandbox, schemas or other enforcement. Unknown, missing or duplicate names fail at load.
- **d12** — Calling a role loads its entry agent and its skills, in catalogue order, as one captured unit; a missing or invalid member stops the call. Each role has exactly one entry agent. The key subagents is reserved in the format and rejected when non-empty.
- **d13** — Owner decision: operators edit the loaded files in place. Updates replace unedited files, keep edited ones with a notice, and compare exact bytes before replacing; invalid edits are saved with a .invalid name and the shipped file is loaded again.
- **d14** — Owner decision: the catalogue is YAML, with categories each holding an array of roles.
- **d15** — Owner decision: reserve a subagents key per role (renamed from helpers). A future entry carries name, description and agent file. Building subagents is out of scope for this issue.


## Assumptions

- **a5** — Editable scope is instruction wording including correction and variant text. Schemas, decoders, tool ceilings, owner confirmation, redaction, evidence binding and size checks stay in code.
- **a7** — Operators own selection; project content, evidence and model output cannot choose roles or widen permissions.
- **a14** — Location assumed as a hidden internal folder in the factory home, one folder per shipped version; edited files carry forward to a newly loaded version. Exact name is design work.
- **a15** — Single-shot roles receive skills as labelled appended sections; tool-capable roles receive them as skills. No role has subagents in this scope: judge and doctor forbid nested agents, conversation disables them, and single-shot calls have no tools.
- **a16** — Conversations and workflow judges keep the version captured at their start; new work uses the current files.
- **a17** — A conversation runs on one provider; the same text reaches it twice today (system channel and inside the prompt), and both copies must come from the catalogue.
- **a13** — Coverage limit: repository source only; no live factory-home audit; no providers or tests run. The sample catalogue's skill and agent names are illustrative.
- **a18** — One Markdown source per subagent can be rendered for both providers (Claude --agents JSON; Codex TOML plus -c overrides). Unverified by a live run; whoever builds subagents must prove it on both providers.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
