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

**Two sections, always.** Open questions first, answered questions after.

### Section 1 — Open questions

**One block per question, one table per question, rows are the possible
answers.** Six questions means six `##` headings and six tables, never one table
with six rows: a row per question holds one resolution, so the options vanish and
the owner gets a verdict instead of a choice. Heading, one-paragraph
description, then that question's own table:

```markdown
## Q-01 <the question, phrased as a question>

<what is actually unresolved, and what it blocks — one short paragraph>

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| <name> — Recommended | <what it means in practice> | <what the user gains> | <what it costs> |
| <name> | … | … | … |

## Q-02 <the next question>

…
```

Those four columns and no others — **no ID column**; the ID is in the heading. A
first column of `Q-01`, `Q-02`, … is the collapsed form.

Mark exactly one row `— Recommended`, first. Never fewer than two rows: a
single-row table is a decision already taken, not a question.

### Section 2 — Answered questions

Every question answered so far, as a list: the question, the answer, and a short
reasoning. The user needs this for compliance, so nothing leaves the list once it
has entered it.

IDs are permanent. Answering moves a question to Section 2 under the same ID;
never renumber, and never mint fresh IDs for what earlier runs already settled.
Reordering Section 1 by consequence leaves gaps and out-of-order numbers — that
is correct.

### Register

In the comment this is business language — no identifiers, no paths, no
provenance tags, and no link to or mention of the brief — litefactory's run
header links it already. Open on the questions: no title line, no "research
completed". Close on the invitation to reply *"move to the next phase."* In the
brief's `## Open Questions` the same content keeps the technical register:
citations, versions and the tags `.claude/skills/shared/research-brief.md`
defines.

## Ordering and completeness

- **Most consequential first** — read from the top, cut from the end.
- **Carry forward** every still-unresolved question. Dropping one silently reads
  as answered.
- **Nothing vanishes.** An answered question moves to Section 2; it is never
  deleted.
- **No empty case.** Nothing unresolved → say so, and Section 2 carries the rest.
- **Never truncate or compress to fit.** Every question keeps its own block and
  table, however many there are. If the list genuinely will not fit, drop the
  least consequential blocks whole and say how many — the brief has them.
  Merging into one table is not a shorter version of this format.

## The bug case

A specialist reporting a blocker **without having attempted the call** is a bug,
not an open question. Send it back once. Only a blocker naming a refused call
becomes an entry, and that entry names the missing capability.
