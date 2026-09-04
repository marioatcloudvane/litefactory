# US-6 — Claude renderer

> As an operator running Claude Code against this repo, I want each of my team's agents rendered
> as a Claude sub-agent file, so that Claude picks it up without any manual conversion.

This story implements the Claude-specific renderer against the contract defined in US-5.

## Why This Matters

Claude Code expects a sub-agent as a Markdown file with a specific YAML frontmatter shape. Rather
than an operator hand-writing that file and keeping it in sync with the team's shared definition
by hand, this story generates it directly from the resolved agent — the part worth calling out is
that Claude's `tools` field wants a flat comma-separated list, which is a small but easy-to-miss
formatting detail that a hand-maintained file would silently get subtly wrong over time.

## Architecture Notes

- Implements `Render` for harness key `claude`, registered in US-5's harness map — no changes
  outside this renderer's own file are expected.
- Output path: `.claude/agents/<agent>.md`.
- Determinism and golden-file test coverage for this harness are delivered by US-5's T-17, not
  duplicated as a separate task here.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-14 | Claude renderer: `.claude/agents/<agent>.md`, YAML frontmatter (`name`, `description`, `tools` as comma-separated list) + Markdown body | logic | US-5 T-13 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The Claude renderer's output has YAML frontmatter with `name` and `description`, and `tools` rendered as a comma-separated list. |
| AC-2 | Determinism (byte-identical repeat renders) and golden-file coverage for this harness are verified by US-5's shared renderer test suite. |
