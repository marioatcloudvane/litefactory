# US-7 — Codex renderer → `.codex/skills/<agent>/SKILL.md`

> As a platform operator using Codex as my coding agent harness, I want each shared agent definition
> rendered into Codex's own skill-directory format automatically, so that I never hand-write
> `.codex/skills/*/SKILL.md` files myself.

Delivers the Codex renderer, converting a resolved markdown source's frontmatter and body into a
skill directory.

## Why This Matters

Codex reads agent-like definitions as skill directories, one directory per skill, not a flat file
per agent the way Claude does. Without a renderer, that structural difference means hand-building a
different layout for the same agent every time it needs to run under Codex.

## Architecture Notes

- Implements `Render` for harness key `codex`; input is US-5's `ResolvedSource{Frontmatter, Body}`.
- Output path `.codex/skills/<agent>/SKILL.md` for a standalone artifact. Codex's skill-directory
  shape has no natural equivalent to a shared root-file section, so this renderer does not
  participate in US-18's marker-tagged-section output shape — it only ever emits standalone files.
  This is recorded here rather than left to be discovered as a gap when US-18 lands.
- A frontmatter field with no Codex equivalent (e.g. Claude's `tools` list, if Codex's skill format
  has no matching concept) fails loudly per US-5 AC-2, naming the field's dotted path — it is not
  silently dropped.
- Determinism/golden-file coverage is delivered by US-5's shared T-17, not duplicated here.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-15 | Codex renderer: skill-directory layout (`SKILL.md` + frontmatter-to-Codex-field mapping), standalone-file output only | logic | US-5 T-13 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | Output lands at `.codex/skills/<agent>/SKILL.md` with frontmatter fields mapped to their Codex equivalents. |
| AC-2 | A frontmatter field with no Codex equivalent fails loudly naming the dotted field path, rather than being silently dropped. |
| AC-3 | Determinism/golden-file coverage is verified by US-5's shared suite. |
