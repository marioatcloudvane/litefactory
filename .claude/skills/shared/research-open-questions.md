# Open questions — the full inventory

Read this when a run has to show **every** unresolved question, rather than the
ranked shortlist the entry report offers. Two callers use it today:

- **Research**, writing the `## Open Questions` section of `RESEARCH_BRIEF.md`,
  and — where the whole inventory belongs in the comment — the tail of the final
  message, after the shortlist that `.claude/skills/shared/research-report-entry.md`
  governs.
- **Plan**, picking up a brief and needing to see what Research left unsettled
  before it commits to scope.

The entry report carries the two or three questions worth acting on. This file
carries all of them. Neither replaces the other.

## What counts as an open question

- A fact the run could not establish — the repository does not say, the web did
  not settle it, the specialist reported it unresolved.
- A decision that is the owner's to make, not yours.
- A surprise the tour turned up that nobody has explained yet — an unexpected
  dependency, a pattern that contradicts the stated architecture, a path with no
  obvious owner.
- A question a specialist handed back explicitly, including one `web-researcher`
  reported as *not settled*.

**Carry uncertainty forward rather than smoothing it over.** An open question is
a legitimate, expected part of a research brief. A confident sentence covering a
gap is how a wrong assumption reaches Plan mode.

What does **not** belong here: anything you could have answered by invoking a
specialist and did not. The inventory records what is genuinely unsettled, not
what went unasked.

## What each entry carries

| Field | Why |
|---|---|
| **The question**, stated as a question | "Research X" is a topic; "does X support Y as of version Z" is answerable |
| **What it blocks** | An unresolved question with no consequence is noise |
| **Who can settle it** | The owner, a named specialist, or the next mode — never left implicit |
| **What it would take** | The search, the decision, or the access that is missing |
| **Provenance** | `[V]` verified this run / `[O]` reported by the owner / `[W]` second-hand, unverified / `[R0]` carried forward from an earlier run |

In the brief, keep the technical register — citations, tags, identifiers. In a
comment, drop all of that and name each thing by what it *does*; the four hard
rules in `.claude/skills/shared/research-report-entry.md` apply to every word
that lands on the issue.

## Ordering and completeness

- **Most consequential first.** The list is read from the top and, in a comment,
  cut from the end.
- **Carry forward.** Questions from an earlier run that are still unresolved stay
  in the inventory, tagged `[R0]`. Dropping one silently reads as "answered".
- **Mark what closed.** A question the run settled leaves the inventory with a
  one-line statement of the answer, not by vanishing.
- **No empty case.** If nothing is unresolved, say *"nothing unresolved"* in as
  many words. Silence is not a finding.
- **Never truncate the inventory to fit.** There is no size limit here. If the
  whole list cannot safely sit in a comment's tail, the comment carries the
  shortlist and the link, and the full inventory lives in the brief — which is
  exactly what the link is for.

## The bug case

A specialist that reports a blocker **without having attempted the call** is a
bug, not an open question. Do not write it into the inventory as unresolved —
send the specialist back once (see the dispatch rules in the invoking
orchestrator). Only a blocker the specialist can name a refused call for becomes
an entry here, and then the entry says which capability was missing.
