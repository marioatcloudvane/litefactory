# US-6 — Claude renderer → `.claude/agents/<agent>.md`

> As a platform operator using Claude as my coding agent harness, I want each shared agent
> definition rendered into Claude's own sub-agent format automatically, so that I never hand-write
> `.claude/agents/*.md` files myself.

Delivers the Claude renderer, converting a resolved markdown source's frontmatter and body into
`.claude/agents/<agent>.md`.

## Why This Matters

Claude reads sub-agents from a specific file location with a YAML frontmatter convention. Without a
renderer, every agent added to a team means someone hand-writing that file correctly, including
keeping the tool list in sync as it changes.

## Architecture Notes

- Implements `Render` for harness key `claude`; input is US-5's `ResolvedSource{Frontmatter, Body}`
  (frontmatter carries `name`, `description`, `tools`, etc.).
- Output path `.claude/agents/<agent>.md` for a standalone artifact, or — for an agent composed
  into a root file per US-18 — the marker-tagged section US-18 defines. This renderer supports both
  output shapes per US-5's contract.
- Frontmatter's `tools` field renders as a comma-separated list.
- Determinism/golden-file coverage is delivered by US-5's shared T-17, not duplicated here.
- **New dependency: US-18**, for the root-file composition output shape.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-14 | Claude renderer: YAML frontmatter (name/description/tools-as-comma-list) + markdown body; standalone-file and marker-tagged-section output shapes | logic | US-5 T-13, US-18 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | Frontmatter has `name` and `description`; `tools` renders as a comma-separated list. |
| AC-2 | Determinism/golden-file coverage is verified by US-5's shared suite. |
