# US-8 — opencode renderer → `.opencode/agents/<agent>.md`

> As a platform operator using opencode as my coding agent harness, I want each shared agent
> definition rendered into opencode's own agent-file format automatically, so that I never
> hand-write `.opencode/agents/*.md` files myself.

Delivers the opencode renderer, converting a resolved markdown source's frontmatter and body into
`.opencode/agents/<agent>.md`.

## Why This Matters

opencode has its own agent-file convention, distinct from both Claude's and Codex's. Supporting it
without a renderer would mean a third hand-maintained copy of every agent definition.

## Architecture Notes

- Implements `Render` for harness key `opencode`; input is US-5's `ResolvedSource{Frontmatter,
  Body}`.
- Output path `.opencode/agents/<agent>.md` for a standalone artifact, or — for an agent composed
  into a root file per US-18 — the marker-tagged section US-18 defines, mirroring US-6's Claude
  renderer in supporting both output shapes.
- opencode support is itself an open item (Open Question 10, credential/format details still
  thin in RESEARCH_BRIEF.md); this story implements the format as best understood from the brief
  and flags any remaining format uncertainty as a task-level risk rather than blocking the story.
- A frontmatter field with no opencode equivalent fails loudly per US-5 AC-2, naming the field's
  dotted path.
- Determinism/golden-file coverage is delivered by US-5's shared T-17, not duplicated here.
- **New dependency: US-18**, for the root-file composition output shape.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-16 | opencode renderer: agent-file layout + frontmatter-to-opencode-field mapping; standalone-file and marker-tagged-section output shapes | logic | US-5 T-13, US-18 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | Output lands at `.opencode/agents/<agent>.md` with frontmatter fields mapped to their opencode equivalents. |
| AC-2 | A frontmatter field with no opencode equivalent fails loudly naming the dotted field path, rather than being silently dropped. |
| AC-3 | Determinism/golden-file coverage is verified by US-5's shared suite. |
