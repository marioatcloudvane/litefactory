# US-5 — Pluggable per-harness renderer contract

> **RECONCILIATION NOTE (2026-09-05).** `plan_brief.md`'s Story Index now titles this story
> "Pluggable per-harness renderer contract (markdown source → per-harness output)." Under the
> corrected design the renderer's input is a resolved **markdown source file** referenced by the
> index (plus whatever root-file composition US-18 requires), not a fully-merged, three-layer
> `ResolvedAgent` struct. **Everything below this note still describes the REJECTED design's
> input shape** — the contract's *purity and failure-mode* requirements (loud failure on an
> inexpressible field, determinism obligations, one implementation per harness in a map) are
> expected to survive; the exact `Render(agent ResolvedAgent)` signature is not. A full body
> rewrite is deferred to a follow-up planning pass. New dependency: this story now also depends on
> US-18 (root-file composition), not reflected below. See `plan_brief.md`'s top-of-file
> Reconciliation Note.

> As a platform maintainer, I want one shared agent definition to translate into any number of
> harness-specific file formats through a single pluggable contract, so that adding a new harness
> later doesn't require touching every renderer that already exists.

This story defines the renderer contract every concrete harness (Claude, Codex, opencode, and
whatever comes next) implements against.

## Why This Matters

Three AI coding harnesses each want an agent described in their own, quite different file format
— YAML frontmatter plus Markdown, a skill directory, an agent file with a permissions object —
and that list will grow. The plan considered three ways to bridge one shared definition to many
output formats and is committing to one of them here, deliberately, rather than leaving it as a
menu of options: a small pure-function contract, one implementation per harness, selected from a
map by name. The part that's easy to get wrong is failure behavior — a renderer that can't express
some field of an agent's definition must say so loudly and refuse to write anything, rather than
quietly dropping the field, which is exactly the failure mode template-file approaches default to
and exactly why they were rejected.

## Architecture Notes

- **Recommendation and the two rejected alternatives (per-harness template files; a common
  intermediate schema with adapters) are recorded in full, with reasoning, in `plan_brief.md`'s
  Shared Decisions §3.** Implement against that text; it is not repeated here.
- Contract: `Render(agent ResolvedAgent) ([]RenderedFile, error)`, where `RenderedFile{Path,
  Bytes}` is deliberately **not** the lockfile's `Artifact` type — a renderer cannot compute
  `renderedHash`/`sourceHash`/`baseSourceHash` and must not be asked to. The caller hashes only
  after every renderer in a run has succeeded.
- Renderers are pure: no filesystem access, no network, no clock. Determinism obligations (sort
  before iterating any map, never range a map inside a template, explicit trailing newline, no
  timestamp/version/hostname in output) are binding on every renderer — see `plan_brief.md`
  Cross-Story Constraints: Platform.
- This story's own test task (T-17) depends on the three concrete renderers (US-6, US-7, US-8)
  existing — it is the last task to close within this story, not the first, even though it's
  filed here because it exercises the contract itself.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-13 | Renderer interface + harness registry map keyed by name + `RenderedFile{Path, Bytes}` type | api | US-2 T-08 | S |
| T-17 | Tests: golden-file + render-twice-byte-equality per harness; fake-harness extensibility test (a test-only harness added purely as a new map entry, no other package touched) | test | US-6 T-14, US-7 T-15, US-8 T-16 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | A fake harness, added as a new map entry only, passes the same contract test suite with no change to any other package. |
| AC-2 | A renderer given a field it cannot express returns an error naming the agent, the harness, and the dotted field path; nothing is written. |
| AC-3 | Two consecutive renders of the same resolved agent produce byte-identical output, for every registered harness. |
