# Research brief

## Request

Research how to move every agent instruction outside the graph modes into editable Markdown files: inventory of what exists, proposed layout and loading design, split between editable wording and code-enforced safety, migration order, and a story split for Plan.

## Decisions

- **d1** — Proposed scope: all non-graph agent wording becomes editable files, versioned and released, captured with a digest per unit of work. Go keeps enforcement only, and no Go copy remains.
- **d2** — Migration order: loader and shipped-file test, then conflict and schedule, then chat and #631 agents, then doctor with its field guide, then graph hard rules. Each step deletes its Go text in the same change.
- **d3** — Out of scope: changing agent behaviour, tools or models; the story progress event schema; a visual editor.


## Assumptions

- **a1** — Reading: factory agents (doctor, chat, conflict, schedule) are not stack-specific, so they belong in one factory-level package, versioned and released the same way as a team, rather than copied into every team. Graph-node fixed text (implement hard rules, result protocol) should move into the team roles/skills where it is wording, because it is stack-agnostic wording the roles already hold. This rests on the four-copy source layout (f9) and on how few of the non-graph agents need stack context.
- **a2** — Reading: a user edit should be a per-factory copy (as in #323/#346) or an operator override like 'team manage', so upgrades keep the edit and show it as locally-modified. The exact edit and upgrade-merge mechanism is for shaping to settle with the owner.
- **a3** — Reading: the runtime should capture a factory agent's text at the start of the unit of work (a doctor attempt, a chat turn, a conflict resolution) and record its digest next to the existing prompt-version columns, so a long job is not changed mid-flight by an edit. Each call needs the digest, not only the version string.
- **a4** — Reading: a missing or invalid file should fail closed for the agent call with a plain notice (chat already has 'Answers unavailable right now'; doctor and conflict already have error paths), never fall back to a Go copy. This follows the 'no Go copy left behind' goal in the issue. Tests would load every shipped file and check frontmatter, size, required placeholders and that no Go constant with the same text remains.
- **a5** — Reading: what must stay in Go regardless of text: tool and sandbox limits; isolation proof; the result protocol and schema (decoders, citation checks, event file schema and byte limits); redaction before sending and after receiving; the untrusted-data wrapper around factory-supplied context (labelled JSON appended by code after the editable text); prompt size bounds. The editable file holds tone, scope, task and examples. Even if a user deletes the 'treat as untrusted' sentence, code still wraps and labels the data.
- **a6** — Reading on migration order, for shaping to confirm: (1) a loader and shipped-file validity test, with the new factory-level package; (2) conflict and schedule advisors (smallest, tool-free, clear split); (3) chat ask and draft, then the new #631 agents written as files from the start; (4) doctor diagnosis and repair together with the field guide; (5) the graph hard rules and fixed appended text, moved into the team roles, with the Go constant deleted in the same change. Each step deletes its Go text in the same change, so there are never two sources of truth.


## Open questions

### q1 — Where do the factory-agent files live?

_Asked of: Product Owner Questions_

- **a** — One factory-level package, versioned and released like a team. One copy of each file.  *(recommended)*
- **b** — Inside every team package. Four copies of the same files to keep in sync.
- **c** — Plain unversioned files in the factory home. Simple, but no release or upgrade story.

### q2 — How does a factory user edit these files and keep the edit across upgrades?

_Asked of: Product Owner Questions_

- **a** — A per-factory copy, like team clone. Status shows locally-modified, and an upgrade never overwrites it without --force.  *(recommended)*
- **b** — Override by path only, like team manage set source. Lighter, but no status or merge hint.
- **c** — Edit the installed files in place. Upgrades would overwrite them.

### q5 — Do graph hard rules and fixed appended text move in this issue?

_Asked of: Product Owner Questions_

- **a** — Yes, as the last stories. The wording goes into team roles. The result protocol and no-commit enforcement stay in Go.  *(recommended)*
- **b** — No. Leave graph text for a separate issue. The #643 drift risk stays.

### q3 — What happens when an edited file is missing or invalid?

_Asked of: Technical Questions_

- **a** — Fail closed for that call with a plain notice that names the file. No fallback.  *(recommended)*
- **b** — Fall back to the shipped file. The edit mistake would be hidden.

### q4 — When is a file's text captured?

_Asked of: Technical Questions_

- **a** — At the start of each unit of work (doctor attempt, chat turn, conflict run), with its digest recorded.  *(recommended)*
- **b** — Once at process start. Edits need a restart, and long runs still differ across restarts.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
