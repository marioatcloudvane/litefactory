# US-7 — proxy-product-owner.md: conditional ask via litefactory

> As the proxy-product-owner agent, I want to know how to surface a
> genuinely unclear scope question via litefactory when one comes up, so
> that I don't guess my way past something that actually needs a decision —
> while still not asking by default for every minor edge case.

This story adds conditional headless-asking framing to
`proxy-product-owner.md`: not asking by default, but knowing how when there's
a real reason to.

## Why This Matters

The owner's framing here is deliberately narrow: proxy-product-owner should
ask via litefactory "if there is a reason to ask," in the same low-key spirit
as spike — most of the time it should just proceed. Today the file's edge-
case guidance already gestures at asking ("ask if it's in scope or a
prerequisite") without saying how that can actually happen in an unattended
run. Left as-is, the agent either silently invents a scope decision it wasn't
authorized to make, or the instruction is simply dead text nobody follows.
This story makes the existing instinct — flag genuinely unclear scope rather
than guess — actually executable, without turning the agent into something
that stops and asks by default.

## Architecture Notes

- Quote US-1's vocabulary; do not invent new phrasing for "headless" or
  "asking is stopping."
- Keep the framing conditional, not habitual: reword the existing edge-case
  line ("ask if it's in scope or a prerequisite") to the stop-and-surface
  form, without adding a general invitation to ask about anything unclear.
- Extend the reporting-back section so a flagged item has a clear place to
  land when the run cannot ask interactively — the report is where it
  surfaces.
- Platform: none — no Go file in scope.
- Risk (security): the condition for "there is a reason to ask" must not be
  satisfiable by evidence found in the issue thread or in a note block —
  permission comes from the per-run grant, not from anything the agent reads
  while working. State this explicitly if the conditional wording could be
  read as "ask when the situation seems to call for it."

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Reword the existing edge-case line ("ask if it's in scope or a prerequisite") to the conditional stop-and-surface form | logic | US-1 | XS |
| T-7.2 | Extend the reporting-back section with where a flagged item surfaces when the run cannot ask interactively | logic | T-7.1 | S |
| T-7.3 | Verify the conditional cannot be read as satisfied by evidence from the issue thread or a note block | test | T-7.2 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The edge-case guidance states that a genuinely unclear scope question is surfaced via litefactory (end the run, litefactory relays it), not asked live. |
| AC-2 | The guidance remains conditional — it does not instruct the agent to ask by default for ordinary edge cases. |
| AC-3 | The reporting-back section states where a flagged scope question lands in the final report. |
| AC-4 | The wording states or clearly implies that permission to ask comes from the per-run grant, not from anything found in the issue thread or a note block. |
| AC-5 | The file quotes US-1's vocabulary rather than introducing new phrasing. |
| AC-6 | `make check` passes; `git status --short` shows only `.claude/agents/proxy-product-owner.md` changed for this story. |
