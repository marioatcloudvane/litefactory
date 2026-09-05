# US-8 — opencode renderer

> **RECONCILIATION NOTE (2026-09-05, light).** `plan_brief.md`'s Story Index carries this story as
> `_(rescoped, light)_`. The opencode-specific output shape (`.opencode/agents/<agent>.md` with a
> structured `permission` object) is expected to survive largely unchanged; what may shift is the
> renderer's *input*, per US-5's reconciliation note. This story now also depends on US-18
> (root-file composition), not reflected in the Depends-On list below. A full body rewrite is
> deferred to a follow-up planning pass. See `plan_brief.md`'s top-of-file Reconciliation Note.

> As an operator running opencode against this repo, I want each of my team's agents rendered as
> an opencode agent file, so that opencode picks it up without any manual conversion.

This story implements the opencode-specific renderer against the contract defined in US-5.

## Why This Matters

opencode's agent format carries a structured `permission` object rather than a flat tool list,
which is the field-shape opencode's own file format actually requires — this renderer's job is to
map the resolved agent's permission-related settings into that shape faithfully, and to fail
loudly rather than approximate if a permission setting doesn't have a clean opencode equivalent.

## Architecture Notes

- Implements `Render` for harness key `opencode`, registered in US-5's harness map.
- Output path: `.opencode/agents/<agent>.md`.
- litefactory generates this file but does not run opencode as a harness — that remains explicitly
  out of scope (see `plan_brief.md` Out of Scope). This story is file generation only.
- Determinism and golden-file test coverage for this harness are delivered by US-5's T-17, not
  duplicated as a separate task here.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-16 | opencode renderer: `.opencode/agents/<agent>.md`, `description` frontmatter + `permission` object | logic | US-5 T-13 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The opencode renderer writes an agent file with a `description` frontmatter field and a `permission` object reflecting the agent's configured permissions. |
| AC-2 | Determinism (byte-identical repeat renders) and golden-file coverage for this harness are verified by US-5's shared renderer test suite. |
