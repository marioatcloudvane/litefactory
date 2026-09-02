# US-8 — go-architect.md: major-deviation escalation trigger

> As the go-architect agent, I want an explicit trigger for escalating a major
> deviation from the plan or spec to the owner via litefactory, so that a
> deviation serious enough to matter doesn't just get silently absorbed into
> the architecture review.

This story extends go-architect.md's existing deviation-request block with a
specific escalation trigger for major deviations from the plan or spec — not
a general question-and-answer pattern.

## Why This Matters

go-architect's job is architectural review, and it already has a "deviation
request" pattern for when the architecture needs to depart from the plan. The
owner wants that pattern strengthened for the case where the deviation is
major — not a general invitation to ask questions the way
requirements-engineer does, but a specific trigger: when the right
architecture genuinely diverges from what was planned or specified in a way
that matters, that needs to reach the owner via litefactory rather than being
resolved unilaterally or quietly downgraded to "follow the standard for now."
Getting the scope right matters here: too broad, and go-architect starts
behaving like a second requirements-engineer; too narrow, and a real
architectural divergence goes unnoticed until implementation is already
underway.

## Architecture Notes

- Quote US-1's vocabulary; do not invent new phrasing for "headless" or
  "asking is stopping."
- Extend the existing deviation-request block — do not create a new,
  separate section. The trigger is an addition to a pattern that already
  exists, not a new pattern.
- Scope the trigger narrowly: "major deviation from the plan or spec,"
  not general Q&A. This story does not give go-architect a way to ask
  clarifying questions about scope, requirements, or anything
  requirements-engineer-shaped — only to escalate when the architecture it
  would recommend materially diverges from what was planned.
- Preserve the existing default: until a deviation is escalated and
  addressed, the engineer follows the standard approach — this story adds an
  escalation path, it does not change what happens by default while waiting
  for one.
- **Shares one code block with US-9** (the same deviation-request block, at
  the same location). Land both stories as one edit: this story's structural
  extension first, then US-9's name-to-role substitution — so the new lines
  this story adds are written with a role, not a name, from the start.
- Platform: none — no Go file in scope.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Extend the deviation-request block with the major-deviation escalation trigger | logic | US-1 | S |
| T-8.2 | Verify the extension preserves the existing "follow the standard until approved" default and does not widen into general Q&A | test | T-8.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The deviation-request block in `go-architect.md` states an explicit trigger: a major deviation from the plan or spec is escalated to the owner via litefactory. |
| AC-2 | The trigger is scoped to major deviations only — the block does not become a general invitation to ask clarifying questions. |
| AC-3 | The existing default ("follow the standard approach until a deviation is approved") is preserved unchanged. |
| AC-4 | The extension uses role-based phrasing (no personal name), written together with US-9 in one edit. |
| AC-5 | The file quotes US-1's vocabulary rather than introducing new phrasing. |
| AC-6 | `make check` passes; `git status --short` shows only `.claude/agents/go-architect.md` changed for this story (landed together with US-9). |
