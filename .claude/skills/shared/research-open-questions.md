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

One `##` heading per question, numbered `Q-01`, `Q-02`, … then a one-paragraph
description, then one table of the solutions. **Repeat this per question** — the
end user has to be able to read them one at a time.

```markdown
## Q-01 <the question, phrased as a question>

<what is actually unresolved, and what it blocks — one short paragraph>

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| <name> — Recommended | <what it means in practice> | <what the user gains> | <what it costs> |
| <name> | … | … | … |
```

Mark exactly one row `— Recommended` and make it the first row. Always offer a
real alternative: a single-row table is a decision already taken, not a question.

### Section 2 — Answered questions

Every question answered so far, as a list: the question, the answer, and a short
reasoning. The user needs this for compliance, so nothing leaves the list once it
has entered it.

### Register

In the comment this is business language — no identifiers, no paths, no
provenance tags. In the brief's `## Open Questions` the same content keeps the
technical register: citations, versions and the tags
`.claude/skills/shared/research-brief.md` defines.

## Ordering and completeness

- **Most consequential first** — read from the top, cut from the end.
- **Carry forward** every still-unresolved question. Dropping one silently reads
  as answered.
- **Nothing vanishes.** An answered question moves to Section 2; it is never
  deleted.
- **No empty case.** Nothing unresolved → say so, and Section 2 carries the rest.
- **Never truncate to fit.** If the full list cannot safely sit in the comment,
  lead with the most consequential questions and link the brief for the rest.
  That is what the link is for.

## The bug case

A specialist reporting a blocker **without having attempted the call** is a bug,
not an open question. Send it back once. Only a blocker naming a refused call
becomes an entry, and that entry names the missing capability.
