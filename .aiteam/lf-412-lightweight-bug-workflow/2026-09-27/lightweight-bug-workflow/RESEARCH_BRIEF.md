# Research brief

## Request

Ease the Bug workflow (issue #412): let the fixer run the project's own checks itself, inside its own sandbox, instead of relying on a separate factory-run check cycle; and once the fixer's repair-attempt budget is spent, have the workflow ask the factory/owner for guidance instead of just posting a one-way failure notice. This sits inside the larger four-point simplification already recorded on #412 (a much smaller data store, evidence shown instead of hard gates, keeping the two owner approvals, and keeping the independent factory checks, the repair budget and the existing graph diagram).

## Decisions

- **d1** — Read 'vack' in the trigger comment as a typo for 'back' -- the ask is for the workflow to feed the outcome back to the factory/owner and request guidance, not a separate unnamed action.
- **d2** — Treat the comment's two asks as sharpening two of the four targets already recorded on the underlying ticket, not as replacing or narrowing them; the scope is drafted against the full four-point target.
- **d3** — Because the three-attempt budget already exists, scope the 'try 3 times' request as confirming current behavior, not building it; the actionable gap is changing what happens after the budget is spent, from a silent notice to an interactive ask.
- **d4** — Read the owner's 'ask for advice' instruction as requiring a real human decision point when attempts run out, never an automatic extra try and never an automatic shutdown.
- **d5** — Any extra attempt granted after exhaustion must be tied to the exact request it answers, the same way today's two approvals are bound to an exact analysis or an exact delivery review, so an old answer can never be replayed for a different bug or a later exhaustion.
- **d6** — A bug stopped this way (the person chooses not to continue) is recorded as its own distinct outcome, separate from a normal close, so history never implies a fix that never shipped.
- **d7** — The owner explicitly rejected deferring the database rebuild and the evidence-instead-of-gates review to a later follow-up; both are now in scope for this build, not just the two behavior changes from the first round.
- **d8** — Adopt the underlying ticket's own test for which hard-stop rules survive verbatim as the in-scope rule: a refusal stays only if removing it would let something merge, close or spend budget without one of the two owner approvals already covering it; every other refusal becomes a visible fact in review.
- **d9** — Because the merge/close retry bookkeeping is live, used behavior rather than dead code, the consolidation folds it into the relevant step's row as extra columns rather than deleting it outright.


## Assumptions

- **a1** — The fixer is allowed to self-run only the faster two of the three check phases; the slowest, most thorough phase stays exclusively the factory's own final check, so the final gate keeps meaning something and self-checking stays fast enough to iterate on.
- **a2** — The fixer gets a small fixed number of self-check runs per attempt (a handful, not unlimited), so a stuck fixer cannot loop indefinitely; the exact number is an implementation choice for the next stage.
- **a3** — A self-check never blocks the fixer from finishing its attempt -- if the sandbox is unavailable, the fixer reports its work the same way it does today, just without the extra self-check evidence.
- **a4** — Self-check results now appear in the delivery review as evidence, consistent with the evidence-instead-of-gates goal that is in scope for this build; this replaces the earlier default of keeping them private to the fixer's own process.
- **a5** — The self-check must run the identical pinned commands, in the identical sandbox, that the factory's own final check would run, so a pass during self-check reliably predicts a pass in the real gate.


## Open questions

### q2 — Once the fixer is out of tries, how far should a person be allowed to push it back into motion?

_Asked of: Product Owner Questions_

- **a** — Ask every single time it runs dry. A person can grant one more attempt as many times as they're willing to, always after seeing the full history of what was tried first.  *(recommended)*
- **b** — Allow exactly one grant, ever, per bug. After that one extra try, the only choice left is to stop and hand it to a person.
- **c** — No grants at all. Once the three tries are gone, the only choice is stop or hand off, never try again.

### q4 — When the smaller database goes live, what happens to bugs already in flight and their old history?

_Asked of: Product Owner Questions_

- **a** — Simplest: only switch over once no bug task is actively running. New bugs use the new, smaller tables from day one. Old finished bugs' history stays exactly where it is, in the old tables, untouched, in case anyone needs to look it up later.  *(recommended)*
- **b** — Build real migration logic that converts every in-flight bug's current state into the new tables, so nothing ever has to pause for the switch.
- **c** — Convert in-flight bugs like option b, but don't bother keeping old finished bugs' history anywhere once the new schema is live.
