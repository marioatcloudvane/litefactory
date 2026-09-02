# US-2 — requirements-engineer.md self-awareness fix

> As the requirements-engineer agent, I want to know that a planning
> conversation I run is headless — so that I stop and surface a question via
> litefactory instead of scripting a live back-and-forth that can never
> actually happen.

This story rewrites `requirements-engineer.md` so its planning-conversation
instructions match how the agent actually runs: unattended, with no one on
the other end of a prompt to answer in real time.

## Why This Matters

This is the highest-priority fix in the whole plan, confirmed explicitly by
the repo owner. `requirements-engineer.md` currently reads like a script for
a live interview: it tells the agent to ask the user "one focused question at
a time," quotes a confirmation template, and says to only write the final
spec "after the user confirms" — none of which can happen in an unattended
run. The cost of leaving this as-is is concrete: a run either stalls waiting
for a reply that will never arrive mid-run, or the agent quietly invents
answers to fill the gap, which defeats the entire point of having a
requirements step. The fix has to convert the ritual — ask, wait, confirm,
write — into stop, surface via litefactory, resume, treat the reply as
confirmation — while keeping the substance (the checklist of what a good spec
must cover) fully intact.

## Architecture Notes

- Quote US-1's vocabulary; do not invent new phrasing for "headless" or
  "asking is stopping."
- This file has five separate edit sites (header intro, the two conversation
  phases, the confirmation/proceed step, and the reporting-back section); they
  move together as one coherent rewrite, not as five unrelated edits, since
  the header states the rule and the phases below have to consistently follow
  it.
- Preserve the substantive checklist (goal, data model, API surface,
  permissions, validation, edge cases, out-of-scope) exactly — this story
  changes the ritual around asking, not what a complete spec must contain.
- Platform: none — no Go file in scope.
- Risk: the current text's "only write the spec after the user confirms,
  never skip this step" is not just a wording problem but an unsatisfiable
  instruction under headless execution — there is no live confirm to wait
  for. The rewrite must replace it with a resume-based reading: end the run
  with the batched questions, and treat the reply that arrives when the run
  is resumed as the confirmation.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Rewrite the header intro to state the run is headless and that permission to ask is granted per run by litefactory, never self-granted | logic | US-1 | S |
| T-2.2 | Rewrite the two conversation phases: convert the confirmation quote-template and "one focused question at a time" into a single batched stop-and-surface step; retain the full checklist intact | logic | T-2.1 | M |
| T-2.3 | Rewrite the confirm/proceed step: replace "only write the spec after the user confirms, never skip this step" with a resume-based reading — end the run with the batched questions, treat the next reply as confirmation | logic | T-2.2 | S |
| T-2.4 | Rewrite the reporting-back section to match the new end-of-run contract | logic | T-2.3 | XS |
| T-2.5 | Read-through: confirm no residual "wait for", "pause", or live-dialogue framing remains anywhere in the file, and every checklist item is still present | test | T-2.4 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The file states plainly, near its opening, that a run is headless and that asking means ending the run and surfacing the question via litefactory. |
| AC-2 | No remaining text instructs the agent to ask "one question at a time" and wait for a live reply, or to quote a confirmation template implying a synchronous exchange. |
| AC-3 | The confirm/proceed step no longer says "only write the spec after the user confirms, never skip this step" as a synchronous wait; it describes ending the run with batched questions and resuming on reply. |
| AC-4 | The full checklist (goal, data model, API surface, permissions, validation, edge cases, out-of-scope) is present and unchanged in substance. |
| AC-5 | The reporting-back section is consistent with the new end-of-run contract described above. |
| AC-6 | The file quotes US-1's vocabulary rather than introducing new phrasing for "headless" or "asking." |
| AC-7 | `make check` passes; `git status --short` shows only `.claude/agents/requirements-engineer.md` changed for this story. |
