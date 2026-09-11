# Open questions — the full inventory

Read when a run must show **every** question. Callers: Research, writing
the brief's `## Open Questions` (and the comment's tail when the whole list
belongs there); Plan, checking what Research left unsettled before fixing scope.

## What qualifies

- A fact the run could not establish — repo silent, web unsettled, specialist
  reported it unresolved.
- A decision that is the owner's, not yours.
- An unexplained surprise from the tour: unexpected dependency, a pattern
  contradicting the stated architecture, a path with no owner.

**Carry uncertainty forward.** A confident sentence covering a gap is how a wrong
assumption reaches Plan mode.

Not an open question: anything a specialist could have answered and was not
asked. The inventory records what is unsettled, not what went unasked.

## How you build the response

Two Sections, always:

1: Open Questions (see structure below)
2: Answered Questions for reference

### Section 1: Open Questions

Show the open questions as a headered List with a table in the structure below, it repeats per Question

Header of the Question (with ##) - Numbered with "Q-xx" where xx is the number

Description of the question

| Field | Why |
|---|---|
| Solution xx (xx is the number) | . Use a " - Recommended" if your prefered one|
| Details | A Detail of the Solution, described |
| Benefits | What are the benefits if this solution is picked? |
| Tradeoff | What are the downsides if this solution is picked? |

Important is that this is repeated per open Question, as we need to keep it viewable for the enduser!

### Section 2: Answered Questions

Show all answered Questions in a list with a short reasoning. The user needs to check this still for compliance reason.

## Ordering and completeness

- **Most consequential first** — read from the top, cut from the end.
- **Carry forward** still-unresolved questions as `[R0]`. Dropping one silently
  reads as answered.
- **Mark what closed** with a one-line answer, rather than letting it vanish.
- **No empty case.** Nothing unresolved → write "nothing unresolved".
- **Never truncate to fit.** If the list cannot safely sit in a comment's tail,
  the comment carries the shortlist and the link; the full list lives in the
  brief. That is what the link is for.

## The bug case

A specialist reporting a blocker **without having attempted the call** is a bug,
not an open question. Send it back once. Only a blocker naming a refused call
becomes an entry, and that entry names the missing capability.
