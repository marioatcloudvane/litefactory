# Research brief

## Request

Research how to move every agent instruction outside the graph modes into editable Markdown files: inventory of what exists, proposed layout and loading design, split between editable wording and code-enforced safety, migration order, and a story split for Plan.

## Decisions

- **d1** — Owner-set scope: non-graph agent roles ship as embedded Markdown extracted to the factory home, while graph extras and protocol instructions remain internal.
- **d2** — Settled by owner: users edit the extracted files.
- **d3** — Settled by owner: the file text is captured at the start of each unit of work with its digest recorded.
- **d4** — Owner's reading of a missing or invalid file: re-extract the shipped file. Shaping adds that a broken edit is saved as .invalid first and the user is told.
- **d5** — Shaping decision: on upgrade, unedited files (digest matches what was extracted) are replaced; edited files are kept and flagged as having a newer shipped version.
- **d6** — Shaping boundary: extraction and loading cover conflict, schedule, chat and doctor; each moved role loses its duplicate program-code text, and graph extras are excluded.
- **d7** — Out of scope: changing agent behaviour, tools or models; the story progress event schema; team-style install or clone; visual editor.
- **d8** — Clarified: 'Go' means the language litefactory is written in, not the projects it builds. The fixed text added to graph roles is stack-neutral (checked: no Go-specific commands in implement policy, result finalization or mode hints), so non-Go projects such as Python are unaffected.
- **d9** — Owner answered q5 no: graph extras remain internal until a later Book of Standards MCP that agents can call and query.
- **d10** — One linked Book of Standards issue proposal was retained with hash 09572e2ec682f1133d86dff61f99ecd6ecc30d2730e9099bf53ee4e8e823ac78; publication awaits the owner's decision.


## Assumptions

- **a1** — Superseded by the owner: factory agents are not a team-style package. They are embedded in the binary and extracted on first run.
- **a2** — Superseded by the owner: users edit the extracted files. Upgrade handling is decision d5.
- **a3** — Settled: the runtime captures a factory agent's text at the start of each unit of work and records its digest next to the existing prompt-version columns.
- **a4** — Superseded by the owner: a missing or invalid file is re-extracted from the binary (decision d4), not failed closed. Tests load every shipped file and check frontmatter, size, required placeholders and that no Go constant with the same text remains.
- **a5** — What must stay in Go regardless of text: tool and sandbox limits; isolation proof; the result protocol and schema (decoders, citation checks, event file schema and byte limits); redaction before sending and after receiving; the untrusted-data wrapper around factory-supplied context (labelled JSON appended by code after the editable text); prompt size bounds. The editable file holds tone, scope, task and examples.
- **a6** — Superseded by decision d6 (migration order).
- **a7** — Shaping assumption based on current isolation and call contracts: operator-owned factory files supply stable identity, description and instruction body, while project inputs and agent outputs cannot select instruction sources or widen permissions.
- **a8** — Shaping assumption based on captured graph work: store shipped release/digest, last extracted digest and captured bytes/digest; retries and resumes reuse capture, and new units read current valid files.
- **a9** — Shaping recovery rule: preserve invalid edits in unique backups; failed read, backup, extraction or invalid shipped defaults prevent launch rather than silently discarding edits or using unvalidated instructions.
- **a10** — Scope boundary: #631 supplies future intent/reply behavior; #649 supplies their instruction source contract and preserves current standard launch, model, tool and result behavior for existing agents.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
