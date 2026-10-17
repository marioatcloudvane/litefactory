# Research brief

## Request

Re-research all factory agent instruction sources for #649 (including judge and newer chat, conversation and routing agents), elaborate the judge findings, and list every agent call as Group | Topic | Instructions.

## Decisions

- **d1** — Owner decision retained: factory agent instructions ship embedded in the binary and are extracted into the factory home; not installable team packages.
- **d2** — Owner decision retained: operators edit extracted files.
- **d3** — Owner decision retained: capture text and digest at each unit of work; already-captured work keeps its bytes.
- **d4** — Owner decision retained: missing or invalid files are re-extracted. Backup and notice details are shaping assumptions.
- **d7** — Boundary retained: no change to agent behaviour, tools, models or supported project languages; no team install/clone or visual editor.
- **d9** — Owner decision retained: graph extras and story protocol instructions stay internal for the later Book of Standards (#728, already created).


## Assumptions

- **a5** — Editable scope is instruction wording including correction and variant text. Schemas, decoders, tool ceilings, owner confirmation, redaction, evidence binding and size checks stay in code.
- **a7** — Operators own instruction selection; project content, evidence and model output cannot choose sources or widen permissions.
- **a8** — Suggested layout: one embedded catalog mirrored under the factory home with stable IDs per row of the table (doctor diagnosis, field guide, repair, repair authority, ask, draft, intent, reply writer, reply checker, conversation native, factory rules, outcome, advisors, classifier, judge), with correction variants as separate parts.
- **a9** — Upgrade replace/preserve, unique invalid backups and safe launch failure remain shaping assumptions; none are implemented in the inspected paths.
- **a11** — Suggested split: shared lifecycle with mandatory capture; advisors plus classifier; response-only chat (ask, draft, intent, reply writer and checker); stateful conversation including provider system text; doctor; judge extraction. A proposal, not a selected story set.
- **a12** — Open design point: when an edit takes effect for long-lived native conversations and workflow judges. Recovery reuses captured bytes and never re-extracts captured work.
- **a13** — Coverage limit: repository source and supplied history only; no live factory-home audit. Table text for the Validate/Implement scope blocks, the progress events and the field guide is abbreviated; full text is in the cited files.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
