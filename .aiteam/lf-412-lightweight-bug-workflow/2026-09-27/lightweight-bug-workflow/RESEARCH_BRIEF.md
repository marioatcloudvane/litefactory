# Research brief

## Request

Ease the Bug workflow (issue #412): let the fixer run the project's own checks itself, inside its own sandbox, instead of relying on a separate factory-run check cycle; and once the fixer's repair-attempt budget is spent, have the workflow ask the factory/owner for guidance instead of just posting a one-way failure notice. This sits inside the larger four-point simplification already recorded on #412 (a much smaller data store, evidence shown instead of hard gates, keeping the two owner approvals, and keeping the independent factory checks, the repair budget and the existing graph diagram).

## Decisions

- **d1** — Read 'vack' in the trigger comment as a typo for 'back' -- the ask is for the workflow to feed the outcome back to the factory/owner and request guidance, not a separate unnamed action.
- **d2** — Treat the comment's two asks as sharpening two of the four targets already recorded on the underlying ticket, not as replacing or narrowing them; the scope below is drafted against the full four-point target, with a proposal on how much to build now.
- **d3** — Because the three-attempt budget already exists, scope the 'try 3 times' request as confirming current behavior, not building it; the actionable gap is changing what happens after the budget is spent, from a silent notice to an interactive ask.
- **d4** — Read the owner's 'ask for advice' instruction as requiring a real human decision point when attempts run out, never an automatic extra try and never an automatic shutdown.
- **d5** — Any extra attempt granted after exhaustion must be tied to the exact request it answers, the same way today's two approvals are bound to an exact analysis or an exact delivery review, so an old answer can never be replayed for a different bug or a later exhaustion.
- **d6** — A bug stopped this way (the person chooses not to continue) is recorded as its own distinct outcome, separate from a normal close, so history never implies a fix that never shipped.


## Assumptions

- **a1** — The fixer is allowed to self-run only the faster two of the three check phases; the slowest, most thorough phase stays exclusively the factory's own final check, so the final gate keeps meaning something and self-checking stays fast enough to iterate on.
- **a2** — The fixer gets a small fixed number of self-check runs per attempt (a handful, not unlimited), so a stuck fixer cannot loop indefinitely; the exact number is an implementation choice for the next stage.
- **a3** — A self-check never blocks the fixer from finishing its attempt -- if the sandbox is unavailable, the fixer reports its work the same way it does today, just without the extra self-check evidence.
- **a4** — Unless answered otherwise in the open questions, the fixer's self-check stays private to its own process; the human reviewer keeps seeing exactly what they see today.
- **a5** — The self-check must run the identical pinned commands, in the identical sandbox, that the factory's own final check would run, so a pass during self-check reliably predicts a pass in the real gate.


## Open questions

### q1 — How much of the underlying ticket should this build take on right now?

_Asked of: Product Owner Questions_

- **a** — Just the two things above: the fixer testing itself, and asking a person once attempts run out. Bank the database rebuild and the rule-by-rule review as separate follow-up work.  *(recommended)*
- **b** — Everything at once: these two plus the database rebuild plus the rule-by-rule review, in one build.
- **c** — These two plus the database rebuild, but leave the rule-by-rule review for later.

### q2 — Once the fixer is out of tries, how far should a person be allowed to push it back into motion?

_Asked of: Product Owner Questions_

- **a** — Ask every single time it runs dry. A person can grant one more attempt as many times as they're willing to, always after seeing the full history of what was tried first.  *(recommended)*
- **b** — Allow exactly one grant, ever, per bug. After that one extra try, the only choice left is to stop and hand it to a person.
- **c** — No grants at all. Once the three tries are gone, the only choice is stop or hand off, never try again.

### q3 — Should the fixer's self-checks show up as evidence in the review a person sees before merging, or stay invisible to them?

_Asked of: Product Owner Questions_

- **a** — Keep it invisible for now. The fixer uses its own checks to decide it's ready; the person reviewing still only sees exactly what they see today.  *(recommended)*
- **b** — Show it. Let the person reviewing see that the fixer already ran its own checks and how many times, as extra confidence before they approve.
