# US-5 — Pluggable per-harness renderer contract (markdown source → per-harness output)

> As a platform operator, I want one harness-neutral agent definition to produce correct output for
> every harness litefactory supports, through a contract that fails loudly rather than silently
> dropping something it can't express, so that adding a new harness never means rewriting every
> existing agent.

Delivers the pluggable renderer contract — a pure function per harness, registered in a map — whose
input is now a resolved markdown source (frontmatter plus body) rather than a merged config object.

## Why This Matters

Three different tools (Claude, Codex, opencode) each want agent definitions in a different shape —
YAML frontmatter plus markdown, a skill directory, an agent file with a permission object — and
this feature needs to support new tools later without touching every agent already defined. The
hardest part isn't formatting: it's making sure the tool never quietly loses information it doesn't
know how to express in one of the formats — it has to say so loudly instead. This story also
settles a question the earlier plan left implicit: once there's no merged config object anymore,
where do the per-harness settings a renderer actually needs — a tool list, a model, permissions —
live? The answer is recorded here as an explicit assumption rather than left open.

## Architecture Notes

- The recommendation and the two rejected alternatives (per-harness template files; a common
  intermediate schema with per-harness adapters) are recorded once, in `plan_brief.md` Shared
  Decision 3, and are not repeated here.
- **Input shape, resolved:** the `Render` contract's input is no longer `ResolvedAgent` (a merged
  three-layer struct). It is a `ResolvedSource` built from the markdown file `AgentEntry.Source`
  points at: a small structured **frontmatter** block (YAML, at the top of the source file — `name`,
  `description`, `tools`, `model`, and any other harness-relevant field a renderer needs) plus the
  **body** (the markdown prompt content). This is a new, explicit assumption: the pure index model
  removes the merged config object, so per-agent structured fields have to live somewhere, and the
  natural place is frontmatter on the same file the index already points at — one file to edit, not
  two.
- Contract: `Render(source ResolvedSource) ([]RenderedFile, error)`, `RenderedFile{Path, Bytes}` —
  deliberately not the lockfile's `Artifact` type (a renderer doesn't compute hashes or know US-10's
  classification scheme; the caller hashes/classifies after every renderer in the run succeeds).
- Renderers are pure (no fs/network/clock access). Determinism obligations, unchanged: sort before
  iterating any map, never range a map inside a template, explicit `\n` endings with exactly one
  trailing newline, no timestamp/version/hostname in output.
- A renderer given a frontmatter field it cannot express in its target format fails loudly, naming
  the agent key, the harness, and the dotted field path — no partial write, no silent drop.
- **New dependency: US-18** (root-file composition). A renderer's output can now also be a
  marker-tagged section of a shared root file (`AGENTS.md`/`CLAUDE.md`), not only a standalone
  per-agent file. This story's contract accommodates both shapes by letting `RenderedFile.Path`
  name either a standalone file or, per US-18's convention, a target root file plus a
  marker-tagged-section identity — the mechanics of marker insertion/detection are US-18's own
  scope, not repeated here.
- T-13's dependency changes from the old `US-2 T-08` (three-layer merge) to US-2's rewritten T-08
  (index-source-selection) — the resolved index plus the markdown tree it points at. T-17 (the
  shared contract test) depends on the three concrete renderers (US-6/7/8) existing and closes last
  within this story, exercising all three plus a fake fourth harness for the extensibility check.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-13 | `Renderer` interface + harness registry map + `RenderedFile{Path,Bytes}` + `ResolvedSource{Frontmatter,Body}` types | api | US-2 T-08 | S |
| T-17 | Tests: golden-file + render-twice byte-equality per harness; fake-harness extensibility test | test | US-6 T-14, US-7 T-15, US-8 T-16 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A fake harness, added purely as a new map entry, passes the same contract test suite as the three real harnesses without any other package being touched. |
| AC-2 | A renderer given a frontmatter field it cannot express in its target harness format fails loudly, naming the agent key, the harness, and the dotted field path — nothing is written. |
| AC-3 | Two consecutive renders of the same resolved source produce byte-identical output, for every registered harness. |
