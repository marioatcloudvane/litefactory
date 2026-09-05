# US-7 — Codex renderer

> **RECONCILIATION NOTE (2026-09-05, light).** `plan_brief.md`'s Story Index carries this story as
> `_(rescoped, light)_`. The Codex-specific output shape (skill directory at
> `.codex/skills/<agent>/SKILL.md`) is expected to survive largely unchanged; what may shift is
> the renderer's *input*, per US-5's reconciliation note. This story now also depends on US-18
> (root-file composition), not reflected in the Depends-On list below. A full body rewrite is
> deferred to a follow-up planning pass. See `plan_brief.md`'s top-of-file Reconciliation Note.

> As an operator running Codex against this repo, I want each of my team's agents rendered as a
> Codex skill, so that Codex picks it up without any manual conversion.

This story implements the Codex-specific renderer against the contract defined in US-5.

## Why This Matters

Codex has no equivalent of Claude's per-agent tool-restriction list, so this renderer has to make
a deliberate choice about what to do with a field the target format simply has no place for —
silently ignoring it and openly omitting it are different behaviors, and only one of them is
honest about what Codex will actually enforce.

## Architecture Notes

- Implements `Render` for harness key `codex`, registered in US-5's harness map.
- Output path: `.codex/skills/<agent>/SKILL.md`.
- Tool-restriction fields are omitted outright (Codex has no equivalent), not mapped to a
  best-effort approximation — this is a deliberate, visible omission, not silent data loss, since
  omission here means "Codex enforces nothing here," which must stay true.
- Determinism and golden-file test coverage for this harness are delivered by US-5's T-17, not
  duplicated as a separate task here.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-15 | Codex renderer: `.codex/skills/<agent>/SKILL.md` with `name`/`description`; tool-restriction fields omitted | logic | US-5 T-13 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The Codex renderer writes `SKILL.md` with `name` and `description`, and omits tool-restriction fields Codex has no equivalent for. |
| AC-2 | Determinism (byte-identical repeat renders) and golden-file coverage for this harness are verified by US-5's shared renderer test suite. |
