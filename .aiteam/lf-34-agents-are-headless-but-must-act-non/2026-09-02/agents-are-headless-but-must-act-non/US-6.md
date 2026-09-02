# US-6 — spike.md: ask via litefactory

> As someone running a Spike, I want the spike mode instructions to say how
> to surface a question via litefactory if one genuinely comes up, so that a
> blocked spike ends with a usable report instead of a silent guess.

This story adds headless-asking framing to `spike.md`, scoped to the low
frequency the owner expects a spike to actually need it.

## Why This Matters

Spike is meant to be quick and low-ceremony, so genuine blockers should be
rare — the owner was explicit that the bar here is low, and this is
correspondingly the smallest of the escalation-behaviour stories in this
plan. But "rare" is not "never," and today spike.md has no instruction at all
for what to do when a spike genuinely cannot proceed without a decision only
the owner can make. Left unaddressed, that gap forces the same choice every
other file in this plan was forcing: guess, or stall. This story closes that
gap in the one place spike.md already hands work back rather than growing
scope, so a blocked spike produces a complete, honest report rather than an
invented answer.

## Architecture Notes

- Quote US-1's vocabulary; do not invent new phrasing for "headless" or
  "asking is stopping."
- Add the blocked case to the section that already tells the agent to hand
  work back rather than grow it — this is the natural home for "and here is
  what to do if you're stuck," not a new section.
- Extend the "when you are done" guidance so a blocked spike still produces
  `SPIKE.md`, populated with a "not done" note and a recommendation, rather
  than no artifact at all.
- Platform: none — no Go file in scope.
- Risk: spike is deliberately excluded from the Go-level asking grant (like
  Implement). Wording here must not imply spike may ask interactively — it
  may only end the run and surface the question via litefactory, the same
  shape as Implement's escalation, not the same shape as Plan/Research's
  live-asking permission.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Extend the hand-work-back guidance with the blocked case, using US-1's vocabulary | logic | US-1 | S |
| T-6.2 | Extend the "when you are done" guidance so a blocked spike still produces `SPIKE.md` with a "not done" note and a recommendation populated | logic | T-6.1 | XS |
| T-6.3 | Verify the wording does not imply spike may ask interactively — only that it ends the run and surfaces the question | test | T-6.2 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | `spike.md` states what to do when a spike genuinely cannot proceed without a decision only the owner can make: end the run and surface the question via litefactory. |
| AC-2 | This guidance sits alongside the existing hand-work-back instruction, not as an unrelated new section. |
| AC-3 | A blocked spike still produces `SPIKE.md`, with a section noting the run is not done and a recommendation for what's needed. |
| AC-4 | The wording does not state or imply that spike may ask interactively or wait for a live reply. |
| AC-5 | The file quotes US-1's vocabulary rather than introducing new phrasing. |
| AC-6 | `make check` passes; `git status --short` shows only `.claude/modes/spike.md` changed for this story. |
