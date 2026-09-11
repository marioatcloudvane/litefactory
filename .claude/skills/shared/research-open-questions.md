# Open questions — the full inventory

Read when a run must show **every** unresolved question, not the ranked shortlist
in `.claude/skills/shared/research-report-entry.md`. Callers: Research, writing
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

## Each entry carries

| Field | Why |
|---|---|
| the question, phrased as a question | "research X" is a topic; "does X support Y as of version Z" is answerable |
| what it blocks | an unresolved question with no consequence is noise |
| who settles it | owner, named specialist, or next mode — never implicit |
| what it would take | the search, decision, or access that is missing |
| provenance | `[V]` verified this run · `[O]` owner-reported · `[W]` second-hand, unverified · `[R0]` carried from an earlier run |

Brief: technical register, citations and tags. Comment: drop all of that; the
four hard rules govern every word that lands on the issue.

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
